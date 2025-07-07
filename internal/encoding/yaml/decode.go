package yaml

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
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

	tokFile  *token.File
	tokLines []int

	// pendingHeadComments collects the head (preceding) comments
	// from the YAML nodes we are extracting.
	// We can't add comments to a CUE syntax tree node until we've created it,
	// but we need to extract these comments first since they have earlier positions.
	pendingHeadComments []*ast.Comment

	// extractingAliases ensures we don't loop forever when expanding YAML anchors.
	extractingAliases map[*yaml.Node]bool

	// lastPos is the last YAML node position that we decoded,
	// used for working out relative positions such as token.NewSection.
	// This position can only increase, moving forward in the file.
	lastPos token.Position

	// forceNewline ensures that the next position will be on a new line.
	forceNewline bool

	// anchorFields contains the anchors that are gathered as we walk the YAML nodes.
	// these are only added to the AST when we're done processing the whole document.
	anchorFields []ast.Field
	// anchorNames map anchor nodes to their names.
	anchorNames map[*yaml.Node]string
	// anchorTakenNames keeps track of anchor names that have been taken.
	// It is used to ensure unique anchor names.
	anchorTakenNames map[string]struct{}
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
		tokFile:          tokFile,
		tokLines:         append(tokFile.Lines(), len(b)),
		yamlDecoder:      *yaml.NewDecoder(bytes.NewReader(b)),
		anchorNames:      make(map[*yaml.Node]string),
		anchorTakenNames: make(map[string]struct{}),
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

func (d *decoder) extractNoAnchor(yn *yaml.Node) (ast.Expr, error) {
	switch yn.Kind {
	case yaml.DocumentNode:
		return d.document(yn)
	case yaml.SequenceNode:
		return d.sequence(yn)
	case yaml.MappingNode:
		return d.mapping(yn)
	case yaml.ScalarNode:
		return d.scalar(yn)
	case yaml.AliasNode:
		return d.referenceAlias(yn)
	default:
		return nil, d.posErrorf(yn, "unknown yaml node kind: %d", yn.Kind)
	}
}

