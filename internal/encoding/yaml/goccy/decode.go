// Package goccy converts YAML to and from CUE syntax trees using
// github.com/goccy/go-yaml. It is the implementation selected by the
// yamlgoccy experiment, reached via
// [cuelang.org/go/internal/encoding/yaml].
package goccy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	yast "github.com/goccy/go-yaml/ast"
	ylexer "github.com/goccy/go-yaml/lexer"
	yparser "github.com/goccy/go-yaml/parser"
	ytoken "github.com/goccy/go-yaml/token"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// TODO(mvdan): we should sanity check that the decoder always produces valid CUE,
// as it is possible to construct a cue/ast syntax tree with invalid literals
// or with expressions that will always error, such as `float & 123`.
//
// TestUnmarshal compiles and validates every decoded result, which catches
// invalid literals; note that YAML infinities and NaN still decode to `+Inf`,
// `-Inf`, and `NaN`, identifiers which do not exist in CUE.
// A more general approach may be fuzzing, which would find more bugs and work
// for any decoder, although it may be slow as we need to involve the evaluator.

// Decoder decodes the documents of a YAML stream into CUE expressions.
type Decoder interface {
	// Decode consumes a YAML value and returns it in CUE syntax tree node.
	Decode() (ast.Expr, error)
}

// decoder converts YAML documents to CUE syntax tree nodes using the
// goccy/go-yaml tokenizer and parser.
//
// The input is tokenized once up front and split into one token segment
// per YAML document. Each segment is parsed on demand by Decode. This
// preserves the streaming semantics of a per-document decoder: documents
// preceding a syntax error are decoded successfully, and the error is
// only reported once the failing document is reached.
type decoder struct {
	// segments holds one token slice per YAML document, split on
	// document markers. See splitDocuments.
	segments []ytoken.Tokens
	segIdx   int

	// docs holds the documents of the most recently parsed segment
	// that have not yet been returned by Decode.
	docs   []*yast.DocumentNode
	docIdx int

	// yamlNonEmpty is true once a document with content has been seen.
	// Useful so that we can extract "null" when the input is empty.
	yamlNonEmpty bool

	// decodeErr is returned by any further calls to Decode when not nil.
	decodeErr error

	// lenient makes Decode tolerate errors instead of terminating the
	// stream: a document with a syntax error is re-parsed up to the
	// error's position, a document that fails to extract is skipped,
	// and the errors are collected in errs. Used by ExtractLenient.
	lenient bool
	errs    []error

	// tagHandles records the tag shorthands declared by %TAG directives,
	// mapping a handle such as "!e!" to its prefix.
	tagHandles map[string]string

	src      []byte
	tokFile  *token.File
	tokLines []int

	// pendingHeadComments collects the head (preceding) comments
	// from the YAML nodes we are extracting.
	// We can't add comments to a CUE syntax tree node until we've created it,
	// but we need to extract these comments first since they have earlier positions.
	pendingHeadComments []*ast.Comment

	// commentRuns memoizes splitCommentRuns per comment group.
	commentRuns map[*yast.CommentGroupNode][]commentRun

	// anchors maps anchor names to their YAML nodes. goccy does not
	// resolve aliases, so we do it ourselves as we walk the tree,
	// which also matches YAML's define-before-use semantics.
	anchors map[string]yast.Node

	// extractingAliases ensures we don't loop forever when expanding YAML anchors.
	extractingAliases map[string]bool

	// aliasDepth is positive while we are expanding an alias, so that we
	// only count the nodes it produces towards [decoder.aliasNodes].
	aliasDepth int

	// aliasNodes counts the nodes produced by alias expansion. See [maxAliasNodes].
	aliasNodes int

	// lastOffset is byte offset from the last yaml node position that
	// we decoded, used for working out relative positions such as
	// token.NewSection. This offset can only increase, moving forward
	// in the file. A value of -1 means no position has been recorded
	// yet.
	lastOffset int

	// forceNewline ensures that the next position will be on a new line.
	forceNewline bool

	// forceInline ensures that the next position will be on the same
	// line, overriding both forceNewline and the line-delta
	// heuristic. Set for the first field of a single-line flow-style
	// mapping `{a: 1, b: 2}`, whose first field sits on a different
	// source line than the preceding sibling but must render inline.
	forceInline bool

	// scopeEnd is the byte offset (exclusive) bounding the current
	// node's extent in the source. Used to compute Rbrace positions
	// for struct literals: the Rbrace is placed at offset scopeEnd-1
	// (typically the \n ending the last line before the next content).
	scopeEnd int

	// colCache memoizes the last rune-column to byte-offset
	// conversion by [decoder.lineColOffset]. goccy columns count
	// runes, so the conversion walks the line's bytes; resuming from
	// the previous lookup keeps repeated lookups on the same line
	// linear.
	colCache struct {
		line, column, offset int
	}
}

// NewDecoder creates a decoder for YAML values to extract CUE syntax tree nodes.
//
// The filename is used for position information in CUE syntax tree nodes
// as well as any errors encountered while decoding YAML.
func NewDecoder(filename string, b []byte) *decoder {
	// Note that we add an extra byte to the file size as some positions
	// may point just past the end of the input in edge cases.
	tokFile := token.NewFile(filename, 0, len(b)+1)
	tokFile.SetLinesForContent(b)
	tokens := normalizeMergeKeys(ylexer.Tokenize(string(b)))
	return &decoder{
		src:        b,
		tokFile:    tokFile,
		tokLines:   append(tokFile.Lines(), len(b)),
		segments:   splitDocuments(tokens),
		lastOffset: -1,
		scopeEnd:   len(b),
	}
}

// normalizeMergeKeys rewrites explicitly tagged merge keys such as
// `!!merge "<<"` or `!<tag:yaml.org,2002:merge> <<` into a plain merge
// key token. goccy cannot parse all such forms, and those it can parse
// produce a tag node where the merge logic expects a merge key node.
// Tags are resolved through any %TAG directive handles, tracked here at
// the token level with the same per-document scope as the decoder, so
// that a shorthand for the merge tag is rewritten and a redefined "!!"
// handle is not.
//
// TODO: this rewrite could become unnecessary if goccy learns to parse
// all tagged merge key forms.
func normalizeMergeKeys(tokens ytoken.Tokens) ytoken.Tokens {
	out := tokens[:0]
	// pending holds the handles declared by %TAG directives ahead of a
	// "---" header; active holds the ones applying to the current
	// document.
	var pending, active map[string]string
	for i := 0; i < len(tokens); i++ {
		tk := tokens[i]
		switch tk.Type {
		case ytoken.DirectiveType:
			if i+3 < len(tokens) && tokens[i+1].Value == "TAG" {
				if pending == nil {
					pending = make(map[string]string)
				}
				pending[tokens[i+2].Value] = tokens[i+3].Value
			}
		case ytoken.DocumentHeaderType:
			active, pending = pending, nil
		case ytoken.DocumentEndType:
			active = nil
		case ytoken.TagType:
			isMergeTag := resolveTagWith(active, tk.Value) == "!!merge" ||
				tk.Value == "!<tag:yaml.org,2002:merge>"
			if isMergeTag && i+2 < len(tokens) &&
				tokens[i+2].Type == ytoken.MappingValueType {
				next := tokens[i+1]
				isMerge := next.Type == ytoken.MergeKeyType
				switch next.Type {
				case ytoken.StringType, ytoken.SingleQuoteType, ytoken.DoubleQuoteType:
					isMerge = next.Value == "<<"
				}
				if isMerge {
					// Position the merge key where the tag began so that the
					// parser sees the mapping key at the right indentation.
					out = append(out, ytoken.MergeKey(tk.Origin, tk.Position))
					i++ // skip the "<<" token as well
					continue
				}
			}
		}
		out = append(out, tk)
	}
	return out
}

