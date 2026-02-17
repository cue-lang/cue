package yaml

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// TODO(mvdan): we should sanity check that the decoder always produces valid CUE,
// as it is possible to construct a cue/ast syntax tree with invalid literals
// or with expressions that will always error, such as `float & 123`.
//
// One option would be to do this as part of the tests; a more general approach
// may be fuzzing, which would find more bugs and work for any decoder,
// although it may be slow as we need to involve the evaluator.

// Decoder is a temporary interface compatible with both the old and new yaml decoders.
type Decoder interface {
	// Decode consumes a YAML value and returns it in CUE syntax tree node.
	Decode() (ast.Expr, error)
}

// decoder wraps a [yaml.Decoder] to extract CUE syntax tree nodes.
type decoder struct {
	yamlDecoder yaml.Decoder

	// yamlNonEmpty is true once yamlDecoder tells us the input YAML wasn't empty.
	// Useful so that we can extract "null" when the input is empty.
	yamlNonEmpty bool

	// decodeErr is returned by any further calls to Decode when not nil.
	decodeErr error

	src      []byte
	tokFile  *token.File
	tokLines []int

	// pendingHeadComments collects the head (preceding) comments
	// from the YAML nodes we are extracting.
	// We can't add comments to a CUE syntax tree node until we've created it,
	// but we need to extract these comments first since they have earlier positions.
	pendingHeadComments []*ast.Comment

	// extractingAliases ensures we don't loop forever when expanding YAML anchors.
	extractingAliases map[*yaml.Node]bool

	// lastOffset is byte offset from the last yaml.Node position that
	// we decoded, used for working out relative positions such as
	// token.NewSection. This offset can only increase, moving forward
	// in the file. A value of -1 means no position has been recorded
	// yet.
	lastOffset int

	// forceNewline ensures that the next position will be on a new line.
	forceNewline bool

	// scopeEnd is the byte offset (exclusive) bounding the current
	// node's extent in the source. Used to compute Rbrace positions
	// for struct literals: the Rbrace is placed at offset scopeEnd-1
	// (typically the \n ending the last line before the next content).
	scopeEnd int
}

// TODO(mvdan): this can be io.Reader really, except that token.Pos is offset-based,
// so the only way to really have true Offset+Line+Col numbers is to know
// the size of the entire YAML node upfront.
// With json we can use RawMessage to know the size of the input
// before we extract into ast.Expr, but unfortunately, yaml.Node has no size.

// NewDecoder creates a decoder for YAML values to extract CUE syntax tree nodes.
//
// The filename is used for position information in CUE syntax tree nodes
// as well as any errors encountered while decoding YAML.
func NewDecoder(filename string, b []byte) *decoder {
	// Note that yaml.v3 can insert a null node just past the end of the input
	// in some edge cases, so we pretend that there's an extra newline
	// so that we don't panic when handling such a position.
	tokFile := token.NewFile(filename, 0, len(b)+1)
	tokFile.SetLinesForContent(b)
	return &decoder{
		src:         b,
		tokFile:     tokFile,
		tokLines:    append(tokFile.Lines(), len(b)),
		yamlDecoder: *yaml.NewDecoder(bytes.NewReader(b)),
		lastOffset:  -1,
		// TODO: for a streaming decoder we'll need to remove this
		// dependency on knowing the length of the input ahead of time.
		scopeEnd: len(b),
	}
}

