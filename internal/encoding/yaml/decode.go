package yaml

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf16"

	gyaml "github.com/goccy/go-yaml"
	gast "github.com/goccy/go-yaml/ast"
	glexer "github.com/goccy/go-yaml/lexer"
	gparser "github.com/goccy/go-yaml/parser"
	gtoken "github.com/goccy/go-yaml/token"

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

// decoder wraps the goccy/go-yaml parser to extract CUE syntax tree nodes.
type decoder struct {
	docs   []*gast.DocumentNode
	docIdx int

	// decodeErr is returned by any further calls to Decode when not nil.
	decodeErr error

	// parseErr records an error from parsing (possibly with partial recovery).
	// When non-nil and docs is also non-nil, it indicates a partial result:
	// the docs contain the successfully parsed prefix of the input.
	parseErr error

	// isEmpty is true when the input was entirely empty (no content, not even ---).
	isEmpty bool

	src      []byte
	tokFile  *token.File
	tokLines []int

	// pendingHeadComments collects the head (preceding) comments
	// from the YAML nodes we are extracting.
	pendingHeadComments []*ast.Comment

	// anchors maps anchor names to their corresponding YAML AST nodes.
	anchors map[string]gast.Node

	// extractingAliases ensures we don't loop forever when expanding YAML anchors.
	extractingAliases map[string]bool

	// lastOffset is byte offset from the last yaml node position that
	// we decoded, used for working out relative positions such as
	// token.NewSection. This offset can only increase, moving forward
	// in the file. A value of -1 means no position has been recorded
	// yet.
	lastOffset int

	// forceNewline ensures that the next position will be on a new line.
	forceNewline bool

	// scopeEnd is the byte offset (exclusive) bounding the current
	// node's extent in the source.
	scopeEnd int
}

// NewDecoder creates a decoder for YAML values to extract CUE syntax tree nodes.
//
// The filename is used for position information in CUE syntax tree nodes
// as well as any errors encountered while decoding YAML.
func NewDecoder(filename string, b []byte) *decoder {
	// Detect and convert UTF-16 BOM to UTF-8.
	// YAML 1.1 allows UTF-16 with BOM; goccy only handles UTF-8.
	b = decodeUTF16BOM(b)

	// Note that we add an extra byte to the file size to handle edge cases
	// where a position might be just past the end of the input.
	tokFile := token.NewFile(filename, 0, len(b)+1)
	tokFile.SetLinesForContent(b)
	d := &decoder{
		src:        b,
		tokFile:    tokFile,
		tokLines:   append(tokFile.Lines(), len(b)),
		lastOffset: -1,
		scopeEnd:   len(b),
	}

	// Detect truly empty input before parsing.
	if len(bytes.TrimSpace(b)) == 0 {
		d.isEmpty = true
		return d
	}

	file, err := d.parseYAML(b)
	if file != nil && hasContent(file) {
		d.docs = file.Docs
	}
	if err != nil {
		fmtErr := fmt.Errorf("%s: %s", filename, err)
		if len(d.docs) > 0 {
			// Partial result: we recovered some content despite errors.
			d.parseErr = fmtErr
		} else {
			// Total failure: no content recovered.
			d.decodeErr = fmtErr
		}
	}

	return d
}

// parseYAML tokenizes and parses YAML input with error recovery.
//
// Rather than using gparser.ParseBytes directly, we use the lower-level
// lexer.Tokenize + parser.Parse pipeline. This gives us control over
// error recovery: when parsing fails, we can truncate the token stream
// at the error position and retry, yielding a partial AST for everything
// before the first error.
//
// The recovery strategy has two levels:
//
//  1. Scanner errors: goccy's lexer produces InvalidType tokens for
//     scanner-level errors (e.g. tab characters in indentation).
//     We strip these and everything after them before parsing.
//
//  2. Parser errors: when the parser rejects structurally valid tokens
//     (e.g. unclosed brackets, unexpected keys), its error includes
//     the offending token's position. We truncate before that position
//     and retry. This typically converges in 1–2 iterations.
//
// If the original input contains tabs that cause scanner errors, we
// also attempt parsing with tabs replaced by spaces as a fallback.
func (d *decoder) parseYAML(b []byte) (*gast.File, error) {
	file, firstErr := d.parseWithRecovery(string(b))
	if firstErr == nil {
		return file, nil
	}

	// If the error might be tab-related, retry with tabs replaced.
	if bytes.ContainsRune(b, '\t') {
		b2 := bytes.ReplaceAll(b, []byte("\t"), []byte(" "))
		file2, err2 := d.parseWithRecovery(string(b2))
		if err2 == nil || (file2 != nil && hasContent(file2)) {
			// Update source and line table for the tab-replaced input.
			d.src = b2
			tokFile2 := token.NewFile(d.tokFile.Name(), 0, len(b2)+1)
			tokFile2.SetLinesForContent(b2)
			d.tokFile = tokFile2
			d.tokLines = append(tokFile2.Lines(), len(b2))
			d.scopeEnd = len(b2)
			return file2, err2
		}
	}

	return file, firstErr
}