// splitDocuments splits a lexed token stream into one segment per YAML
// document so that each document can be parsed independently. A "---"
// header starts a new document when the current segment already has
// content or a header of its own; %TAG and %YAML directive lines and
// comments belong to the document that follows them. A "..." document
// end marker terminates the current segment.
func splitDocuments(tokens ytoken.Tokens) []ytoken.Tokens {
	var segments []ytoken.Tokens
	var cur ytoken.Tokens
	contentSeen := false
	headerSeen := false
	directiveLine := -1
	flush := func() {
		if len(cur) > 0 {
			segments = append(segments, cur)
			cur = nil
		}
		contentSeen = false
		headerSeen = false
	}
	for _, tk := range tokens {
		switch tk.Type {
		case ytoken.DocumentHeaderType:
			if contentSeen || headerSeen {
				flush()
			}
			headerSeen = true
			cur = append(cur, tk)
		case ytoken.DocumentEndType:
			cur = append(cur, tk)
			flush()
		case ytoken.DirectiveType:
			if tk.Position != nil {
				directiveLine = tk.Position.Line
			}
			cur = append(cur, tk)
		case ytoken.CommentType:
			cur = append(cur, tk)
		default:
			if tk.Position == nil || tk.Position.Line != directiveLine {
				contentSeen = true
			}
			cur = append(cur, tk)
		}
	}
	flush()
	return segments
}

// parseTokens parses one document segment. goccy cannot parse a flow
// collection in which a comment line immediately precedes the closing
// bracket or brace, such as `[\n# c\n]`, or sits between two flow
// mapping entries; see https://github.com/goccy/go-yaml/issues/821 and
// https://github.com/goccy/go-yaml/issues/903. When the parse error
// involves such a comment we retry with comment tokens inside flow
// collections removed, and then with all comment tokens removed,
// dropping those comments.
//
// TODO: the retries could be removed once the goccy issues above are
// fixed.
func parseTokens(toks ytoken.Tokens) (*yast.File, error) {
	file, err := yparser.Parse(toks, yparser.ParseComments, yparser.AllowDuplicateMapKey())
	if err == nil {
		return file, nil
	}
	// Only retry when the error plausibly involves a comment inside a
	// flow collection, so that unrelated parse failures are not
	// silently "fixed" by dropping the document's comments.
	if !errorInvolvesFlowComment(toks, err) {
		return nil, err
	}
	inFlow := stripComments(toks, true)
	if len(inFlow) < len(toks) {
		if file, err2 := yparser.Parse(inFlow, yparser.ParseComments, yparser.AllowDuplicateMapKey()); err2 == nil {
			return file, nil
		}
	}
	all := stripComments(toks, false)
	if len(all) < len(inFlow) {
		if file, err2 := yparser.Parse(all, 0, yparser.AllowDuplicateMapKey()); err2 == nil {
			return file, nil
		}
	}
	return nil, err
}

// errorInvolvesFlowComment reports whether the parse error's token is a
// comment or sits inside a flow collection, the failure modes seen when
// goccy parses comments inside flow collections.
func errorInvolvesFlowComment(toks ytoken.Tokens, err error) bool {
	var perr interface{ GetToken() *ytoken.Token }
	if !errors.As(err, &perr) {
		return false
	}
	tk := perr.GetToken()
	if tk == nil {
		return false
	}
	if tk.Type == ytoken.CommentType {
		return true
	}
	depth := 0
	for _, t := range toks {
		if t == tk {
			return depth > 0
		}
		switch t.Type {
		case ytoken.SequenceStartType, ytoken.MappingStartType:
			depth++
		case ytoken.SequenceEndType, ytoken.MappingEndType:
			depth--
		}
	}
	return false
}

// parseTokenPrefix handles a document segment which failed to parse:
// it parses the segment's longest token prefix preceding the error's
// position, resulting in a best-effort partial document for input that
// is invalid mid-edit. It returns nil when no usable prefix exists or
// when the prefix does not parse either.
func parseTokenPrefix(seg ytoken.Tokens, err error) *yast.File {
	var perr interface{ GetToken() *ytoken.Token }
	if !errors.As(err, &perr) {
		return nil
	}
	tk := perr.GetToken()
	if tk == nil || tk.Position == nil {
		return nil
	}
	var prefix ytoken.Tokens
	for _, t := range seg {
		if t == tk || t.Position == nil || !positionBefore(t.Position, tk.Position) {
			break
		}
		prefix = append(prefix, t)
	}
	if len(prefix) == 0 {
		return nil
	}
	file, err := parseTokens(prefix)
	if err != nil {
		return nil
	}
	return file
}

// positionBefore reports whether position a is strictly before b.
func positionBefore(a, b *ytoken.Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Column < b.Column)
}

// stripComments removes comment tokens, either inside flow collections
// only, or everywhere.
func stripComments(toks ytoken.Tokens, onlyFlow bool) ytoken.Tokens {
	depth := 0
	out := make(ytoken.Tokens, 0, len(toks))
	for _, tk := range toks {
		switch tk.Type {
		case ytoken.SequenceStartType, ytoken.MappingStartType:
			depth++
		case ytoken.SequenceEndType, ytoken.MappingEndType:
			depth--
		case ytoken.CommentType:
			if !onlyFlow || depth > 0 {
				continue
			}
		}
		out = append(out, tk)
	}
	return out
}