// Decode consumes a YAML value and returns it in CUE syntax tree node.
//
// A nil node with an io.EOF error is returned once no more YAML values
// are available for decoding.
func (d *decoder) Decode() (ast.Expr, error) {
	if err := d.decodeErr; err != nil {
		return nil, err
	}
	var yn yaml.Node
	if err := d.yamlDecoder.Decode(&yn); err != nil {
		if err == io.EOF {
			// Any further Decode calls must return EOF to avoid an endless loop.
			d.decodeErr = io.EOF

			// If the input is empty, we produce `*null | _` followed by EOF.
			// Note that when the input contains "---", we get an empty document
			// with a null scalar value inside instead.
			if !d.yamlNonEmpty {
				// Attach positions which at least point to the filename.
				pos := d.tokFile.Pos(0, token.NoRelPos)
				return &ast.BinaryExpr{
					Op:    token.OR,
					OpPos: pos,
					X: &ast.UnaryExpr{
						Op:    token.MUL,
						OpPos: pos,
						X: &ast.BasicLit{
							Kind:     token.NULL,
							ValuePos: pos,
							Value:    "null",
						},
					},
					Y: &ast.Ident{
						Name:    "_",
						NamePos: pos,
					},
				}, nil
			}
			// If the input wasn't empty, we already decoded some CUE syntax nodes,
			// so here we should just return io.EOF to stop.
			return nil, io.EOF
		}
		// Unfortunately, yaml.v3's syntax errors are opaque strings,
		// and they only include line numbers in some but not all cases.
		// TODO(mvdan): improve upstream's errors so they are structured
		// and always contain some position information.
		e := err.Error()
		if s, ok := strings.CutPrefix(e, "yaml: line "); ok {
			// From "yaml: line 3: some issue" to "foo.yaml:3: some issue".
			e = d.tokFile.Name() + ":" + s
		} else if s, ok := strings.CutPrefix(e, "yaml:"); ok {
			// From "yaml: some issue" to "foo.yaml: some issue".
			e = d.tokFile.Name() + ":" + s
		} else {
			return nil, err
		}
		err = errors.New(e)
		// Any further Decode calls repeat this error.
		d.decodeErr = err
		return nil, err
	}
	d.yamlNonEmpty = true
	return d.extract(&yn)
}

// Unmarshal parses a single YAML value to a CUE expression.
func Unmarshal(filename string, data []byte) (ast.Expr, error) {
	d := NewDecoder(filename, data)
	n, err := d.Decode()
	if err != nil {
		if err == io.EOF {
			return nil, nil // empty input
		}
		return nil, err
	}
	// TODO(mvdan): decoding the entire next value is unnecessary;
	// consider either a "More" or "Done" method to tell if we are at EOF,
	// or splitting the Decode method into two variants.
	// This should use proper error values with positions as well.
	if n2, err := d.Decode(); err == nil {
		return nil, fmt.Errorf("%s: expected a single YAML document", n2.Pos())
	} else if err != io.EOF {
		return nil, fmt.Errorf("expected a single YAML document: %v", err)
	}
	return n, nil
}

func (d *decoder) extract(yn *yaml.Node) (ast.Expr, error) {
	d.addHeadCommentsToPending(yn)
	var expr ast.Expr
	var err error
	switch yn.Kind {
	case yaml.DocumentNode:
		expr, err = d.document(yn)
	case yaml.SequenceNode:
		expr, err = d.sequence(yn)
	case yaml.MappingNode:
		expr, err = d.mapping(yn)
	case yaml.ScalarNode:
		expr, err = d.scalar(yn)
	case yaml.AliasNode:
		expr, err = d.alias(yn)
	default:
		return nil, d.posErrorf(yn, "unknown yaml node kind: %d", yn.Kind)
	}
	if err != nil {
		return nil, err
	}
	d.addCommentsToNode(expr, yn, 1)
	return expr, nil
}

// comments parses a newline-delimited list of YAML "#" comments
// and turns them into a list of cue/ast comments.
func (d *decoder) comments(src string) []*ast.Comment {
	if src == "" {
		return nil
	}
	var comments []*ast.Comment
	for line := range strings.SplitSeq(src, "\n") {
		if line == "" {
			continue // yaml.v3 comments have a trailing newline at times
		}
		comments = append(comments, &ast.Comment{
			// Trim the leading "#".
			// Note that yaml.v3 does not give us comment positions.
			Text: "//" + line[1:],
		})
	}
	return comments
}