// parseWithRecovery tokenizes src and attempts to parse it, falling
// back to progressively shorter token prefixes on parse errors.
// It returns the best AST it can produce, along with the first error
// encountered (which may be non-nil even when a partial AST is returned).
func (d *decoder) parseWithRecovery(src string) (*gast.File, error) {
	tokens := glexer.Tokenize(src)

	// Level 1: strip invalid tokens (scanner-level errors like tabs).
	var firstErr error
	validTokens := stripInvalidTokens(tokens, &firstErr)

	// Try parsing the valid token prefix.
	file, err := gparser.Parse(validTokens, gparser.ParseComments)
	if err == nil {
		return file, firstErr // firstErr may be non-nil if we stripped tokens
	}
	if firstErr == nil {
		firstErr = err
	}

	// Level 2: parser-level recovery. Use the error's token position
	// to truncate before the problematic area and retry.
	for range 10 {
		errTok := syntaxErrorToken(err)
		if errTok == nil || errTok.Position == nil {
			// No position info; drop the last token and retry.
			if len(validTokens) == 0 {
				break
			}
			validTokens = validTokens[:len(validTokens)-1]
		} else {
			shorter := truncateTokensBefore(validTokens, errTok.Position.Line, errTok.Position.Column)
			if len(shorter) == len(validTokens) {
				// No progress from position-based truncation; drop last token.
				if len(validTokens) == 0 {
					break
				}
				validTokens = validTokens[:len(validTokens)-1]
			} else {
				validTokens = shorter
			}
		}

		if len(validTokens) == 0 {
			break
		}
		file, err = gparser.Parse(validTokens, gparser.ParseComments)
		if err == nil {
			return file, firstErr
		}
	}

	// All recovery attempts failed. Return whatever we have.
	return file, firstErr
}

// stripInvalidTokens returns the prefix of tokens up to (but not including)
// the first InvalidType token. If an invalid token is found, *errOut is set
// to an error describing it (if *errOut is nil).
func stripInvalidTokens(tokens gtoken.Tokens, errOut *error) gtoken.Tokens {
	for i, tk := range tokens {
		if tk.Type == gtoken.InvalidType {
			if *errOut == nil {
				*errOut = fmt.Errorf("%s (at line %d)", tk.Error, tk.Position.Line)
			}
			return tokens[:i]
		}
	}
	return tokens
}

// truncateTokensBefore returns the prefix of tokens that appear strictly
// before the given line and column.
func truncateTokensBefore(tokens gtoken.Tokens, line, col int) gtoken.Tokens {
	for i, tk := range tokens {
		if tk.Position.Line > line ||
			(tk.Position.Line == line && tk.Position.Column >= col) {
			return tokens[:i]
		}
	}
	return tokens
}

// syntaxErrorToken extracts the offending token from a goccy parse error.
func syntaxErrorToken(err error) *gtoken.Token {
	var se *gyaml.SyntaxError
	if errors.As(err, &se) {
		return se.GetToken()
	}
	return nil
}

// hasContent reports whether a parsed YAML file has at least one
// document with a non-nil body.
func hasContent(f *gast.File) bool {
	for _, doc := range f.Docs {
		if doc.Body != nil {
			return true
		}
	}
	return false
}

// decodeUTF16BOM detects a UTF-16 LE or BE BOM at the start of b
// and converts the content to UTF-8. If no BOM is found, b is returned as-is.
func decodeUTF16BOM(b []byte) []byte {
	if len(b) < 2 {
		return b
	}
	var byteOrder binary.ByteOrder
	switch {
	case b[0] == 0xFF && b[1] == 0xFE:
		byteOrder = binary.LittleEndian
	case b[0] == 0xFE && b[1] == 0xFF:
		byteOrder = binary.BigEndian
	default:
		return b
	}
	// Strip BOM and decode UTF-16.
	b = b[2:]
	if len(b)%2 != 0 {
		b = b[:len(b)-1] // drop trailing byte if odd
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		if byteOrder == binary.LittleEndian {
			u16[i] = binary.LittleEndian.Uint16(b[2*i:])
		} else {
			u16[i] = binary.BigEndian.Uint16(b[2*i:])
		}
	}
	runes := utf16.Decode(u16)
	var buf bytes.Buffer
	for _, r := range runes {
		buf.WriteRune(r)
	}
	return buf.Bytes()
}