// Decode consumes a YAML value and returns it in CUE syntax tree node.
//
// A nil node with an io.EOF error is returned once no more YAML values
// are available for decoding.
func (d *decoder) Decode() (ast.Expr, error) {
	if err := d.decodeErr; err != nil {
		return nil, err
	}
	for {
		for d.docIdx < len(d.docs) {
			doc := d.docs[d.docIdx]
			d.docIdx++
			// Anchors are scoped to the document that defines them; an
			// alias cannot reference an anchor from an earlier document.
			d.anchors = nil
			switch body := doc.Body.(type) {
			case nil:
				// An empty document, such as a bare "---".
				d.yamlNonEmpty = true
				return d.nullExpr(doc), nil
			case *yast.DirectiveNode:
				if err := d.directive(body); err != nil {
					if d.lenient {
						d.errs = append(d.errs, err)
						continue
					}
					d.decodeErr = err
					return nil, err
				}
			case *yast.CommentGroupNode:
				// A document holding only comments, e.g. preceding a
				// "---" header; carry them over to the next value.
				d.addPendingGroup(body)
			default:
				d.yamlNonEmpty = true
				expr, err := d.extract(body)
				if err != nil {
					if d.lenient {
						// Skip just this document; keep decoding.
						d.errs = append(d.errs, err)
						continue
					}
					d.decodeErr = err
					return nil, err
				}
				return expr, nil
			}
		}
		if d.segIdx >= len(d.segments) {
			d.decodeErr = io.EOF
			if !d.yamlNonEmpty {
				// If the input is empty, we produce `*null | _` followed by EOF.
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
			return nil, io.EOF
		}
		seg := d.segments[d.segIdx]
		d.segIdx++
		// %TAG directives apply only to the single document that follows
		// them, and segments are split per document.
		d.tagHandles = nil
		file, err := parseTokens(seg)
		if err != nil {
			if !d.lenient {
				// Any further Decode calls repeat this error.
				err = d.wrapParseError(err)
				d.decodeErr = err
				return nil, err
			}
			d.errs = append(d.errs, d.wrapParseError(err))
			// Re-parse the longest token prefix preceding the error,
			// giving a best-effort partial document; any following
			// documents are still decoded.
			file = parseTokenPrefix(seg, err)
			if file == nil {
				continue
			}
		}
		d.docs, d.docIdx = file.Docs, 0
	}
}

// wrapParseError converts a goccy parse error to one prefixed by
// filename and line number.
func (d *decoder) wrapParseError(err error) error {
	var perr interface {
		GetToken() *ytoken.Token
		GetMessage() string
	}
	if errors.As(err, &perr) {
		if tk := perr.GetToken(); tk != nil && tk.Position != nil {
			return fmt.Errorf("%s:%d: %s", d.tokFile.Name(), tk.Position.Line, perr.GetMessage())
		}
		return fmt.Errorf("%s: %s", d.tokFile.Name(), perr.GetMessage())
	}
	return fmt.Errorf("%s: %v", d.tokFile.Name(), err)
}

// nullExpr returns the null literal used for empty YAML documents.
func (d *decoder) nullExpr(doc *yast.DocumentNode) ast.Expr {
	offset := 0
	if doc.Start != nil {
		offset = d.tokenOffset(doc.Start)
	}
	return d.nullLit(d.pos(offset))
}

// directive records the tag shorthands declared by a %TAG directive.
// Other directives, such as %YAML, are ignored.
func (d *decoder) directive(n *yast.DirectiveNode) error {
	if name := n.Name.GetToken(); name == nil || name.Value != "TAG" || len(n.Values) != 2 {
		return nil
	}
	handle := n.Values[0].GetToken().Value
	prefix := n.Values[1].GetToken().Value
	if !validTagHandle(handle) {
		return d.posErrorf(n, "invalid tag directive handle %q", handle)
	}
	if d.tagHandles == nil {
		d.tagHandles = make(map[string]string)
	}
	d.tagHandles[handle] = prefix
	return nil
}

// validTagHandle reports whether s is a valid %TAG directive handle:
// "!", "!!", or "!" followed by word characters and a final "!".
func validTagHandle(s string) bool {
	if len(s) < 1 || s[0] != '!' {
		return false
	}
	if s == "!" || s == "!!" {
		return true
	}
	if s[len(s)-1] != '!' {
		return false
	}
	for _, c := range s[1 : len(s)-1] {
		switch {
		case c >= '0' && c <= '9', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '-':
		default:
			return false
		}
	}
	return true
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

// registerAnchor records value as the content anchored by n, to be
// resolved by later aliases. The value is usually n.Value, but a tagged
// anchor such as `!!str &x hi` records the enclosing tag node.
func (d *decoder) registerAnchor(n *yast.AnchorNode, value yast.Node) {
	if d.anchors == nil {
		d.anchors = make(map[string]yast.Node)
	}
	d.anchors[n.Name.GetToken().Value] = value
}

func (d *decoder) extract(yn yast.Node) (ast.Expr, error) {
	// Reject documents whose aliases expand into too large a tree; nodes
	// outside an alias map one-to-one to the input, so are not counted.
	if d.aliasDepth > 0 {
		d.aliasNodes++
		if d.aliasNodes > maxAliasNodes {
			return nil, d.posErrorf(yn, "aliases expand to more than %d nodes", maxAliasNodes)
		}
	}
	switch n := yn.(type) {
	case *yast.AnchorNode:
		d.registerAnchor(n, n.Value)
		expr, err := d.extract(n.Value)
		if err != nil {
			return nil, err
		}
		d.adjustAnchoredStart(n, expr)
		return expr, nil
	case *yast.TagNode:
		return d.tagged(n)
	}
	lineComments := d.nodeComments(yn)
	var expr ast.Expr
	var err error
	switch n := yn.(type) {
	case *yast.MappingNode:
		expr, err = d.mapping(n.Values, n.IsFlowStyle, n.Start, n.End)
	case *yast.MappingValueNode:
		expr, err = d.mapping([]*yast.MappingValueNode{n}, false, nil, nil)
	case *yast.SequenceNode:
		expr, err = d.sequence(n)
	case *yast.StringNode:
		expr, err = d.scalarString(n)
	case *yast.LiteralNode:
		expr = d.quotedString(d.pos(d.nodeOffset(n)), n.Value.Value)
	case *yast.IntegerNode:
		expr, err = d.integer(n)
	case *yast.FloatNode:
		expr, err = d.float(n)
	case *yast.BoolNode:
		lit := ast.NewBool(n.Value)
		lit.ValuePos = d.pos(d.nodeOffset(n))
		expr = lit
	case *yast.NullNode:
		expr = d.nullLit(d.pos(d.nodeOffset(n)))
	case *yast.InfinityNode:
		expr = d.makeNum(d.pos(d.nodeOffset(n)), infString(n), token.FLOAT)
	case *yast.NanNode:
		expr = d.makeNum(d.pos(d.nodeOffset(n)), "NaN", token.FLOAT)
	case *yast.AliasNode:
		expr, err = d.alias(n)
	default:
		return nil, d.posErrorf(yn, "unsupported YAML node type %s", yn.Type())
	}
	if err != nil {
		return nil, err
	}
	d.flushComments(expr, lineComments, 1)
	return expr, nil
}

// commentRun is a contiguous run of comment-only lines.
type commentRun struct {
	comments           []*ast.Comment
	startLine, endLine int
}

// splitCommentRuns converts a goccy comment group into runs of
// contiguous comment lines, translating each comment to CUE form.
func (d *decoder) splitCommentRuns(cg *yast.CommentGroupNode) []commentRun {
	if cg == nil {
		return nil
	}
	// Memoize the result: the lookaheads in entryHasHeadComments and
	// nodeHasHeadComments split a comment group ahead of the pass that
	// actually attaches it.
	if runs, ok := d.commentRuns[cg]; ok {
		return runs
	}
	var runs []commentRun
	var cur commentRun
	for _, c := range cg.Comments {
		tk := c.Token
		if tk == nil {
			continue
		}
		line := 0
		if tk.Position != nil {
			line = tk.Position.Line
		}
		// The token value carries the comment text without the leading
		// "#". The position points at the "#".
		cmt := &ast.Comment{
			Slash: d.tokFile.Pos(d.tokenOffset(tk), token.NoRelPos),
			Text:  "//" + tk.Value,
		}
		if len(cur.comments) > 0 && line == cur.endLine+1 {
			cur.comments = append(cur.comments, cmt)
			cur.endLine = line
		} else {
			if len(cur.comments) > 0 {
				runs = append(runs, cur)
			}
			cur = commentRun{comments: []*ast.Comment{cmt}, startLine: line, endLine: line}
		}
	}
	if len(cur.comments) > 0 {
		runs = append(runs, cur)
	}
	if d.commentRuns == nil {
		d.commentRuns = make(map[*yast.CommentGroupNode][]commentRun)
	}
	d.commentRuns[cg] = runs
	return runs
}

// groupComments returns all the comments of a goccy comment group,
// converted to CUE form.
func (d *decoder) groupComments(cg *yast.CommentGroupNode) []*ast.Comment {
	var comments []*ast.Comment
	for _, run := range d.splitCommentRuns(cg) {
		comments = append(comments, run.comments...)
	}
	return comments
}

// addPendingGroup adds all of a comment group's runs to the pending
// head comments.
func (d *decoder) addPendingGroup(cg *yast.CommentGroupNode) {
	for _, run := range d.splitCommentRuns(cg) {
		d.addPendingRun(run)
	}
}

// nodeComments processes the comments attached to a YAML node: comments
// on lines preceding the node become pending head comments, and comments
// on the node's own line are returned so they can be attached as line
// comments once the CUE node exists.
func (d *decoder) nodeComments(yn yast.Node) (lineComments []*ast.Comment) {
	cg := yn.GetComment()
	if cg == nil {
		return nil
	}
	nodeLine := goccyLine(yn)
	for _, run := range d.splitCommentRuns(cg) {
		if run.startLine >= nodeLine {
			lineComments = append(lineComments, run.comments...)
		} else {
			d.addPendingRun(run)
		}
	}
	return lineComments
}

// addPendingRun adds a run of head comments to the pending list,
// starting a new section when the run is separated from earlier content
// by a blank line.
func (d *decoder) addPendingRun(run commentRun) {
	if len(d.pendingHeadComments) == 0 && d.runStartsSection(run) {
		c := run.comments[0]
		c.Slash = c.Slash.WithRel(token.NewSection)
	}
	d.pendingHeadComments = append(d.pendingHeadComments, run.comments...)
}

// runStartsSection reports whether a head comment run is separated by at
// least one blank line from content earlier in the file.
func (d *decoder) runStartsSection(run commentRun) bool {
	lineIdx := run.startLine - 2 // 0-indexed line above the run
	if lineIdx < 0 || lineIdx >= len(d.tokLines)-1 || !d.isBlankLine(lineIdx) {
		return false
	}
	for ; lineIdx >= 0; lineIdx-- {
		if !d.isBlankLine(lineIdx) {
			return true
		}
	}
	return false // reached the start of the file: not a section break
}

// flushComments attaches the pending head comments and the given line
// comments, placed after the token at linePos, to a CUE node.
func (d *decoder) flushComments(n ast.Node, lineComments []*ast.Comment, linePos int8) {
	if comments := d.pendingHeadComments; len(comments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Doc:      true,
			Position: 0,
			List:     comments,
		})
		d.pendingHeadComments = nil
	}
	if len(lineComments) > 0 {
		ast.AddComment(n, &ast.CommentGroup{
			Line:     true,
			Position: linePos,
			List:     lineComments,
		})
	}
}