// addHeadCommentsToPending parses a node's head comments and adds them to a pending list,
// to be used later by addComments once a cue/ast node is constructed.
func (d *decoder) addHeadCommentsToPending(yn *yaml.Node) {
	comments := d.comments(yn.HeadComment)
	// TODO(mvdan): once yaml.v3 records comment positions,
	// we can better ensure that sections separated by empty lines are kept that way.
	// For now, all we can do is approximate by counting lines,
	// and assuming that head comments are not separated from their node.
	// This will be wrong in some cases, moving empty lines, but is better than nothing.
	if len(d.pendingHeadComments) == 0 && len(comments) > 0 {
		c := comments[0]
		if d.lastOffset >= 0 && (yn.Line-len(comments))-d.offsetLine(d.lastOffset) >= 2 {
			c.Slash = c.Slash.WithRel(token.NewSection)
		}
	}
	d.pendingHeadComments = append(d.pendingHeadComments, comments...)
}

// addCommentsToNode adds any pending head comments, plus a YAML node's line
// and foot comments, to a cue/ast node.
func (d *decoder) addCommentsToNode(n ast.Node, yn *yaml.Node, linePos int8) {
	// cue/ast and cue/format are not able to attach a comment to a node
	// when the comment immediately follows the node.
	// For some nodes like fields, the best we can do is move the comments up.
	// For the root-level struct, we do want to leave comments
	// at the end of the document to be left at the very end.
	//
	// TODO(mvdan): can we do better? for example, support attaching trailing comments to a cue/ast.Node?
	footComments := d.comments(yn.FootComment)
	if _, ok := n.(*ast.StructLit); !ok {
		d.pendingHeadComments = append(d.pendingHeadComments, footComments...)
		footComments = nil
	}
	if comments := d.pendingHeadComments; len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Doc:      true,
			Position: 0,
			List:     comments,
		})
	}
	if comments := d.comments(yn.LineComment); len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Line:     true,
			Position: linePos,
			List:     comments,
		})
	}
	if comments := footComments; len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			// After 100 tokens, so that the comment goes after the entire node.
			// TODO(mvdan): this is hacky, can the cue/ast API support trailing comments better?
			Position: 100,
			List:     comments,
		})
	}
	d.pendingHeadComments = nil
}

func (d *decoder) posErrorf(yn *yaml.Node, format string, args ...any) error {
	// TODO(mvdan): use columns as well; for now they are left out to avoid test churn
	// return fmt.Errorf(d.pos(n).String()+" "+format, args...)
	return fmt.Errorf(d.tokFile.Name()+":"+strconv.Itoa(yn.Line)+": "+format, args...)
}

// yamlOffset converts a YAML node's line and column to a byte offset.
func (d *decoder) yamlOffset(yn *yaml.Node) int {
	return d.tokLines[yn.Line-1] + (yn.Column - 1)
}

// offsetLine returns a 1-indexed line number for the given byte
// offset.
func (d *decoder) offsetLine(offset int) int {
	return sort.Search(len(d.tokLines), func(i int) bool {
		return d.tokLines[i] > offset
	})
}

// contentOffset returns the byte offset where a node's content
// starts, skipping past any YAML anchor prefix (&name). For
// flow-style nodes, it finds the opening delimiter (open). For
// block-style nodes, it skips past the anchor and newline. When the
// node has no anchor, it returns the node's position as-is.
func (d *decoder) contentOffset(yn *yaml.Node, open byte) int {
	offset := d.yamlOffset(yn)
	if yn.Anchor == "" {
		return offset
	}

	if yn.Style&yaml.FlowStyle != 0 {
		for offset < len(d.src) && d.src[offset] != open {
			offset++
		}
		return offset
	}

	// For block style, we want to be as greedy as possible. So once
	// we're past the anchor, we want to stop right after the first
	// newline, or at the first non-whitespace, whichever is sooner.
	offset += 1 + len(yn.Anchor) // skip '&' and anchor name
	newlineSeen := false
	for ; offset < len(d.src); offset++ {
		if newlineSeen {
			return offset
		}
		switch d.src[offset] {
		case ' ', '\t':
		case '\n', '\r':
			newlineSeen = true
		default:
			return offset
		}
	}
	return offset
}