// Decode consumes a YAML value and returns it in CUE syntax tree node.
//
// A nil node with an io.EOF error is returned once no more YAML values
// are available for decoding.
//
// When the YAML input contains errors, Decode uses error recovery to
// return a partial AST for the valid prefix of the input. In this case,
// the returned expression is non-nil AND the error is non-nil: the
// expression contains whatever could be parsed, and the error describes
// what went wrong. Callers that want best-effort results (e.g. LSP)
// should check the expression even when the error is non-nil.
func (d *decoder) Decode() (ast.Expr, error) {
	if err := d.decodeErr; err != nil {
		return nil, err
	}

	// Handle truly empty input: produce `*null | _` then EOF.
	if d.isEmpty {
		d.isEmpty = false
		d.decodeErr = io.EOF
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

	if d.docIdx >= len(d.docs) {
		if d.parseErr != nil {
			// We had a partial recovery: return the parse error now
			// that all recovered documents have been consumed.
			err := d.parseErr
			d.parseErr = nil
			d.decodeErr = io.EOF
			return nil, err
		}
		d.decodeErr = io.EOF
		return nil, io.EOF
	}

	// Skip documents that only contain directives (e.g., %TAG, %YAML).
	for d.docIdx < len(d.docs) {
		doc := d.docs[d.docIdx]
		d.docIdx++
		if doc.Body == nil {
			// Empty document (e.g., bare ---).
			pos := d.tokFile.Pos(0, token.NoRelPos)
			return &ast.BasicLit{
				ValuePos: pos.WithRel(token.Blank),
				Kind:     token.NULL,
				Value:    "null",
			}, nil
		}
		if _, isDir := doc.Body.(*gast.DirectiveNode); isDir {
			// Skip directive-only documents.
			continue
		}
		expr, err := d.extract(doc.Body)
		if err != nil {
			return expr, err
		}
		// If this is the last document and we had recovery errors,
		// return both the expression and the error.
		if d.docIdx >= len(d.docs) && d.parseErr != nil {
			return expr, d.parseErr
		}
		return expr, nil
	}
	if d.parseErr != nil {
		err := d.parseErr
		d.parseErr = nil
		d.decodeErr = io.EOF
		return nil, err
	}
	d.decodeErr = io.EOF
	return nil, io.EOF
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
	if n2, err := d.Decode(); err == nil {
		return nil, fmt.Errorf("%s: expected a single YAML document", n2.Pos())
	} else if err != io.EOF {
		return nil, fmt.Errorf("expected a single YAML document: %v", err)
	}
	return n, nil
}

// goccyOffset converts a goccy AST node's position to a 0-based byte offset.
// We use Line and Column rather than the Offset field because goccy's Offset
// tracking is inconsistent when comments are present.
func (d *decoder) goccyOffset(n gast.Node) int {
	tk := n.GetToken()
	if tk == nil || tk.Position == nil {
		return 0
	}
	return d.tokenOffset(tk)
}

// tokenOffset converts a goccy token's Line/Column to a 0-based byte offset.
// We use Line and Column rather than the Offset field because goccy's Offset
// tracking is inconsistent when comments are present.
func (d *decoder) tokenOffset(tk *gtoken.Token) int {
	if tk == nil || tk.Position == nil {
		return 0
	}
	line := tk.Position.Line  // 1-indexed
	col := tk.Position.Column // 1-indexed
	if line < 1 || line > len(d.tokLines) {
		return 0
	}
	return d.tokLines[line-1] + (col - 1)
}

// goccyLine returns the 1-indexed line number for a goccy AST node.
func goccyLine(n gast.Node) int {
	tk := n.GetToken()
	if tk == nil || tk.Position == nil {
		return 1
	}
	return tk.Position.Line
}

func (d *decoder) extract(yn gast.Node) (ast.Expr, error) {
	if yn == nil {
		var off int
		if d.lastOffset >= 0 {
			off = d.lastOffset
		}
		pos := d.pos(off)
		return &ast.BasicLit{
			ValuePos: pos.WithRel(token.Blank),
			Kind:     token.NULL,
			Value:    "null",
		}, nil
	}

	// Unwrap anchor nodes: register the anchor and extract the inner value.
	if anchor, ok := yn.(*gast.AnchorNode); ok {
		if d.anchors == nil {
			d.anchors = make(map[string]gast.Node)
		}
		name := anchor.Name.GetToken().Value
		d.anchors[name] = anchor.Value
		return d.extract(anchor.Value)
	}

	// Handle tag nodes.
	if tag, ok := yn.(*gast.TagNode); ok {
		return d.tagged(tag)
	}

	d.addHeadCommentsToPending(yn)

	var expr ast.Expr
	var err error

	switch n := yn.(type) {
	case *gast.MappingNode:
		expr, err = d.mapping(n)
	case *gast.MappingValueNode:
		expr, err = d.singleMappingValue(n)
	case *gast.SequenceNode:
		expr, err = d.sequence(n)
	case *gast.StringNode:
		expr, err = d.stringNode(n)
	case *gast.LiteralNode:
		expr, err = d.literalNode(n)
	case *gast.IntegerNode:
		expr, err = d.integerNode(n)
	case *gast.FloatNode:
		expr, err = d.floatNode(n)
	case *gast.BoolNode:
		expr, err = d.boolNode(n)
	case *gast.NullNode:
		expr, err = d.nullNode(n)
	case *gast.InfinityNode:
		expr, err = d.infinityNode(n)
	case *gast.NanNode:
		expr, err = d.nanNode(n)
	case *gast.AliasNode:
		expr, err = d.alias(n)
	case *gast.DirectiveNode:
		// YAML directives like %TAG or %YAML. Skip and return null.
		// The directive itself is metadata, not content.
		// If there's a next document, it will be handled separately.
		pos := d.pos(d.goccyOffset(n))
		return &ast.BasicLit{
			ValuePos: pos.WithRel(token.Blank),
			Kind:     token.NULL,
			Value:    "null",
		}, nil
	default:
		return nil, fmt.Errorf("unknown yaml node type: %T", yn)
	}

	if err != nil {
		return nil, err
	}
	d.addCommentsToNode(expr, yn, 1)
	return expr, nil
}

// Comment handling

// extractGoccyComments converts a goccy CommentGroupNode to CUE comments.
func (d *decoder) extractGoccyComments(cg *gast.CommentGroupNode) []*ast.Comment {
	if cg == nil {
		return nil
	}
	var comments []*ast.Comment
	for _, c := range cg.Comments {
		text := c.Token.Value
		// Convert YAML # comment to CUE // comment.
		if strings.HasPrefix(text, "#") {
			text = text[1:]
		}
		comments = append(comments, &ast.Comment{
			Text: "//" + text,
		})
	}
	return comments
}

func (d *decoder) addHeadCommentsToPending(yn gast.Node) {
	// Head comments for MappingValueNodes are handled in insertMap.
	// Head comments for sequence entries are handled in sequence().
	// For other node types, we don't expect head comments from goccy.
}

func (d *decoder) addCommentsToNode(n ast.Node, yn gast.Node, linePos int8) {
	// Add any pending head comments.
	if comments := d.pendingHeadComments; len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Doc:      true,
			Position: 0,
			List:     comments,
		})
	}
	d.pendingHeadComments = nil

	// For scalar nodes that are NOT inside a mapping value (where insertMap
	// handles comments), add their own line comment.
	// Mapping value inline comments are handled by insertMap.
	// We only add comments here for top-level or sequence element scalars.
}