func (d *decoder) posErrorf(yn yast.Node, format string, args ...any) error {
	// TODO(mvdan): use columns as well; for now they are left out to avoid test churn
	return fmt.Errorf("%s:%d: "+format, append([]any{d.tokFile.Name(), goccyLine(yn)}, args...)...)
}

// goccyLine returns the 1-indexed line number of a goccy AST node.
func goccyLine(yn yast.Node) int {
	if tk := yn.GetToken(); tk != nil && tk.Position != nil {
		return tk.Position.Line
	}
	return 1
}

// nodeOffset returns the 0-indexed byte offset of a goccy AST node.
func (d *decoder) nodeOffset(yn yast.Node) int {
	return d.tokenOffset(yn.GetToken())
}

// tokenOffset returns the 0-indexed byte offset of a goccy token.
func (d *decoder) tokenOffset(tk *ytoken.Token) int {
	if tk == nil || tk.Position == nil {
		return 0
	}
	return d.lineColOffset(tk.Position.Line, tk.Position.Column)
}

// lineColOffset converts a 1-indexed line and rune-based column, as used
// by goccy positions, to a byte offset.
func (d *decoder) lineColOffset(line, col int) int {
	if line < 1 {
		return 0
	}
	if line > len(d.tokLines) {
		return len(d.src)
	}
	offset := d.tokLines[line-1]
	c := 1
	// Resume from the cached conversion when moving forward on the same line.
	if d.colCache.line == line && col >= d.colCache.column {
		offset, c = d.colCache.offset, d.colCache.column
	}
	for ; c < col && offset < len(d.src); c++ {
		_, size := utf8.DecodeRune(d.src[offset:])
		offset += size
	}
	d.colCache.line, d.colCache.column, d.colCache.offset = line, col, offset
	return offset
}

// offsetLine returns a 1-indexed line number for the given byte
// offset.
func (d *decoder) offsetLine(offset int) int {
	// Line n, counting from 1, starts at d.tokLines[n-1], so the
	// number of line starts at or before the offset is its line
	// number.
	line, _ := slices.BinarySearch(d.tokLines, offset+1)
	return line
}

// pos converts a byte offset to a cue/ast position.
// Note that this method uses and updates the last offset in lastOffset,
// so it should be called with increasing offsets.
func (d *decoder) pos(offset int) token.Pos {
	pos := d.tokFile.Pos(offset, token.NoRelPos)

	if d.forceInline {
		d.forceInline = false
		d.forceNewline = false
		pos = pos.WithRel(token.Blank)
	} else if d.forceNewline {
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
		// TODO(mvdan): when does this happen? Can we get rid of
		// those edge cases and this bit of logic?
		if offset < d.lastOffset {
			return token.NoPos
		}
	}
	d.lastOffset = offset
	return pos
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

// lineIsContent reports whether the 1-indexed line exists and holds
// content: it is neither blank nor a comment-only line.
func (d *decoder) lineIsContent(line int) bool {
	idx := line - 1
	return idx >= 0 && idx < len(d.tokLines)-1 && !d.isBlankLine(idx) && !d.isCommentLine(idx)
}

// scopeEndBefore computes the scope end before the YAML node starting at
// the given 1-indexed line, excluding the node's head comments and their
// surrounding blank lines from the scope. This ensures that comments
// belonging to the next sibling are not consumed by the current node's
// scope.
func (d *decoder) scopeEndBefore(keyLine int, hasHeadComments bool) int {
	end := d.tokLines[keyLine-1]
	if !hasHeadComments {
		return end
	}
	// Walk backwards from the line before the node, skipping blank lines
	// and then comment lines that belong to the node's head comments.
	lineIdx := keyLine - 2 // 0-indexed line just before the node
	for lineIdx >= 0 && d.isBlankLine(lineIdx) {
		lineIdx--
	}
	for lineIdx >= 0 && d.isCommentLine(lineIdx) {
		lineIdx--
	}
	return d.tokLines[lineIdx+1]
}

// entryRuns splits the comments attached to a mapping entry into those
// that trail the previous entry and those that head this entry. A run
// directly below the previous entry's content but separated from this
// entry's key by a blank line trails the previous entry.
func (d *decoder) entryRuns(cg *yast.CommentGroupNode, keyLine int) (prev, head []commentRun) {
	for _, run := range d.splitCommentRuns(cg) {
		if run.endLine < keyLine-1 && d.lineIsContent(run.startLine-1) {
			prev = append(prev, run)
		} else {
			head = append(head, run)
		}
	}
	return prev, head
}

// entryHasHeadComments reports whether a mapping entry has head comments
// of its own, for the purpose of computing the preceding scope end.
func (d *decoder) entryHasHeadComments(mv *yast.MappingValueNode) bool {
	_, head := d.entryRuns(mv.GetComment(), goccyLine(mv.Key))
	return len(head) > 0
}

// nodeHasHeadComments is like entryHasHeadComments for sequence
// elements, where head comments attach to the element's leading node.
func (d *decoder) nodeHasHeadComments(yn yast.Node) bool {
	switch n := yn.(type) {
	case *yast.AnchorNode:
		return d.nodeHasHeadComments(n.Value)
	case *yast.TagNode:
		return d.nodeHasHeadComments(n.Value)
	case *yast.MappingNode:
		if !n.IsFlowStyle && len(n.Values) > 0 {
			return d.entryHasHeadComments(n.Values[0])
		}
	case *yast.MappingValueNode:
		return d.entryHasHeadComments(n)
	case *yast.SequenceNode:
		if !n.IsFlowStyle && len(n.Values) > 0 {
			return d.nodeHasHeadComments(n.Values[0])
		}
	}
	cg := yn.GetComment()
	if cg == nil {
		return false
	}
	nodeLine := goccyLine(yn)
	for _, run := range d.splitCommentRuns(cg) {
		if run.startLine < nodeLine {
			return true
		}
	}
	return false
}

// adjustAnchoredStart moves the opening brace or bracket position of a
// block-style collection to just past its anchor, matching the greedy
// position choice made for anchorless block collections: past the
// anchor name, stop right after the first newline, or at the first
// non-whitespace, whichever is sooner.
func (d *decoder) adjustAnchoredStart(n *yast.AnchorNode, expr ast.Expr) {
	switch inner := n.Value.(type) {
	case *yast.MappingNode:
		if inner.IsFlowStyle {
			return
		}
	case *yast.MappingValueNode:
	case *yast.SequenceNode:
		if inner.IsFlowStyle {
			return
		}
	default:
		return
	}
	offset := d.nodeOffset(n) // at the '&'
	offset += 1 + len(n.Name.GetToken().Value)
	newlineSeen := false
loop:
	for ; offset < len(d.src); offset++ {
		if newlineSeen {
			break
		}
		switch d.src[offset] {
		case ' ', '\t':
		case '\n', '\r':
			newlineSeen = true
		default:
			break loop // stop at the first non-whitespace
		}
	}
	switch e := expr.(type) {
	case *ast.StructLit:
		e.Lbrace = d.tokFile.Pos(offset, token.Blank)
	case *ast.ListLit:
		e.Lbrack = d.tokFile.Pos(offset, token.Blank)
	}
}

// valueLine returns the 1-indexed line where a node's value content
// starts, looking through anchors and tags.
func valueLine(yn yast.Node) int {
	switch n := yn.(type) {
	case *yast.AnchorNode:
		return valueLine(n.Value)
	case *yast.TagNode:
		return valueLine(n.Value)
	}
	return goccyLine(yn)
}

func (d *decoder) mapping(values []*yast.MappingValueNode, flow bool, start, end *ytoken.Token) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd // save before insertMap modifies it
	var lbraceOffset int
	var nodeLine int
	switch {
	case flow && start != nil:
		lbraceOffset = d.tokenOffset(start)
		if start.Position != nil {
			nodeLine = start.Position.Line
		}
	case len(values) > 0:
		lbraceOffset = d.nodeOffset(values[0].Key)
		nodeLine = goccyLine(values[0].Key)
	}
	strct := &ast.StructLit{
		// Braces are a CUE concept with no YAML counterpart in
		// block-style mappings, so this position is our best guess.
		Lbrace: d.tokFile.Pos(lbraceOffset, token.Blank),
	}

	multiline := false
	if len(values) > 0 {
		multiline = nodeLine < valueLine(values[len(values)-1].Value)
	}

	// A single-line flow-style mapping `{a: 1, b: 2}` is inline: its
	// braces are Blank and its fields stay on one line. Force the
	// first field inline so neither a pending forceNewline (set by an
	// enclosing block sequence per element) nor the line-delta
	// heuristic (the map sits on a different source line than the
	// preceding sibling) leaks onto it and forces the map open; the
	// remaining fields are already on the same line. A multiline flow
	// mapping still breaks its fields: insertMap re-arms forceNewline
	// per field below.
	if flow && !multiline {
		d.forceInline = true
	}

	if err := d.insertMap(values, strct, multiline, false, parentScopeEnd, nil); err != nil {
		return nil, err
	}

	switch {
	case flow && end != nil:
		rbraceOffset := d.tokenOffset(end)
		// Update lastOffset past the '}' so that a parent flow
		// collection's positions stay monotonic.
		d.lastOffset = rbraceOffset + 1
		strct.Rbrace = d.tokFile.Pos(rbraceOffset, token.Blank)
	case len(values) > 0:
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
	default:
		strct.Rbrace = strct.Lbrace
	}
	return strct, nil
}