// pos converts a byte offset to a cue/ast position.
// Note that this method uses and updates the last offset in lastOffset,
// so it should be called with increasing offsets.
func (d *decoder) pos(offset int) token.Pos {
	pos := d.tokFile.Pos(offset, token.NoRelPos)

	if d.forceNewline {
		d.forceNewline = false
		pos = pos.WithRel(token.Newline)
	} else if d.lastOffset >= 0 {
		lastLine := d.offsetLine(d.lastOffset)
		curLine := d.offsetLine(offset)
		switch {
		case curLine-lastLine >= 2:
			pos = pos.WithRel(token.NewSection)
		case curLine-lastLine == 1:
			pos = pos.WithRel(token.Newline)
		case offset-d.lastOffset > 0:
			pos = pos.WithRel(token.Blank)
		default:
			pos = pos.WithRel(token.NoSpace)
		}
		// If for any reason the offset is before the last offset, give
		// up and return an empty position.
		//
		// TODO(mvdan): Brought over from the old decoder; when does this happen?
		// Can we get rid of those edge cases and this bit of logic?
		if offset < d.lastOffset {
			return token.NoPos
		}
	}
	d.lastOffset = offset
	return pos
}

// findClosing scans forward from start in the source bytes to find
// the first occurrence of close (typically '}' or ']') that is not
// inside a quoted string or comment. It returns the byte offset.
func (d *decoder) findClosing(start int, close byte) int {
	for i := start; i < len(d.src); i++ {
		switch d.src[i] {
		case close:
			return i
		case '"':
			// Skip double-quoted string.
			for i++; i < len(d.src); i++ {
				if d.src[i] == '\\' {
					i++ // skip escaped character
				} else if d.src[i] == '"' {
					break
				}
			}
		case '\'':
			// Skip single-quoted string.
			for i++; i < len(d.src); i++ {
				if d.src[i] == '\'' {
					if i+1 < len(d.src) && d.src[i+1] == '\'' {
						i++ // skip '' escape
					} else {
						break
					}
				}
			}
		case '#':
			// Skip comment to end of line. In YAML flow context, #
			// starts a comment when preceded by whitespace (or at the
			// start of the scan region).
			if i == start || d.src[i-1] == ' ' || d.src[i-1] == '\t' {
				for i++; i < len(d.src) && d.src[i] != '\n'; i++ {
				}
			}
		}
	}
	return len(d.src) // shouldn't happen with valid YAML
}

// isBlankLine returns true if the 0-indexed line contains only
// whitespace.
func (d *decoder) isBlankLine(lineIdx int) bool {
	start := d.tokLines[lineIdx]
	end := d.tokLines[lineIdx+1]
	for i := start; i < end; i++ {
		switch d.src[i] {
		case ' ', '\t', '\n', '\r':
		default:
			return false
		}
	}
	return true
}

// isCommentLine returns true if the 0-indexed line is a comment-only
// line (optional leading whitespace followed by '#').
func (d *decoder) isCommentLine(lineIdx int) bool {
	start := d.tokLines[lineIdx]
	end := d.tokLines[lineIdx+1]
	for i := start; i < end; i++ {
		switch c := d.src[i]; c {
		case ' ', '\t':
		default:
			return c == '#'
		}
	}
	return false
}

// scopeEndBefore computes the scope end before the given YAML node,
// excluding any head comments and their surrounding blank lines from
// the scope. This ensures that comments belonging to the next sibling
// are not consumed by the current node's scope.
func (d *decoder) scopeEndBefore(yn *yaml.Node) int {
	end := d.tokLines[yn.Line-1]
	if yn.HeadComment == "" {
		return end
	}
	// Walk backwards from the line before yn, skipping blank lines and
	// then comment lines that belong to yn's head comments.
	lineIdx := yn.Line - 2 // 0-indexed line just before yn
	// Skip blank lines between comment and node.
	for lineIdx >= 0 && d.isBlankLine(lineIdx) {
		lineIdx--
	}
	// Skip comment lines.
	for lineIdx >= 0 && d.isCommentLine(lineIdx) {
		lineIdx--
	}
	return d.tokLines[lineIdx+1]
}