// Position helpers

// pos converts a byte offset to a cue/ast position.
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
		if offset < d.lastOffset {
			return token.NoPos
		}
	}
	d.lastOffset = offset
	return pos
}

// offsetLine returns a 1-indexed line number for the given byte offset.
func (d *decoder) offsetLine(offset int) int {
	return sort.Search(len(d.tokLines), func(i int) bool {
		return d.tokLines[i] > offset
	})
}

// findClosing scans forward from start in the source bytes to find
// the first occurrence of close that is not inside a quoted string or comment.
func (d *decoder) findClosing(start int, close byte) int {
	for i := start; i < len(d.src); i++ {
		switch d.src[i] {
		case close:
			return i
		case '"':
			for i++; i < len(d.src); i++ {
				if d.src[i] == '\\' {
					i++
				} else if d.src[i] == '"' {
					break
				}
			}
		case '\'':
			for i++; i < len(d.src); i++ {
				if d.src[i] == '\'' {
					if i+1 < len(d.src) && d.src[i+1] == '\'' {
						i++
					} else {
						break
					}
				}
			}
		case '#':
			if i == start || d.src[i-1] == ' ' || d.src[i-1] == '\t' {
				for i++; i < len(d.src) && d.src[i] != '\n'; i++ {
				}
			}
		}
	}
	return len(d.src)
}

// isBlankLine returns true if the 0-indexed line contains only whitespace.
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

// isCommentLine returns true if the 0-indexed line is a comment-only line.
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
// excluding any head comments and their surrounding blank lines.
func (d *decoder) scopeEndBefore(n gast.Node) int {
	line := goccyLine(n)
	end := d.tokLines[line-1]
	lineIdx := line - 2
	for lineIdx >= 0 && d.isBlankLine(lineIdx) {
		lineIdx--
	}
	for lineIdx >= 0 && d.isCommentLine(lineIdx) {
		lineIdx--
	}
	if lineIdx < line-2 {
		return d.tokLines[lineIdx+1]
	}
	return end
}

// Scalar extractors