func (d *decoder) insertMap(values []*yast.MappingValueNode, m *ast.StructLit, multiline, mergeValues bool, parentScopeEnd int, ownKeys map[string]bool) error {
	// merging is true when these values are being merged in via a `<<`
	// merge key. A mapping's own keys take precedence over merged ones
	// regardless of the order in which the own keys and the merge key
	// appear, so track the own keys and drop any merged key that
	// collides with one.
	merging := mergeValues
	if !merging {
		ownKeys = make(map[string]bool)
	}
outer:
	for i, mv := range values {
		if multiline {
			d.forceNewline = true
		}
		keyLine := goccyLine(mv.Key)
		prevRuns, headRuns := d.entryRuns(mv.GetComment(), keyLine)
		for _, run := range prevRuns {
			if n := len(m.Elts); n > 0 {
				appendDocComments(m.Elts[n-1], run.comments)
			} else {
				d.addPendingRun(run)
			}
		}
		for _, run := range headRuns {
			d.addPendingRun(run)
		}

		if _, ok := mv.Key.(*yast.MergeKeyNode); ok {
			mergeValues = true
			// The anchor content named by `<<: *name` typically sits
			// earlier in the source, where pos() would refuse to move
			// back to and produce invalid positions. Reset the position
			// tracking around the expansion, like alias() does.
			savedLastOffset := d.lastOffset
			d.lastOffset = -1
			err := d.merge(mv.Value, m, multiline, ownKeys)
			d.lastOffset = savedLastOffset
			if err != nil {
				return err
			}
			continue
		}

		field := &ast.Field{}
		label, err := d.label(mv.Key)
		if err != nil {
			return err
		}
		key := labelStr(label)
		if merging && ownKeys[key] {
			// This key is defined directly by the mapping being merged
			// into, whose own value wins; drop the merged value.
			continue outer
		}
		if !merging {
			ownKeys[key] = true
		}
		// A comment on the key's own line, such as `a: # comment` with a
		// nested value below, becomes a line comment on the field.
		d.flushComments(field, d.nodeComments(mv.Key), 2)
		field.Label = label

		// Set the scope end for the value we're about to extract.
		if i+1 < len(values) {
			next := values[i+1]
			d.scopeEnd = d.scopeEndBefore(goccyLine(next.Key), d.entryHasHeadComments(next))
		} else {
			d.scopeEnd = parentScopeEnd
		}

		if mergeValues {
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

		if mv.FootComment != nil {
			if i+1 < len(values) {
				// More entries follow: foot comments become head comments for later.
				d.addPendingGroup(mv.FootComment)
			} else if comments := d.groupComments(mv.FootComment); len(comments) > 0 {
				// Last entry: foot comments go after the struct.
				ast.AddComment(m, &ast.CommentGroup{
					// After 100 tokens, so that the comment goes after the entire node.
					// TODO(mvdan): this is hacky, can the cue/ast API support trailing comments better?
					Position: 100,
					List:     comments,
				})
			}
		}

		m.Elts = append(m.Elts, field)
	}
	return nil
}

// appendDocComments appends comments to a node's doc comment group,
// creating one if needed.
func appendDocComments(n ast.Node, comments []*ast.Comment) {
	for _, cg := range ast.Comments(n) {
		if cg.Doc && cg.Position == 0 {
			cg.List = append(cg.List, comments...)
			return
		}
	}
	ast.AddComment(n, &ast.CommentGroup{Doc: true, Position: 0, List: comments})
}

func (d *decoder) merge(yn yast.Node, m *ast.StructLit, multiline bool, ownKeys map[string]bool) error {
	if anchor, ok := yn.(*yast.AnchorNode); ok {
		d.registerAnchor(anchor, anchor.Value)
		yn = anchor.Value
	}
	switch n := yn.(type) {
	case *yast.MappingNode:
		return d.insertMap(n.Values, m, multiline, true, d.scopeEnd, ownKeys)
	case *yast.MappingValueNode:
		return d.insertMap([]*yast.MappingValueNode{n}, m, multiline, true, d.scopeEnd, ownKeys)
	case *yast.AliasNode:
		// aliasTarget guards against a merge key referencing its own
		// mapping; mark the anchor as being expanded, like alias()
		// does for value aliases.
		target, err := d.aliasTarget(n)
		if err != nil {
			return err
		}
		name := n.Value.GetToken().Value
		if d.extractingAliases == nil {
			d.extractingAliases = make(map[string]bool)
		}
		d.extractingAliases[name] = true
		// Count the merged nodes towards the alias-expansion budget, as
		// alias() does, so that a merge key referencing an alias cannot
		// expand without bound: `<<: [*a, *a, ...]` nested many levels
		// deep would otherwise blow up exponentially.
		d.aliasDepth++
		err = d.merge(target, m, multiline, ownKeys)
		d.aliasDepth--
		delete(d.extractingAliases, name)
		return err
	case *yast.SequenceNode:
		// Step backwards as earlier nodes take precedence.
		for i := len(n.Values) - 1; i >= 0; i-- {
			// Each merged mapping's anchor content may sit anywhere in
			// the source, so reset the position tracking around each
			// element too, not just around the sequence as a whole;
			// otherwise an earlier element following a later one in the
			// source would produce invalid positions.
			savedLastOffset := d.lastOffset
			d.lastOffset = -1
			err := d.merge(n.Values[i], m, multiline, ownKeys)
			d.lastOffset = savedLastOffset
			if err != nil {
				return err
			}
		}
		return nil
	default:
		return d.posErrorf(yn, "map merge requires map or sequence of maps as the value")
	}
}

// aliasTarget resolves an alias node to the anchored YAML node,
// rejecting an anchor that is already being expanded, as its value
// would contain itself.
func (d *decoder) aliasTarget(yn *yast.AliasNode) (yast.Node, error) {
	name := yn.Value.GetToken().Value
	if d.extractingAliases[name] {
		// TODO this could actually be allowed in some circumstances.
		return nil, d.posErrorf(yn, "anchor %q value contains itself", name)
	}
	target, ok := d.anchors[name]
	if !ok {
		return nil, d.posErrorf(yn, "unknown anchor '%s' referenced", name)
	}
	return target, nil
}

func (d *decoder) label(yn yast.MapKeyNode) (ast.Label, error) {
	pos := d.pos(d.nodeOffset(yn))

	var value string
	numeric := false
	switch n := yn.(type) {
	case *yast.StringNode:
		value = n.Value
	case *yast.LiteralNode:
		value = n.Value.Value
	case *yast.NullNode:
		// With incoming YAML like `Null: 1`, the key scalar is normalized to "null".
		value = "null"
	case *yast.BoolNode:
		value = strconv.FormatBool(n.Value)
	case *yast.IntegerNode:
		value, numeric = yaml11OctalToCUE(n.GetToken().Value), true
	case *yast.FloatNode:
		value, numeric = n.GetToken().Value, true
	case *yast.InfinityNode:
		value, numeric = infString(n), true
	case *yast.NanNode:
		value = "NaN"
	case *yast.MappingKeyNode:
		// An explicit "? key" mapping key.
		return d.keyLabel(n.Value, pos)
	case *yast.AnchorNode:
		// An anchored key; the anchor names the key scalar.
		d.registerAnchor(n, n.Value)
		return d.keyLabel(n.Value, pos)
	case *yast.TagNode:
		return d.taggedLabel(n, pos)
	case *yast.AliasNode:
		target, err := d.aliasTarget(n)
		if err != nil {
			return nil, err
		}
		key, ok := target.(yast.MapKeyNode)
		if !ok {
			return nil, d.posErrorf(n, "invalid map key: %s", shortTag(target))
		}
		// Note that the label position stays at the alias reference site.
		labelValue, err := d.label(key)
		if err != nil {
			return nil, err
		}
		ast.SetPos(labelValue, pos)
		return labelValue, nil
	default:
		return nil, d.posErrorf(yn, "invalid map key: %s", shortTag(yn))
	}
	// Reject non-string keys that CUE would render as expressions
	// rather than literals, such as negative numbers. String keys
	// may of course begin with a dash.
	if numeric && strings.HasPrefix(value, "-") {
		return nil, d.posErrorf(yn, "invalid label %s", value)
	}

	label := ast.NewStringLabel(value)
	ast.SetPos(label, pos)
	return label, nil
}

// keyLabel converts the node wrapped by an explicit key or an anchor
// into a label, keeping the outer node's position.
func (d *decoder) keyLabel(inner yast.Node, pos token.Pos) (ast.Label, error) {
	key, ok := inner.(yast.MapKeyNode)
	if !ok {
		return nil, d.posErrorf(inner, "invalid map key: %s", shortTag(inner))
	}
	label, err := d.label(key)
	if err != nil {
		return nil, err
	}
	ast.SetPos(label, pos)
	return label, nil
}

// taggedLabel converts an explicitly tagged key such as `!!str 123: v`
// into a label, using the string form that the tag implies.
func (d *decoder) taggedLabel(n *yast.TagNode, pos token.Pos) (ast.Label, error) {
	expr, err := d.tagged(n)
	if err != nil {
		return nil, err
	}
	var value string
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			value = nodeStringValue(n.Value)
		} else {
			value = e.Value
		}
	case *ast.UnaryExpr:
		// A negative number; see the check in label.
		return nil, d.posErrorf(n, "invalid label %s", nodeStringValue(n.Value))
	default:
		return nil, d.posErrorf(n, "invalid map key: %s", shortTag(n.Value))
	}
	label := ast.NewStringLabel(value)
	ast.SetPos(label, pos)
	return label, nil
}