func (d *decoder) document(yn *yaml.Node) (ast.Expr, error) {
	if n := len(yn.Content); n != 1 {
		return nil, d.posErrorf(yn, "yaml document nodes are meant to have one content node but have %d", n)
	}
	return d.extract(yn.Content[0])
}

func (d *decoder) sequence(yn *yaml.Node) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd // save before the loop modifies it
	list := &ast.ListLit{
		// Compute the bracket position directly without the side
		// effects of d.pos. Like struct braces, brackets are a CUE
		// concept with no YAML counterpart in block-style sequences.
		// Use contentOffset to skip past any anchor prefix.
		Lbrack: d.tokFile.Pos(d.contentOffset(yn, '['), token.Blank),
	}

	// Unlike mappings which use d.label for keys, sequences extract
	// elements directly. Advance lastOffset so that element relative
	// positions are computed against the sequence node, not whatever
	// came before it.
	if ynOffset := d.yamlOffset(yn); ynOffset >= d.lastOffset {
		d.lastOffset = ynOffset
	}

	multiline := false
	if len(yn.Content) > 0 {
		multiline = yn.Line < yn.Content[len(yn.Content)-1].Line
	}

	// If a list is empty, or ends with a struct, the closing `]` is on the same line.
	closeSameLine := true
	for i, c := range yn.Content {
		d.forceNewline = multiline
		// Set the scope end for the element we're about to extract.
		if i+1 < len(yn.Content) {
			d.scopeEnd = d.scopeEndBefore(yn.Content[i+1])
		} else {
			d.scopeEnd = parentScopeEnd
		}
		elem, err := d.extract(c)
		if err != nil {
			return nil, err
		}
		list.Elts = append(list.Elts, elem)
		// A list of structs begins with `[{`, so let it end with `}]`.
		_, closeSameLine = elem.(*ast.StructLit)
	}

	if yn.Style&yaml.FlowStyle != 0 {
		// Flow-style sequence: find the actual ']' in the source.
		start := d.lastOffset
		if len(yn.Content) == 0 {
			start = list.Lbrack.Offset() + 1
		}
		rbrackOff := d.findClosing(start, ']')
		// Update lastOffset past the ']' so that any parent flow
		// mapping's scan starts after this one's closing brace.
		d.lastOffset = rbrackOff + 1
		list.Rbrack = d.tokFile.Pos(rbrackOff, token.Blank)

	} else if len(yn.Content) > 0 {
		// In block-style, there are no explicit brackets, so we have to
		// guess. We want to be as greedy as possible, so we go one byte
		// before the end of our parent node. This intentionally
		// includes whitespace after the end of this sequence but before
		// the end of our parent.
		rel := token.Blank
		if multiline && !closeSameLine {
			rel = token.Newline
		}
		list.Rbrack = d.tokFile.Pos(parentScopeEnd-1, rel)

	} else {
		list.Rbrack = list.Lbrack
	}
	return list, nil
}