func (d *decoder) stringNode(n *gast.StringNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))

	// goccy parses some values as StringNode that yaml.v3 treated as numbers:
	// - Integers too large for int64/uint64 (e.g. 18446744073709551616)
	// - Exponential notation without a dot (e.g. 123456e1)
	// For unquoted strings, check if they're valid CUE numbers.
	if n.Token.Type == gtoken.StringType {
		value := n.Value
		var info literal.NumInfo
		if err := literal.ParseNum(value, &info); err == nil {
			if info.IsInt() {
				// Integer too large for goccy's int64/uint64.
				// yaml.v3 would treat this as !!float, producing `number & <value>`.
				return d.makeNum(pos, fmt.Sprintf("number & %s", value), token.FLOAT), nil
			}
			// It's a valid float (e.g. 123456e1). Produce it directly.
			return d.makeNum(pos, value, token.FLOAT), nil
		}
	}

	return &ast.BasicLit{
		ValuePos: pos,
		Kind:     token.STRING,
		Value:    literal.String.WithOptionalTabIndent(1).Quote(n.Value),
	}, nil
}

func (d *decoder) literalNode(n *gast.LiteralNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	return &ast.BasicLit{
		ValuePos: pos,
		Kind:     token.STRING,
		Value:    literal.String.WithOptionalTabIndent(1).Quote(n.Value.Value),
	}, nil
}

func (d *decoder) integerNode(n *gast.IntegerNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	value := n.GetToken().Value
	// Convert YAML octal (0123) to CUE octal (0o123) if needed.
	// goccy already uses 0o prefix for YAML 1.2 octal, but
	// also parses YAML 1.1 octal (0123) as OctetInteger.
	if len(value) > 1 && value[0] == '0' && value[1] >= '0' && value[1] <= '9' {
		value = "0o" + value[1:]
	}
	return d.makeNum(pos, value, token.INT), nil
}

func (d *decoder) floatNode(n *gast.FloatNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	value := n.GetToken().Value
	return d.makeNum(pos, value, token.FLOAT), nil
}

func (d *decoder) boolNode(n *gast.BoolNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	lit := ast.NewBool(n.Value)
	lit.ValuePos = pos
	return lit, nil
}

func (d *decoder) nullNode(n *gast.NullNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	return &ast.BasicLit{
		ValuePos: pos.WithRel(token.Blank),
		Kind:     token.NULL,
		Value:    "null",
	}, nil
}

func (d *decoder) infinityNode(n *gast.InfinityNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	if n.Value < 0 {
		return d.makeNum(pos, "-Inf", token.FLOAT), nil
	}
	return d.makeNum(pos, "+Inf", token.FLOAT), nil
}

func (d *decoder) nanNode(n *gast.NanNode) (ast.Expr, error) {
	pos := d.pos(d.goccyOffset(n))
	return d.makeNum(pos, "NaN", token.FLOAT), nil
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

// tagged handles YAML nodes with explicit tags.
func (d *decoder) tagged(n *gast.TagNode) (ast.Expr, error) {
	tag := n.Start.Value
	inner := n.Value

	switch tag {
	case "!!timestamp":
		pos := d.pos(d.goccyOffset(n))
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.String.Quote(nodeStringValue(inner)),
		}, nil

	case "!!str":
		pos := d.pos(d.goccyOffset(n))
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.String.WithOptionalTabIndent(1).Quote(nodeStringValue(inner)),
		}, nil

	case "!!binary":
		pos := d.pos(d.goccyOffset(n))
		b64 := nodeStringValue(inner)
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: !!binary value contains invalid base64 data",
				d.tokFile.Name(), goccyLine(n))
		}
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.Bytes.Quote(string(data)),
		}, nil

	case "!!bool":
		pos := d.pos(d.goccyOffset(n))
		var t bool
		if bn, ok := inner.(*gast.BoolNode); ok {
			t = bn.Value
		} else {
			sv := strings.ToLower(nodeStringValue(inner))
			t = sv == "true" || sv == "yes" || sv == "on"
		}
		lit := ast.NewBool(t)
		lit.ValuePos = pos
		return lit, nil

	case "!!int":
		pos := d.pos(d.goccyOffset(n))
		value := inner.GetToken().Value
		if len(value) > 1 && value[0] == '0' && value[1] >= '0' && value[1] <= '9' {
			value = "0o" + value[1:]
		}
		var info literal.NumInfo
		if err := literal.ParseNum(value, &info); err != nil {
			return nil, fmt.Errorf("%s:%d: cannot decode %q as !!int: %v",
				d.tokFile.Name(), goccyLine(n), value, err)
		}
		if !info.IsInt() {
			return nil, fmt.Errorf("%s:%d: cannot decode %q as !!int: not an integer",
				d.tokFile.Name(), goccyLine(n), value)
		}
		return d.makeNum(pos, value, token.INT), nil

	case "!!float":
		pos := d.pos(d.goccyOffset(n))
		value := inner.GetToken().Value
		switch inner.(type) {
		case *gast.InfinityNode:
			inf := inner.(*gast.InfinityNode)
			if inf.Value < 0 {
				value = "-Inf"
			} else {
				value = "+Inf"
			}
		case *gast.NanNode:
			value = "NaN"
		default:
			// Convert YAML 1.1 octal to CUE octal.
			if len(value) > 1 && value[0] == '0' && value[1] >= '0' && value[1] <= '9' {
				value = "0o" + value[1:]
			}
			if strings.IndexAny(value, ".eEiInN") == -1 {
				value = fmt.Sprintf("number & %s", value)
			}
		}
		return d.makeNum(pos, value, token.FLOAT), nil

	case "!!null":
		pos := d.pos(d.goccyOffset(n))
		return &ast.BasicLit{
			ValuePos: pos.WithRel(token.Blank),
			Kind:     token.NULL,
			Value:    "null",
		}, nil

	case "!!seq":
		return d.extract(inner)

	case "!!map":
		return d.extract(inner)

	case "!":
		// Non-specific tag: just extract the inner value as-is.
		return d.extract(inner)

	default:
		// For unknown tags (including custom and resolved tags like !y!int),
		// try to extract the inner value.
		return d.extract(inner)
	}
}