// shortTag returns a YAML short tag like "!!map" describing the node,
// for error messages.
func shortTag(yn yast.Node) string {
	switch yn.(type) {
	case *yast.MappingNode, *yast.MappingValueNode:
		return "!!map"
	case *yast.SequenceNode:
		return "!!seq"
	case *yast.AnchorNode, *yast.AliasNode, *yast.TagNode:
		return yn.Type().String()
	default:
		return "!!str"
	}
}

func (d *decoder) sequence(yn *yast.SequenceNode) (ast.Expr, error) {
	parentScopeEnd := d.scopeEnd // save before the loop modifies it
	flow := yn.IsFlowStyle
	var lbrackOffset int
	if flow && yn.Start != nil {
		lbrackOffset = d.tokenOffset(yn.Start)
	} else {
		lbrackOffset = d.nodeOffset(yn)
	}
	list := &ast.ListLit{
		// Like struct braces, brackets are a CUE concept with no YAML
		// counterpart in block-style sequences.
		Lbrack: d.tokFile.Pos(lbrackOffset, token.Blank),
	}

	// Unlike mappings which use d.label for keys, sequences extract
	// elements directly. Advance lastOffset so that element relative
	// positions are computed against the sequence node, not whatever
	// came before it.
	if lbrackOffset >= d.lastOffset {
		d.lastOffset = lbrackOffset
	}

	multiline := false
	if n := len(yn.Values); n > 0 {
		multiline = goccyLine(yn) < valueLine(yn.Values[n-1])
	}

	// If a list is empty, or ends with a struct, the closing `]` is on the same line.
	closeSameLine := true
	for i, c := range yn.Values {
		d.forceNewline = multiline
		// Comments between block sequence elements attach to the
		// sequence entry rather than to the element node.
		var entry, nextEntry *yast.SequenceEntryNode
		if i < len(yn.Entries) {
			entry = yn.Entries[i]
		}
		if i+1 < len(yn.Entries) {
			nextEntry = yn.Entries[i+1]
		}
		if entry != nil {
			d.addPendingGroup(entry.HeadComment)
		}
		// Set the scope end for the element we're about to extract.
		if i+1 < len(yn.Values) {
			next := yn.Values[i+1]
			hasHead := d.nodeHasHeadComments(next) ||
				(nextEntry != nil && nextEntry.HeadComment != nil)
			d.scopeEnd = d.scopeEndBefore(goccyLine(next), hasHead)
		} else {
			d.scopeEnd = parentScopeEnd
		}
		elem, err := d.extract(c)
		if err != nil {
			return nil, err
		}
		if entry != nil && entry.LineComment != nil {
			ast.AddComment(elem, &ast.CommentGroup{
				Line:     true,
				Position: 1,
				List:     d.groupComments(entry.LineComment),
			})
		}
		list.Elts = append(list.Elts, elem)
		// A list of structs begins with `[{`, so let it end with `}]`.
		_, closeSameLine = elem.(*ast.StructLit)
	}

	switch {
	case flow && yn.End != nil:
		rbrackOffset := d.tokenOffset(yn.End)
		// Update lastOffset past the ']' so that a parent flow
		// collection's positions stay monotonic.
		d.lastOffset = rbrackOffset + 1
		list.Rbrack = d.tokFile.Pos(rbrackOffset, token.Blank)
	case len(yn.Values) > 0:
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
	default:
		list.Rbrack = list.Lbrack
	}
	return list, nil
}