func (d *decoder) extract(yn *yaml.Node) (ast.Expr, error) {
	d.addHeadCommentsToPending(yn)

	var expr ast.Expr
	var err error

	if yn.Anchor == "" {
		expr, err = d.extractNoAnchor(yn)
	} else {
		expr, err = d.anchor(yn)
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
	for _, line := range strings.Split(src, "\n") {
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
		if d.lastPos.IsValid() && (yn.Line-len(comments))-d.lastPos.Line >= 2 {
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

// pos converts a YAML node position to a cue/ast position.
// Note that this method uses and updates the last position in lastPos,
// so it should be called on YAML nodes in increasing position order.
func (d *decoder) pos(yn *yaml.Node) token.Pos {
	// Calculate the position's offset via the line and column numbers.
	offset := d.tokLines[yn.Line-1] + (yn.Column - 1)
	pos := d.tokFile.Pos(offset, token.NoRelPos)

	if d.forceNewline {
		d.forceNewline = false
		pos = pos.WithRel(token.Newline)
	} else if d.lastPos.IsValid() {
		switch {
		case yn.Line-d.lastPos.Line >= 2:
			pos = pos.WithRel(token.NewSection)
		case yn.Line-d.lastPos.Line == 1:
			pos = pos.WithRel(token.Newline)
		case yn.Column-d.lastPos.Column > 0:
			pos = pos.WithRel(token.Blank)
		default:
			pos = pos.WithRel(token.NoSpace)
		}
		// If for any reason the node's position is before the last position,
		// give up and return an empty position. Akin to: yn.Pos().Before(d.lastPos)
		//
		// TODO(mvdan): Brought over from the old decoder; when does this happen?
		// Can we get rid of those edge cases and this bit of logic?
		if yn.Line < d.lastPos.Line || (yn.Line == d.lastPos.Line && yn.Column < d.lastPos.Column) {
			return token.NoPos
		}
	}
	d.lastPos = token.Position{Line: yn.Line, Column: yn.Column}
	return pos
}

func (d *decoder) document(yn *yaml.Node) (ast.Expr, error) {
	if n := len(yn.Content); n != 1 {
		return nil, d.posErrorf(yn, "yaml document nodes are meant to have one content node but have %d", n)
	}

	expr, err := d.extract(yn.Content[0])
	if err != nil {
		return nil, err
	}

	return d.addAnchorNodes(expr)
}

// addAnchorNodes prepends anchor nodes at the top of the document.
func (d *decoder) addAnchorNodes(expr ast.Expr) (ast.Expr, error) {
	elements := []ast.Decl{}

	for _, field := range d.anchorFields {
		elements = append(elements, &field)
	}

	switch x := expr.(type) {
	case *ast.StructLit:
		x.Elts = append(elements, x.Elts...)
	case *ast.ListLit:
		if len(elements) > 0 {
			expr = &ast.StructLit{
				Elts: append(elements, x),
			}
		}
	default:
		// If the whole YAML document is not a map / seq, then it can't have anchors.
		// maybe assert that `anchorFields` is empty?
		break
	}

	return expr, nil
}

func (d *decoder) sequence(yn *yaml.Node) (ast.Expr, error) {
	list := &ast.ListLit{
		Lbrack: d.pos(yn).WithRel(token.Blank),
	}
	multiline := false
	if len(yn.Content) > 0 {
		multiline = yn.Line < yn.Content[len(yn.Content)-1].Line
	}

	// If a list is empty, or ends with a struct, the closing `]` is on the same line.
	closeSameLine := true
	for _, c := range yn.Content {
		d.forceNewline = multiline
		elem, err := d.extract(c)
		if err != nil {
			return nil, err
		}
		list.Elts = append(list.Elts, elem)
		// A list of structs begins with `[{`, so let it end with `}]`.
		_, closeSameLine = elem.(*ast.StructLit)
	}
	if multiline && !closeSameLine {
		list.Rbrack = list.Rbrack.WithRel(token.Newline)
	}
	return list, nil
}

func (d *decoder) mapping(yn *yaml.Node) (ast.Expr, error) {
	strct := &ast.StructLit{}
	multiline := false
	if len(yn.Content) > 0 {
		multiline = yn.Line < yn.Content[len(yn.Content)-1].Line
	}

	if err := d.insertMap(yn, strct, multiline, false); err != nil {
		return nil, err
	}
	// TODO(mvdan): moving these positions above insertMap breaks a few tests, why?
	strct.Lbrace = d.pos(yn).WithRel(token.Blank)
	if multiline {
		strct.Rbrace = strct.Lbrace.WithRel(token.Newline)
	} else {
		strct.Rbrace = strct.Lbrace
	}
	return strct, nil
}

func (d *decoder) insertMap(yn *yaml.Node, m *ast.StructLit, multiline, mergeValues bool) error {
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
	pos := d.pos(yn)

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
		expr, err = d.inlineAlias(yn)
		value = yn.Alias.Value
	default:
		return nil, d.posErrorf(yn, "invalid map key: %v", yn.ShortTag())
	}
	if err != nil {
		return nil, err
	}

	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			if ast.IsValidIdent(value) && !internal.IsDefOrHidden(value) {
				return &ast.Ident{
					NamePos: pos,
					Name:    value,
				}, nil
			}
			ast.SetPos(expr, pos)
			return expr, nil
		}

		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.Label.Quote(expr.Value),
		}, nil

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
	switch tag {
	// TODO: use parse literal or parse expression instead.
	case timestampTag:
		return &ast.BasicLit{
			ValuePos: d.pos(yn),
			Kind:     token.STRING,
			Value:    literal.String.Quote(yn.Value),
		}, nil
	case strTag:
		return &ast.BasicLit{
			ValuePos: d.pos(yn),
			Kind:     token.STRING,
			Value:    literal.String.WithOptionalTabIndent(1).Quote(yn.Value),
		}, nil

	case binaryTag:
		data, err := base64.StdEncoding.DecodeString(yn.Value)
		if err != nil {
			return nil, d.posErrorf(yn, "!!binary value contains invalid base64 data")
		}
		return &ast.BasicLit{
			ValuePos: d.pos(yn),
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
		lit.ValuePos = d.pos(yn)
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
		return d.makeNum(yn, value, token.INT), nil

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
		return d.makeNum(yn, value, token.FLOAT), nil

	case nullTag:
		return &ast.BasicLit{
			ValuePos: d.pos(yn).WithRel(token.Blank),
			Kind:     token.NULL,
			Value:    "null",
		}, nil
	default:
		return nil, d.posErrorf(yn, "cannot unmarshal tag %q", tag)
	}
}

func (d *decoder) makeNum(yn *yaml.Node, val string, kind token.Token) (expr ast.Expr) {
	val, negative := strings.CutPrefix(val, "-")
	expr = &ast.BasicLit{
		ValuePos: d.pos(yn),
		Kind:     kind,
		Value:    val,
	}
	if negative {
		expr = &ast.UnaryExpr{
			OpPos: d.pos(yn),
			Op:    token.SUB,
			X:     expr,
		}
	}
	return expr
}

// inlineAlias expands an alias node in place, returning the expanded node.
// Sometimes we have to resort to this, for example when the alias
// is inside a map key, since CUE does not support structs as map keys.
func (d *decoder) inlineAlias(yn *yaml.Node) (ast.Expr, error) {
	if d.extractingAliases[yn] {
		// TODO this could actually be allowed in some circumstances.
		return nil, d.posErrorf(yn, "anchor %q value contains itself", yn.Value)
	}
	if d.extractingAliases == nil {
		d.extractingAliases = make(map[*yaml.Node]bool)
	}
	d.extractingAliases[yn] = true
	var node ast.Expr
	node, err := d.extractNoAnchor(yn.Alias)
	delete(d.extractingAliases, yn)
	return node, err
}

// referenceAlias replaces an alias with a reference to the identifier of its anchor.
func (d *decoder) referenceAlias(yn *yaml.Node) (ast.Expr, error) {
	anchor, ok := d.anchorNames[yn.Alias]
	if !ok {
		return nil, d.posErrorf(yn, "anchor %q not found", yn.Alias.Anchor)
	}

	return &ast.Ident{
		NamePos: d.pos(yn),
		Name:    anchor,
	}, nil
}

func (d *decoder) anchor(yn *yaml.Node) (ast.Expr, error) {
	var anchorIdent string

	// Pick a non-conflicting anchor name.
	for i := 1; ; i++ {
		if i == 1 {
			anchorIdent = "#" + yn.Anchor
		} else {
			anchorIdent = "#" + yn.Anchor + "_" + strconv.Itoa(i)
		}
		if _, ok := d.anchorTakenNames[anchorIdent]; !ok {
			d.anchorTakenNames[anchorIdent] = struct{}{}
			break
		}
	}
	d.anchorNames[yn] = anchorIdent

	// Process the node itself, but don't put it into the AST just yet,
	// store it for later to be used as an anchor identifier.
	pos := d.pos(yn)
	expr, err := d.extractNoAnchor(yn)
	if err != nil {
		return nil, err
	}
	d.anchorFields = append(d.anchorFields, ast.Field{
		Label: &ast.Ident{Name: anchorIdent},
		Value: expr,
	})

	return &ast.Ident{
		NamePos: pos,
		Name:    anchorIdent,
	}, nil
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