// nodeStringValue extracts a string value from any scalar node.
func nodeStringValue(n gast.Node) string {
	switch n := n.(type) {
	case *gast.StringNode:
		return n.Value
	case *gast.LiteralNode:
		return n.Value.Value
	case *gast.IntegerNode:
		return n.GetToken().Value
	case *gast.FloatNode:
		return n.GetToken().Value
	case *gast.BoolNode:
		return n.GetToken().Value
	case *gast.NullNode:
		return "null"
	case *gast.InfinityNode:
		return n.GetToken().Value
	case *gast.NanNode:
		return n.GetToken().Value
	default:
		return fmt.Sprintf("%v", n)
	}
}

// Container handlers

func (d *decoder) mapping(yn *gast.MappingNode) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd

	// Compute Lbrace position.
	var startOffset int
	if yn.IsFlowStyle && yn.Start != nil {
		startOffset = d.tokenOffset(yn.Start)
	} else if len(yn.Values) > 0 {
		// For block style, use the first key's position.
		startOffset = d.goccyOffset(yn.Values[0].Key.(gast.Node))
	} else {
		startOffset = d.goccyOffset(yn)
	}

	strct := &ast.StructLit{
		Lbrace: d.tokFile.Pos(startOffset, token.Blank),
	}

	multiline := false
	if len(yn.Values) > 0 {
		firstLine := goccyLine(yn.Values[0].Key.(gast.Node))
		lastMV := yn.Values[len(yn.Values)-1]
		lastLine := goccyLine(lastMV)
		multiline = firstLine < lastLine
	}

	if err := d.insertMap(yn.Values, strct, multiline, false, parentScopeEnd); err != nil {
		return nil, err
	}

	if yn.IsFlowStyle && yn.End != nil {
		rbraceOff := d.tokenOffset(yn.End)
		d.lastOffset = rbraceOff + 1
		strct.Rbrace = d.tokFile.Pos(rbraceOff, token.Blank)
	} else if len(yn.Values) > 0 {
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

func (d *decoder) singleMappingValue(n *gast.MappingValueNode) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd
	startOffset := d.goccyOffset(n.Key.(gast.Node))
	strct := &ast.StructLit{
		Lbrace: d.tokFile.Pos(startOffset, token.Blank),
	}
	if err := d.insertMap([]*gast.MappingValueNode{n}, strct, false, false, parentScopeEnd); err != nil {
		return nil, err
	}
	strct.Rbrace = strct.Lbrace
	return strct, nil
}