// quotedString returns a CUE string literal for the given decoded YAML
// string value.
func (d *decoder) quotedString(pos token.Pos, s string) ast.Expr {
	return &ast.BasicLit{
		ValuePos: pos,
		Kind:     token.STRING,
		Value:    literal.String.WithOptionalTabIndent(1).WithOptionalHashes().Quote(s),
	}
}

func (d *decoder) nullLit(pos token.Pos) ast.Expr {
	return &ast.BasicLit{
		ValuePos: pos.WithRel(token.Blank),
		Kind:     token.NULL,
		Value:    "null",
	}
}

func infString(n *yast.InfinityNode) string {
	if n.Value < 0 {
		return "-Inf"
	}
	return "+Inf"
}

// rxYamlInt matches the plain scalars this package resolves as
// integers: the YAML 1.2 core schema forms extended with the YAML 1.1
// forms CUE keeps supporting, namely leading-zero octals, binary
// integers, and a sign before any base. A decimal integer must not
// start with a zero digit, so that broken octals such as `0778`
// resolve as strings. Underscore separators are stripped before
// matching; see [numberKind].
var rxYamlInt = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[-+]?(0|[1-9][0-9]*|0b[01]+|0o?[0-7]+|0x[0-9a-fA-F]+)$`)
})

// rxYamlFloat matches the plain scalars this package resolves as
// floats: decimal digits with a dot, an exponent, or both. Requiring
// the dot or exponent keeps decimal integers, whatever their leading
// digit, out of the float form. Underscore separators are stripped
// before matching; see [numberKind].
var rxYamlFloat = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[-+]?((\.[0-9]+|[0-9]+\.[0-9]*)([eE][-+]?[0-9]+)?|[0-9]+[eE][-+]?[0-9]+)$`)
})

// specialFloats maps the plain scalar spellings of infinities and NaN
// to their decoded forms.
var specialFloats = map[string]string{
	".inf": "+Inf", ".Inf": "+Inf", ".INF": "+Inf",
	"+.inf": "+Inf", "+.Inf": "+Inf", "+.INF": "+Inf",
	"-.inf": "-Inf", "-.Inf": "-Inf", "-.INF": "-Inf",
	".nan": "NaN", ".NaN": "NaN", ".NAN": "NaN",
}

// numberKind reports whether this package resolves the plain scalar s
// as a number, returning token.INT, token.FLOAT, or token.ILLEGAL when
// s is not a number. The accepted forms are those of [rxYamlInt] and
// [rxYamlFloat], with no limit on the size of integers. Infinities and
// NaN are handled separately via [specialFloats]. Note that we cannot
// use CUE's own number syntax to decide, as it is a superset of
// YAML's: for example, `1Gi` is a valid CUE literal but a string in
// YAML.
func numberKind(s string) token.Token {
	if s == "" || s[0] == '_' {
		return token.ILLEGAL
	}
	// Strip underscores, which YAML 1.1 allows as separators within
	// numbers.
	plain := strings.ReplaceAll(s, "_", "")
	switch {
	case rxYamlInt().MatchString(plain):
		return token.INT
	case rxYamlFloat().MatchString(plain):
		return token.FLOAT
	}
	return token.ILLEGAL
}

// scalarString extracts a YAML string scalar. Note that goccy's lexer
// falls back to typing a plain (unquoted) scalar as a string whenever
// it does not resolve it as another scalar type such as an integer,
// float, boolean, or null. Plain scalars lexed as strings but that
// this package resolves as numbers, such as integers beyond 64 bits,
// infinities with unusual casing, or exponents without a decimal
// point, are decoded as numbers; see [numberKind].
func (d *decoder) scalarString(n *yast.StringNode) (ast.Expr, error) {
	pos := d.pos(d.nodeOffset(n))
	value := n.Value
	if n.GetToken().Type == ytoken.StringType { // a plain, unquoted scalar
		if num, ok := specialFloats[value]; ok {
			return d.makeNum(pos, num, token.FLOAT), nil
		}
		switch numberKind(value) {
		case token.INT:
			return d.intExpr(pos, value)
		case token.FLOAT:
			return d.floatExpr(pos, value, true)
		}
	}
	return d.quotedString(pos, value), nil
}

// yaml11OctalToCUE converts a YAML 1.1 octal literal like 0777 or
// -0777 to CUE form. Other values, such as floats with a leading zero
// digit like 01289.5, are returned unchanged.
func yaml11OctalToCUE(value string) string {
	sign, digits := "", value
	if len(digits) > 0 && (digits[0] == '+' || digits[0] == '-') {
		sign, digits = digits[:1], digits[1:]
	}
	rest, ok := strings.CutPrefix(digits, "0")
	if !ok || rest == "" {
		return value
	}
	for _, c := range rest {
		if (c < '0' || c > '7') && c != '_' {
			return value
		}
	}
	return sign + "0o" + rest
}

func (d *decoder) integer(n *yast.IntegerNode) (ast.Expr, error) {
	pos := d.pos(d.nodeOffset(n))
	expr, err := d.intExpr(pos, n.GetToken().Value)
	if err != nil {
		return nil, d.posErrorf(n, "%v", err)
	}
	return expr, nil
}

// intExpr converts a YAML integer literal to a CUE expression.
// If YAML accepted an invalid integer, we pass it along to ensure
// CUE will fail.
func (d *decoder) intExpr(pos token.Pos, value string) (ast.Expr, error) {
	value = yaml11OctalToCUE(value)
	var info literal.NumInfo
	// We make the assumption that any valid YAML integer literal will be a valid
	// CUE integer literal as well, with the only exception of octal numbers above.
	if err := literal.ParseNum(value, &info); err != nil {
		return nil, fmt.Errorf("cannot decode %q as %s: %v", value, "!!int", err)
	} else if !info.IsInt() {
		return nil, fmt.Errorf("cannot decode %q as %s: not a literal number", value, "!!int")
	}
	return d.makeNum(pos, value, token.INT), nil
}

func (d *decoder) float(n *yast.FloatNode) (ast.Expr, error) {
	pos := d.pos(d.nodeOffset(n))
	expr, err := d.floatExpr(pos, n.GetToken().Value, false)
	if err != nil {
		return nil, d.posErrorf(n, "%v", err)
	}
	return expr, nil
}