func (d *decoder) mapping(yn *yaml.Node) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd // save before insertMap modifies it
	strct := &ast.StructLit{
		Lbrace: d.tokFile.Pos(d.contentOffset(yn, '{'), token.Blank),
	}
	multiline := false
	if len(yn.Content) > 0 {
		multiline = yn.Line < yn.Content[len(yn.Content)-1].Line
	}

	if err := d.insertMap(yn, strct, multiline, false); err != nil {
		return nil, err
	}

	if yn.Style&yaml.FlowStyle != 0 {
		// Flow-style mapping: find the actual '}' in the source.
		// Start scanning from the last decoded position (which is
		// past all children due to chaining), or from the '{' if empty.
		start := d.lastOffset
		if len(yn.Content) == 0 {
			start = strct.Lbrace.Offset() + 1
		}
		rbraceOff := d.findClosing(start, '}')
		d.lastOffset = rbraceOff + 1
		strct.Rbrace = d.tokFile.Pos(rbraceOff, token.Blank)

	} else if len(yn.Content) > 0 {
		// In block-style, there are no explicit braces, so we have to
		// guess. We want to be as greedy as possible, so we go one byte
		// before the end of our parent node. This intentionally
		// includes whitespace after the end of this mapping but before
		// the end of our parent.
		rel := token.Blank
		if multiline {
			rel = token.Newline
		}
		strct.Rbrace = d.tokFile.Pos(parentScopeEnd-1, rel)

	} else {
		strct.Rbrace = strct.Lbrace
	}
	return strct, nil
}

func (d *decoder) insertMap(yn *yaml.Node, m *ast.StructLit, multiline, mergeValues bool) error {
	parentScopeEnd := d.scopeEnd
	l := len(yn.Content)
outer:
	for i := 0; i < l; i += 2 {
		if multiline {
			d.forceNewline = true
		}
		yk, yv := yn.Content[i], yn.Content[i+1]
		d.addHeadCommentsToPending(yk)
		if isMerge(yk) {
			mergeValues = true
			if err := d.merge(yv, m, multiline); err != nil {
				return err
			}
			continue
		}

		field := &ast.Field{}
		label, err := d.label(yk)
		if err != nil {
			return err
		}
		d.addCommentsToNode(field, yk, 2)
		field.Label = label

		// Set the scope end for the value we're about to extract.
		if i+2 < l {
			d.scopeEnd = d.scopeEndBefore(yn.Content[i+2])
		} else {
			d.scopeEnd = parentScopeEnd
		}

		if mergeValues {
			key := labelStr(label)
			for _, decl := range m.Elts {
				f := decl.(*ast.Field)
				name, _, err := ast.LabelName(f.Label)
				if err == nil && name == key {
					f.Value, err = d.extract(yv)
					if err != nil {
						return err
					}
					continue outer
				}
			}
		}

		value, err := d.extract(yv)
		if err != nil {
			return err
		}
		field.Value = value

		m.Elts = append(m.Elts, field)
	}
	return nil
}

func (d *decoder) merge(yn *yaml.Node, m *ast.StructLit, multiline bool) error {
	switch yn.Kind {
	case yaml.MappingNode:
		return d.insertMap(yn, m, multiline, true)
	case yaml.AliasNode:
		return d.insertMap(yn.Alias, m, multiline, true)
	case yaml.SequenceNode:
		// Step backwards as earlier nodes take precedence.
		for _, c := range slices.Backward(yn.Content) {
			if err := d.merge(c, m, multiline); err != nil {
				return err
			}
		}
		return nil
	default:
		return d.posErrorf(yn, "map merge requires map or sequence of maps as the value")
	}
}

func (d *decoder) label(yn *yaml.Node) (ast.Label, error) {
	pos := d.pos(d.yamlOffset(yn))

	var expr ast.Expr
	var err error
	var value string
	switch yn.Kind {
	case yaml.ScalarNode:
		expr, err = d.scalar(yn)
		value = yn.Value
	case yaml.AliasNode:
		if yn.Alias.Kind != yaml.ScalarNode {
			return nil, d.posErrorf(yn, "invalid map key: %v", yn.Alias.ShortTag())
		}
		expr, err = d.alias(yn)
		value = yn.Alias.Value
	default:
		return nil, d.posErrorf(yn, "invalid map key: %v", yn.ShortTag())
	}
	if err != nil {
		return nil, err
	}

	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind != token.STRING {
			// With incoming YAML like `Null: 1`, the key scalar is normalized to "null".
			value = expr.Value
		}
		label := ast.NewStringLabel(value)
		ast.SetPos(label, pos)
		return label, nil
	default:
		return nil, d.posErrorf(yn, "invalid label "+value)
	}
}