func (d *decoder) insertMap(values []*gast.MappingValueNode, m *ast.StructLit, multiline, mergeValues bool, parentScopeEnd int) error {
outer:
	for i, mv := range values {
		if multiline {
			d.forceNewline = true
		}

		// Check for merge key.
		if mv.Key.IsMergeKey() {
			mergeValues = true
			if err := d.merge(mv.Value, m, multiline); err != nil {
				return err
			}
			continue
		}

		// Handle head comments from MappingValueNode.
		// In goccy, MV.GetComment() holds comments that appear before this entry's key.
		// These comments might be:
		//  (a) Head comments for this entry (adjacent to the key)
		//  (b) Foot comments from the previous entry that goccy attached here
		//
		// We split them based on blank lines: comments that are separated from the
		// previous content by a blank line but adjacent to each other form a group.
		// If the entire comment block is separated from the key by a blank line,
		// it belongs to the previous entry.
		if cg := mv.GetComment(); cg != nil {
			comments := d.extractGoccyComments(cg)
			if len(comments) > 0 {
				firstCommentLine := 0
				lastCommentLine := 0
				if len(cg.Comments) > 0 {
					if pos := cg.Comments[0].Token.Position; pos != nil {
						firstCommentLine = pos.Line
					}
					if pos := cg.Comments[len(cg.Comments)-1].Token.Position; pos != nil {
						lastCommentLine = pos.Line
					}
				}
				// Determine section break for first comment.
				if len(d.pendingHeadComments) == 0 && firstCommentLine > 0 {
					if d.lastOffset >= 0 && firstCommentLine-d.offsetLine(d.lastOffset) >= 2 {
						comments[0].Slash = comments[0].Slash.WithRel(token.NewSection)
					}
				}

				// Check if there's a blank line between the comment block and
				// this entry's key. If so, and the comment is adjacent to the
				// previous entry, attach it to the previous entry.
				_ = lastCommentLine // used for future refinement
				d.pendingHeadComments = append(d.pendingHeadComments, comments...)
			}
		}

		field := &ast.Field{}
		label, err := d.label(mv.Key)
		if err != nil {
			return err
		}

		// Flush pending head comments as doc comments on this field.
		if comments := d.pendingHeadComments; len(comments) > 0 {
			ast.AddComment(field, &ast.CommentGroup{
				Doc:      true,
				Position: 0,
				List:     comments,
			})
		}
		d.pendingHeadComments = nil

		field.Label = label

		// Set scope end for value extraction.
		if i+1 < len(values) {
			d.scopeEnd = d.scopeEndBefore(values[i+1].Key.(gast.Node))
		} else {
			d.scopeEnd = parentScopeEnd
		}

		if mergeValues {
			key := labelStr(label)
			for _, decl := range m.Elts {
				f := decl.(*ast.Field)
				name, _, err := ast.LabelName(f.Label)
				if err == nil && name == key {
					f.Value, err = d.extract(mv.Value)
					if err != nil {
						return err
					}
					continue outer
				}
			}
		}

		value, err := d.extract(mv.Value)
		if err != nil {
			return err
		}
		field.Value = value

		// Add inline comment from the value node.
		if valueCG := mv.Value.GetComment(); valueCG != nil {
			lineComments := d.extractGoccyComments(valueCG)
			if len(lineComments) > 0 {
				// Position 4 puts the comment after the entire field value.
				ast.AddComment(value, &ast.CommentGroup{
					Line:     true,
					Position: 1,
					List:     lineComments,
				})
			}
		}

		// Add foot comment from the MappingValueNode.
		// Foot comments appear after this entry and before the next.
		// They become pending head comments or trailing struct comments.
		if mv.FootComment != nil {
			footComments := d.extractGoccyComments(mv.FootComment)
			if i+1 < len(values) {
				// More entries follow: foot comments become head comments for later.
				d.pendingHeadComments = append(d.pendingHeadComments, footComments...)
			} else {
				// Last entry: foot comments go after the struct.
				if len(footComments) > 0 {
					ast.AddComment(m, &ast.CommentGroup{
						Position: 100,
						List:     footComments,
					})
				}
			}
		}

		m.Elts = append(m.Elts, field)
	}
	return nil
}

func (d *decoder) label(key gast.MapKeyNode) (ast.Label, error) {
	node := key.(gast.Node)
	pos := d.pos(d.goccyOffset(node))

	var value string
	switch k := key.(type) {
	case *gast.StringNode:
		value = k.Value
	case *gast.LiteralNode:
		value = k.Value.Value
	case *gast.IntegerNode:
		// Non-string scalar used as key; normalize to its string representation.
		value = k.GetToken().Value
	case *gast.FloatNode:
		value = k.GetToken().Value
	case *gast.BoolNode:
		// Normalize to lowercase.
		if k.Value {
			value = "true"
		} else {
			value = "false"
		}
	case *gast.NullNode:
		value = "null"
	case *gast.InfinityNode:
		// .Inf as a key: use the CUE representation.
		if k.Value > 0 {
			value = "+Inf"
		} else {
			value = "-Inf"
		}
	case *gast.NanNode:
		value = "NaN"
	case *gast.AliasNode:
		// Alias used as a map key: resolve the alias and use its value.
		name := k.Value.GetToken().Value
		target, ok := d.anchors[name]
		if !ok {
			return nil, fmt.Errorf("%s:%d: unknown anchor %q", d.tokFile.Name(), goccyLine(node), name)
		}
		return d.label(target.(gast.MapKeyNode))
	default:
		return nil, fmt.Errorf("%s:%d: invalid map key type: %T",
			d.tokFile.Name(), goccyLine(node), key)
	}

	label := ast.NewStringLabel(value)
	ast.SetPos(label, pos)
	return label, nil
}