// floatExpr converts a YAML float literal to a CUE expression. When the
// literal has an explicit or implied !!float tag but looks like an
// integer, it is unified with "number" to record the fact that it was
// represented as a float. Don't unify with float, as `float & 123` is
// invalid, and there's no need to forbid representing the number as an
// integer either.
func (d *decoder) floatExpr(pos token.Pos, value string, explicitTag bool) (ast.Expr, error) {
	// Convert YAML 1.1 octal, like with integers.
	value = yaml11OctalToCUE(value)
	var info literal.NumInfo
	// We make the assumption that any valid YAML float literal will be a valid
	// CUE float literal as well, with the only exception of Inf/NaN and octal above.
	// Note that `!!float 123` is allowed.
	if err := literal.ParseNum(value, &info); err != nil {
		return nil, fmt.Errorf("cannot decode %q as %s: %v", value, "!!float", err)
	}
	if explicitTag && !strings.ContainsAny(value, ".eEiInN") {
		// TODO: number(v) when we have conversions
		return &ast.BinaryExpr{
			X:     &ast.Ident{NamePos: pos, Name: "number"},
			OpPos: pos.WithRel(token.Blank),
			Op:    token.AND,
			Y:     d.makeNum(pos.WithRel(token.Blank), value, token.INT),
		}, nil
	}
	return d.makeNum(pos, value, token.FLOAT), nil
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

// resolveTag expands a tag through any %TAG directive handles, and
// shortens tags in the default yaml.org namespace to their !!name form.
func (d *decoder) resolveTag(tag string) string {
	return resolveTagWith(d.tagHandles, tag)
}

// resolveTagWith is resolveTag over the given handle map.
func resolveTagWith(handles map[string]string, tag string) string {
	if len(tag) < 2 || tag[0] != '!' || strings.HasPrefix(tag, "!<") {
		// Not a tag shorthand; verbatim "!<...>" tags in particular do
		// not go through handle resolution.
		return tag
	}
	var handle, suffix string
	if strings.HasPrefix(tag, "!!") {
		handle, suffix = "!!", tag[2:]
	} else if i := strings.Index(tag[1:], "!"); i >= 0 {
		handle, suffix = tag[:i+2], tag[i+2:]
	} else {
		// The primary handle, as in "!str" with a `%TAG !` directive.
		handle, suffix = "!", tag[1:]
	}
	prefix, ok := handles[handle]
	if !ok {
		return tag
	}
	long := prefix + suffix
	if s, ok := strings.CutPrefix(long, "tag:yaml.org,2002:"); ok {
		return "!!" + s
	}
	return long
}

// tagged extracts a YAML node with an explicit tag.
func (d *decoder) tagged(n *yast.TagNode) (ast.Expr, error) {
	tag := d.resolveTag(n.Start.Value)
	inner := n.Value

	switch v := inner.(type) {
	case *yast.AnchorNode:
		// A tagged anchor, such as `!!str &x hi`; the anchor names the
		// tagged content.
		d.registerAnchor(v, n)
		inner = v.Value
	case *yast.AliasNode:
		return nil, d.posErrorf(n, "cannot unmarshal tag %q on alias node", tag)
	}

	switch tag {
	case "!":
		// A non-specific tag; the value is treated as a plain scalar
		// would be, except that untagged scalars stay strings.
		return d.extract(inner)

	case "!!str":
		pos := d.pos(d.nodeOffset(inner))
		return d.quotedString(pos, nodeStringValue(inner)), nil

	case "!!timestamp":
		pos := d.pos(d.nodeOffset(inner))
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.String.Quote(nodeStringValue(inner)),
		}, nil

	case "!!binary":
		pos := d.pos(d.nodeOffset(inner))
		// Note that base64 decoding skips newlines, which occur in
		// !!binary block scalars.
		data, err := base64.StdEncoding.DecodeString(nodeStringValue(inner))
		if err != nil {
			return nil, d.posErrorf(n, "!!binary value contains invalid base64 data")
		}
		return &ast.BasicLit{
			ValuePos: pos,
			Kind:     token.STRING,
			Value:    literal.Bytes.Quote(string(data)),
		}, nil

	case "!!bool":
		pos := d.pos(d.nodeOffset(inner))
		t := false
		switch nodeStringValue(inner) {
		case "true", "True", "TRUE":
			t = true
		}
		lit := ast.NewBool(t)
		lit.ValuePos = pos
		return lit, nil

	case "!!int":
		pos := d.pos(d.nodeOffset(inner))
		expr, err := d.intExpr(pos, nodeStringValue(inner))
		if err != nil {
			return nil, d.posErrorf(n, "%v", err)
		}
		return expr, nil

	case "!!float":
		switch inner.(type) {
		case *yast.InfinityNode, *yast.NanNode:
			// The tag is redundant; decode like the plain scalars.
			return d.extract(inner)
		}
		pos := d.pos(d.nodeOffset(inner))
		expr, err := d.floatExpr(pos, nodeStringValue(inner), true)
		if err != nil {
			return nil, d.posErrorf(n, "%v", err)
		}
		return expr, nil

	case "!!null":
		pos := d.pos(d.nodeOffset(inner))
		return d.nullLit(pos), nil

	case "!!seq", "!!map":
		return d.extract(inner)

	default:
		switch inner.(type) {
		case *yast.MappingNode, *yast.MappingValueNode, *yast.SequenceNode:
			// Tags on collections carry no meaning for CUE.
			return d.extract(inner)
		}
		return nil, d.posErrorf(n, "cannot unmarshal tag %q", tag)
	}
}

// nodeStringValue returns the string contents of a scalar node.
func nodeStringValue(yn yast.Node) string {
	switch n := yn.(type) {
	case *yast.StringNode:
		return n.Value
	case *yast.LiteralNode:
		return n.Value.Value
	case *yast.NullNode:
		if n.GetToken().Type == ytoken.ImplicitNullType {
			return ""
		}
		return n.GetToken().Value
	default:
		if tk := yn.GetToken(); tk != nil {
			return tk.Value
		}
		return ""
	}
}

// maxAliasNodes bounds the nodes produced by alias expansion. Each alias
// reference expands into a fresh subtree, so wide fan-out can produce a
// tree far larger than the input. Realistic documents use little or no
// aliasing, so this limit is generous.
const maxAliasNodes = 50_000

func (d *decoder) alias(yn *yast.AliasNode) (ast.Expr, error) {
	target, err := d.aliasTarget(yn)
	if err != nil {
		return nil, err
	}
	name := yn.Value.GetToken().Value
	if d.extractingAliases == nil {
		d.extractingAliases = make(map[string]bool)
	}
	d.extractingAliases[name] = true

	// Save and reset decoder state so the alias extraction doesn't
	// interfere with the outer position tracking. The aliased node
	// may be earlier in the source (the common case: define then use),
	// which would leave lastOffset pointing past the aliased content.
	savedLastOffset := d.lastOffset
	savedForceNewline := d.forceNewline
	savedScopeEnd := d.scopeEnd
	d.lastOffset = -1 // no position yet, so pos() produces valid positions for the alias's children
	d.forceNewline = false

	d.aliasDepth++
	node, err := d.extract(target)
	d.aliasDepth--

	d.lastOffset = savedLastOffset
	d.forceNewline = savedForceNewline
	d.scopeEnd = savedScopeEnd
	delete(d.extractingAliases, name)
	if err != nil {
		return nil, err
	}

	// For container types, override brace/bracket positions to reflect
	// the alias reference site (*name), not the anchor definition site.
	// The alias is where this value logically appears in the document.
	aliasStart := d.nodeOffset(yn)
	aliasEnd := aliasStart + len(name) // *<name>: 1 + len(name) - 1
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