const (
	// TODO(mvdan): The strings below are from yaml.v3; should we be relying on upstream somehow?
	nullTag      = "!!null"
	boolTag      = "!!bool"
	strTag       = "!!str"
	intTag       = "!!int"
	floatTag     = "!!float"
	timestampTag = "!!timestamp"
	seqTag       = "!!seq"
	mapTag       = "!!map"
	binaryTag    = "!!binary"
	mergeTag     = "!!merge"
)

// rxAnyOctalYaml11 uses the implicit tag resolution regular expression for base-8 integers
// from YAML's 1.1 spec, but including the 8 and 9 digits which aren't valid for octal integers.
var rxAnyOctalYaml11 = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[-+]?0[0-9_]+$`)
})

func (d *decoder) scalar(yn *yaml.Node) (ast.Expr, error) {
	tag := yn.ShortTag()
	// If the YAML scalar has no explicit tag, yaml.v3 infers a float tag,
	// and the value looks like a YAML 1.1 octal literal,
	// that means the input value was like `01289` and not a valid octal integer.
	// The safest thing to do, and what most YAML decoders do, is to interpret as a string.
	if yn.Style&yaml.TaggedStyle == 0 && tag == floatTag && rxAnyOctalYaml11().MatchString(yn.Value) {
		tag = strTag
	}
	pos := d.pos(d.yamlOffset(yn))
	switch tag {
	// TODO: use parse literal or parse expression instead.
	case timestampTag:
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.String.Quote(yn.Value),
		}, nil
	case strTag:
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.String.WithOptionalTabIndent(1).Quote(yn.Value),
		}, nil

	case binaryTag:
		data, err := base64.StdEncoding.DecodeString(yn.Value)
		if err != nil {
			return nil, d.posErrorf(yn, "!!binary value contains invalid base64 data")
		}
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.Bytes.Quote(string(data)),
		}, nil

	case boolTag:
		t := false
		switch yn.Value {
		// TODO(mvdan): The strings below are from yaml.v3; should we be relying on upstream somehow?
		case "true", "True", "TRUE":
			t = true
		}
		lit := ast.NewBool(t)
		lit.ValuePos = pos
		return lit, nil

	case intTag:
		// Convert YAML octal to CUE octal. If YAML accepted an invalid
		// integer, just convert it as well to ensure CUE will fail.
		value := yn.Value
		if len(value) > 1 && value[0] == '0' && value[1] <= '9' {
			value = "0o" + value[1:]
		}
		var info literal.NumInfo
		// We make the assumption that any valid YAML integer literal will be a valid
		// CUE integer literal as well, with the only exception of octal numbers above.
		// Note that `!!int 123.456` is not allowed.
		if err := literal.ParseNum(value, &info); err != nil {
			return nil, d.posErrorf(yn, "cannot decode %q as %s: %v", value, tag, err)
		} else if !info.IsInt() {
			return nil, d.posErrorf(yn, "cannot decode %q as %s: not a literal number", value, tag)
		}
		return d.makeNum(pos, value, token.INT), nil

	case floatTag:
		value := yn.Value
		// TODO(mvdan): The strings below are from yaml.v3; should we be relying on upstream somehow?
		switch value {
		case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
			value = "+Inf"
		case "-.inf", "-.Inf", "-.INF":
			value = "-Inf"
		case ".nan", ".NaN", ".NAN":
			value = "NaN"
		default:
			var info literal.NumInfo
			// We make the assumption that any valid YAML float literal will be a valid
			// CUE float literal as well, with the only exception of Inf/NaN above.
			// Note that `!!float 123` is allowed.
			if err := literal.ParseNum(value, &info); err != nil {
				return nil, d.posErrorf(yn, "cannot decode %q as %s: %v", value, tag, err)
			}
			// If the decoded YAML scalar was explicitly or implicitly a float,
			// and the scalar literal looks like an integer,
			// unify it with "number" to record the fact that it was represented as a float.
			// Don't unify with float, as `float & 123` is invalid, and there's no need
			// to forbid representing the number as an integer either.
			if yn.Tag != "" {
				if p := strings.IndexAny(value, ".eEiInN"); p == -1 {
					// TODO: number(v) when we have conversions
					// TODO(mvdan): don't shove the unification inside a BasicLit.Value string
					//
					// TODO(mvdan): would it be better to do turn `!!float 123` into `123.0`
					// rather than `number & 123`? Note that `float & 123` is an error.
					value = fmt.Sprintf("number & %s", value)
				}
			}
		}
		return d.makeNum(pos, value, token.FLOAT), nil

	case nullTag:
		return &ast.BasicLit{
			ValuePos: pos.WithRel(token.Blank),
			Kind:     token.NULL,
			Value:    "null",
		}, nil
	default:
		return nil, d.posErrorf(yn, "cannot unmarshal tag %q", tag)
	}
}

func (d *decoder) makeNum(pos token.Pos, val string, kind token.Token) (expr ast.Expr) {
	val, negative := strings.CutPrefix(val, "-")
	expr = &ast.BasicLit{
		ValuePos: pos,
		Kind:     kind,
		Value:    val,
	}
	if negative {
		expr = &ast.UnaryExpr{
			OpPos: pos,
			Op:    token.SUB,
			X:     expr,
		}
	}
	return expr
}

func (d *decoder) alias(yn *yaml.Node) (ast.Expr, error) {
	if d.extractingAliases[yn] {
		// TODO this could actually be allowed in some circumstances.
		return nil, d.posErrorf(yn, "anchor %q value contains itself", yn.Value)
	}
	if d.extractingAliases == nil {
		d.extractingAliases = make(map[*yaml.Node]bool)
	}
	d.extractingAliases[yn] = true

	// Save and reset decoder state so the alias extraction doesn't
	// interfere with the outer position tracking. The aliased node
	// may be earlier in the source (the common case: define then use),
	// which would leave lastOffset pointing past the aliased content
	// and cause findClosing to find the wrong closing delimiter.
	savedLastOffset := d.lastOffset
	savedForceNewline := d.forceNewline
	savedScopeEnd := d.scopeEnd
	d.lastOffset = -1 // no position yet, so pos() produces valid positions for the alias's children
	d.forceNewline = false

	node, err := d.extract(yn.Alias)

	d.lastOffset = savedLastOffset
	d.forceNewline = savedForceNewline
	d.scopeEnd = savedScopeEnd
	delete(d.extractingAliases, yn)
	if err != nil {
		return nil, err
	}

	// For container types, override brace/bracket positions to reflect
	// the alias reference site (*name), not the anchor definition site.
	// The alias is where this value logically appears in the document.
	aliasStart := d.yamlOffset(yn)
	aliasEnd := aliasStart + len(yn.Value) // *<name>: 1 + len(name) - 1
	switch n := node.(type) {
	case *ast.StructLit:
		n.Lbrace = d.tokFile.Pos(aliasStart, token.Blank)
		n.Rbrace = d.tokFile.Pos(aliasEnd, token.Blank)
	case *ast.ListLit:
		n.Lbrack = d.tokFile.Pos(aliasStart, token.Blank)
		n.Rbrack = d.tokFile.Pos(aliasEnd, token.Blank)
	}
	return node, nil
}

func labelStr(l ast.Label) string {
	switch l := l.(type) {
	case *ast.Ident:
		return l.Name
	case *ast.BasicLit:
		s, _ := literal.Unquote(l.Value)
		return s
	}
	return ""
}

func isMerge(yn *yaml.Node) bool {
	// TODO(mvdan): The boolean logic below is from yaml.v3; should we be relying on upstream somehow?
	return yn.Kind == yaml.ScalarNode && yn.Value == "<<" && (yn.Tag == "" || yn.Tag == "!" || yn.ShortTag() == mergeTag)
}