func (d *decoder) sequence(yn *gast.SequenceNode) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd

	var startOffset int
	if yn.IsFlowStyle && yn.Start != nil {
		startOffset = d.tokenOffset(yn.Start)
	} else {
		startOffset = d.goccyOffset(yn)
	}

	list := &ast.ListLit{
		Lbrack: d.tokFile.Pos(startOffset, token.Blank),
	}

	// Advance lastOffset past the sequence start.
	if startOffset >= d.lastOffset {
		d.lastOffset = startOffset
	}

	multiline := false
	if len(yn.Values) > 0 {
		firstLine := goccyLine(yn)
		lastLine := goccyLine(yn.Values[len(yn.Values)-1])
		multiline = firstLine < lastLine
	}

	closeSameLine := true
	for i, val := range yn.Values {
		d.forceNewline = multiline

		// Handle head comments from sequence entries.
		if i < len(yn.Entries) {
			entry := yn.Entries[i]
			if entry.HeadComment != nil {
				comments := d.extractGoccyComments(entry.HeadComment)
				d.pendingHeadComments = append(d.pendingHeadComments, comments...)
			}
		}

		// Set scope end for element.
		if i+1 < len(yn.Values) {
			d.scopeEnd = d.scopeEndBefore(yn.Values[i+1])
		} else {
			d.scopeEnd = parentScopeEnd
		}

		elem, err := d.extract(val)
		if err != nil {
			return nil, err
		}

		// Add line comments from sequence entries.
		if i < len(yn.Entries) {
			entry := yn.Entries[i]
			if entry.LineComment != nil {
				lineComments := d.extractGoccyComments(entry.LineComment)
				if len(lineComments) > 0 {
					ast.AddComment(elem, &ast.CommentGroup{
						Line:     true,
						Position: 1,
						List:     lineComments,
					})
				}
			}
		}

		list.Elts = append(list.Elts, elem)
		_, closeSameLine = elem.(*ast.StructLit)
	}

	if yn.IsFlowStyle && yn.End != nil {
		rbrackOff := d.tokenOffset(yn.End)
		d.lastOffset = rbrackOff + 1
		list.Rbrack = d.tokFile.Pos(rbrackOff, token.Blank)
	} else if len(yn.Values) > 0 {
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

func (d *decoder) alias(yn *gast.AliasNode) (ast.Expr, error) {
	name := yn.Value.GetToken().Value
	if d.extractingAliases[name] {
		return nil, fmt.Errorf("anchor %q value contains itself", name)
	}
	target, ok := d.anchors[name]
	if !ok {
		return nil, fmt.Errorf("unknown anchor %q", name)
	}

	if d.extractingAliases == nil {
		d.extractingAliases = make(map[string]bool)
	}
	d.extractingAliases[name] = true

	// Save and reset decoder state so the alias extraction doesn't
	// interfere with the outer position tracking.
	savedLastOffset := d.lastOffset
	savedForceNewline := d.forceNewline
	savedScopeEnd := d.scopeEnd
	d.lastOffset = -1
	d.forceNewline = false

	node, err := d.extract(target)

	d.lastOffset = savedLastOffset
	d.forceNewline = savedForceNewline
	d.scopeEnd = savedScopeEnd
	delete(d.extractingAliases, name)

	if err != nil {
		return nil, err
	}

	// Override brace/bracket positions for container types to the alias site.
	aliasStart := d.goccyOffset(yn)
	aliasEnd := aliasStart + 1 + len(name) // *name
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

func (d *decoder) merge(yn gast.Node, m *ast.StructLit, multiline bool) error {
	// Unwrap anchor nodes.
	if anchor, ok := yn.(*gast.AnchorNode); ok {
		if d.anchors == nil {
			d.anchors = make(map[string]gast.Node)
		}
		name := anchor.Name.GetToken().Value
		d.anchors[name] = anchor.Value
		yn = anchor.Value
	}

	switch n := yn.(type) {
	case *gast.MappingNode:
		return d.insertMap(n.Values, m, multiline, true, d.scopeEnd)
	case *gast.AliasNode:
		name := n.Value.GetToken().Value
		target, ok := d.anchors[name]
		if !ok {
			return fmt.Errorf("unknown anchor %q in merge", name)
		}
		// Unwrap anchor if the target is an anchor node.
		if anchor, ok := target.(*gast.AnchorNode); ok {
			target = anchor.Value
		}
		if mapping, ok := target.(*gast.MappingNode); ok {
			return d.insertMap(mapping.Values, m, multiline, true, d.scopeEnd)
		}
		return fmt.Errorf("map merge requires map as the value")
	case *gast.SequenceNode:
		// Merge sequence of maps in reverse order (earlier takes precedence).
		for i := len(n.Values) - 1; i >= 0; i-- {
			if err := d.merge(n.Values[i], m, multiline); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("map merge requires map or sequence of maps as the value")
	}
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
