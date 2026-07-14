// Copyright 2020 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yaml

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	gyaml "github.com/goccy/go-yaml"
	gast "github.com/goccy/go-yaml/ast"
	glexer "github.com/goccy/go-yaml/lexer"
	gprinter "github.com/goccy/go-yaml/printer"
	gtoken "github.com/goccy/go-yaml/token"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
)

// The encoder is built on goccy/go-yaml's Encoder: the CUE syntax tree
// is converted to a Go value tree (yaml.MapSlice for mappings, []any
// for sequences, and strings or pre-rendered scalars for values) which
// goccy's EncodeToNode turns into a YAML syntax tree. Aspects that the
// value tree cannot express, recorded in a parallel tree of encInfo
// nodes, are then applied to the YAML syntax tree before printing:
// forced double quoting of keys, flow styles, tags, and comments.

// Encode converts a CUE AST to YAML.
//
// The given file must only contain values that can be directly supported by
// YAML:
//
//	Type          Restrictions
//	BasicLit
//	File          no imports, aliases, or definitions
//	StructLit     no embeddings, aliases, or definitions
//	List
//	Field         must be regular; label must be a BasicLit or Ident
//	CommentGroup
//
// TODO: support anchors through Ident.
func Encode(n ast.Node, opts ...EncodeOption) (b []byte, err error) {
	cfg := encodeConfig{indentSequence: true}
	for _, o := range opts {
		o(&cfg)
	}

	v, info, err := encode(n)
	if err != nil {
		return nil, err
	}
	enc := gyaml.NewEncoder(nil,
		// Use idiomatic indentation.
		gyaml.Indent(2),
		gyaml.IndentSequence(cfg.indentSequence),
		// Prefer single quotes where goccy quotes on its own accord;
		// strings quoted deliberately by this package are pre-rendered
		// with double quotes.
		gyaml.UseSingleQuote(true),
		gyaml.UseLiteralStyleIfMultiline(true))
	node, err := enc.EncodeToNode(v)
	if err != nil {
		return nil, err
	}
	if seq, ok := node.(*gast.SequenceNode); ok && cfg.indentSequence {
		// A document-level sequence has no mapping key to be indented
		// relative to.
		seq.AddColumn(-2)
	}
	node = applyInfo(node, info)
	if len(info.line) > 0 {
		// A line comment on the root value itself, such as an embedded
		// scalar document.
		node.SetComment(commentGroup(info.line, false))
	}
	var p gprinter.Printer
	out := p.PrintNode(node)

	// Comments on an embedded root value have no place in the YAML
	// syntax tree; render them around the document instead.
	rootHead := append(info.rootHead, info.head...)
	rootFoot := append(info.rootFoot, info.foot...)
	if len(rootHead) > 0 || len(rootFoot) > 0 {
		var sb strings.Builder
		writeCommentLines(&sb, rootHead)
		if info.rootHeadGap {
			sb.WriteString("\n")
		}
		sb.Write(out)
		if len(rootFoot) > 0 {
			if !strings.HasSuffix(sb.String(), "\n") {
				sb.WriteString("\n")
			}
			writeCommentLines(&sb, rootFoot)
		}
		out = []byte(sb.String())
	}
	return out, nil
}

// EncodeOption configures the behavior of [Encode].
type EncodeOption func(*encodeConfig)

type encodeConfig struct {
	indentSequence bool
}

// IndentSequences controls whether sequence (list) elements are indented
// relative to their enclosing mapping key. It defaults to true.
func IndentSequences(indent bool) EncodeOption {
	return func(c *encodeConfig) { c.indentSequence = indent }
}

// rawScalar is a pre-rendered YAML scalar, emitted verbatim through
// goccy's BytesMarshaler mechanism: the bytes are parsed as YAML and
// grafted into the output tree. Used for numbers, quoted strings, and
// tagged scalars such as !!binary.
type rawScalar string

// MarshalYAML implements [gyaml.BytesMarshaler].
func (r rawScalar) MarshalYAML() ([]byte, error) { return []byte(r), nil }

// commentLine is one comment line, without the leading "#".
type commentLine struct {
	text string
	gap  bool // render a blank line above this comment line
}

// encInfo carries the aspects of a node that the value tree passed to
// goccy's encoder cannot express. It mirrors the structure of the value
// tree: entries correspond to mapping entries or sequence elements.
type encInfo struct {
	// quoteKey forces double quotes on this mapping entry's key,
	// overriding any quoting style chosen by goccy. keyName holds the
	// raw key name.
	quoteKey bool
	keyName  string
	// flow renders a mapping or sequence in flow style.
	flow bool
	// tag wraps the value in a YAML tag such as !!binary or a custom
	// tag from an @yaml attribute.
	tag string
	// head, line and foot are the comments surrounding this entry.
	head      []commentLine
	line      []commentLine
	foot      []commentLine
	headGap   bool // blank line between the head comments and the entry
	footBlank bool // blank line between the entry and its foot comment

	// rootHead and rootFoot hold comments on an embedded root value,
	// rendered around the printed document. rootHeadGap adds a blank
	// line between the head comments and the value.
	rootHead    []commentLine
	rootFoot    []commentLine
	rootHeadGap bool

	entries []*encInfo
}

func (i *encInfo) entry(n int) *encInfo {
	if i == nil || n >= len(i.entries) {
		return nil
	}
	return i.entries[n]
}

// applyInfo applies the recorded fixups to the YAML syntax tree
// produced by goccy's encoder, returning the node (wrapped in a tag
// node when a tag applies).
func applyInfo(node gast.Node, info *encInfo) gast.Node {
	if info == nil {
		return node
	}
	switch n := node.(type) {
	case *gast.MappingNode:
		if info.flow {
			n.IsFlowStyle = true
		}
		for i, mv := range n.Values {
			e := info.entry(i)
			if e == nil {
				continue
			}
			if e.quoteKey {
				if sn, ok := mv.Key.(*gast.StringNode); ok {
					q := strconv.Quote(e.keyName)
					sn.Token.Value = q
					sn.Value = q
				}
			}
			if len(e.head) > 0 {
				mv.SetComment(commentGroup(e.head, false))
				if e.headGap {
					// Render a blank line between the head comments and
					// the entry through a synthetic line break on the key.
					if tk := mv.Key.GetToken(); tk != nil && tk.Position != nil {
						tk.Position.Line = 3
						tk.Prev = &gtoken.Token{Origin: "x", Position: &gtoken.Position{Line: 1}}
					}
				}
			}
			if len(e.foot) > 0 {
				mv.FootComment = commentGroup(e.foot, e.footBlank)
			}
			mv.Value = applyInfo(mv.Value, e)
			if len(e.line) > 0 {
				cg := commentGroup(e.line, false)
				// A line comment on a block collection goes after the
				// key, as the value starts on the next line.
				blockCollection := false
				switch v := mv.Value.(type) {
				case *gast.MappingNode:
					blockCollection = !v.IsFlowStyle && len(v.Values) > 0
				case *gast.SequenceNode:
					blockCollection = !v.IsFlowStyle && len(v.Values) > 0
				}
				if blockCollection {
					mv.Key.SetComment(cg)
				} else {
					mv.Value.SetComment(cg)
				}
			}
		}
	case *gast.SequenceNode:
		if info.flow {
			n.IsFlowStyle = true
		}
		for i := range n.Values {
			e := info.entry(i)
			if e == nil {
				continue
			}
			if len(e.head) > 0 {
				if len(n.ValueHeadComments) == 0 {
					n.ValueHeadComments = make([]*gast.CommentGroupNode, len(n.Values))
				}
				n.ValueHeadComments[i] = commentGroup(e.head, false)
			}
			n.Values[i] = applyInfo(n.Values[i], e)
			if len(e.line) > 0 {
				n.Values[i].SetComment(commentGroup(e.line, false))
			}
		}
	}
	if info.tag != "" {
		tag := escapeTag(info.tag)
		// Copy the wrapped value's position so that rendering the tag
		// node knows its column.
		var pos *gtoken.Position
		if tk := node.GetToken(); tk != nil && tk.Position != nil {
			p := *tk.Position
			pos = &p
		}
		tn := gast.Tag(gtoken.New(tag, tag, pos))
		tn.Value = node
		return tn
	}
	return node
}

// commentGroup builds a goccy comment group from comment lines.
// Blank separator lines and a leading blank line are expressed through
// synthetic token positions: goccy renders a blank line above a comment
// token when its position is more than one line past its predecessor.
func commentGroup(lines []commentLine, leadingBlank bool) *gast.CommentGroupNode {
	var toks []*gtoken.Token
	line := 2
	if leadingBlank {
		line++
	}
	prev := &gtoken.Token{Origin: "x", Position: &gtoken.Position{Line: 1}}
	for _, l := range lines {
		if l.gap {
			line++
		}
		tk := gtoken.New(l.text, l.text, &gtoken.Position{Line: line})
		tk.Prev = prev
		prev = tk
		line++
		toks = append(toks, tk)
	}
	return gast.CommentGroup(toks)
}

func writeCommentLines(sb *strings.Builder, lines []commentLine) {
	for _, l := range lines {
		if l.gap {
			sb.WriteString("\n")
		}
		sb.WriteString("#")
		sb.WriteString(l.text)
		sb.WriteString("\n")
	}
}

// escapeTag percent-escapes the characters of a YAML tag that are not
// valid tag characters, such as "<" and ">" in verbatim tags.
func escapeTag(tag string) string {
	var sb strings.Builder
	for i := 0; i < len(tag); i++ {
		c := tag[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			sb.WriteByte(c)
		case strings.IndexByte("!;/?:@&=+$,_.~*'()-", c) >= 0:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

func encode(n ast.Node) (v any, info *encInfo, err error) {
	switch x := n.(type) {
	case *ast.BasicLit:
		v, err = encodeScalar(x)

	case *ast.ListLit:
		v, info, err = encodeExprs(x.Elts)
		line := x.Lbrack.Line()
		if err == nil && line > 0 && line == x.Rbrack.Line() {
			info.flow = true
		}

	case *ast.StructLit:
		v, info, err = encodeDecls(x.Elts)
		line := x.Lbrace.Line()
		if err == nil && line > 0 && line == x.Rbrace.Line() {
			info.flow = true
		}

	case *ast.File:
		v, info, err = encodeDecls(x.Decls)

	case *ast.UnaryExpr:
		b, ok := x.X.(*ast.BasicLit)
		if ok && x.Op == token.SUB && (b.Kind == token.INT || b.Kind == token.FLOAT) {
			var s string
			s, err = yamlNumber(b)
			if err != nil {
				return nil, nil, err
			}
			if !strings.HasPrefix(s, "-") {
				v = rawScalar("-" + s)
				break
			}
		}
		return nil, nil, errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", astinternal.DebugStr(x), x)
	default:
		return nil, nil, errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", astinternal.DebugStr(x), x)
	}
	if err != nil {
		return nil, nil, err
	}
	if info == nil {
		info = &encInfo{}
	}
	addDocs(n, info)
	// Head comments on a collection render above its first entry, and
	// foot comments after its last entry.
	if len(info.entries) > 0 {
		if len(info.head) > 0 {
			first := info.entries[0]
			if len(first.head) > 0 {
				first.head[0].gap = true
			}
			first.head = append(info.head, first.head...)
			info.head = nil
		}
		if len(info.foot) > 0 {
			last := info.entries[len(info.entries)-1]
			last.foot = append(last.foot, info.foot...)
			last.footBlank = last.footBlank || info.footBlank
			info.foot, info.footBlank = nil, false
		}
	}
	return v, info, nil
}

func encodeScalar(b *ast.BasicLit) (any, error) {
	switch b.Kind {
	case token.INT, token.FLOAT:
		s, err := yamlNumber(b)
		if err != nil {
			return nil, err
		}
		return rawScalar(s), nil

	case token.TRUE, token.FALSE, token.NULL:
		return rawScalar(b.Value), nil

	case token.STRING:
		info, nStart, _, err := literal.ParseQuotes(b.Value, b.Value)
		if err != nil {
			return nil, err
		}
		str, err := info.Unquote(b.Value[nStart:])
		if err != nil {
			panic(fmt.Sprintf("invalid string: %v", err))
		}
		switch {
		case !info.IsDouble():
			return rawScalar("!!binary " + base64.StdEncoding.EncodeToString([]byte(str))), nil

		case str == "":
			return rawScalar(`""`), nil

		case strings.Contains(str, "\n"):
			if info.IsMulti() {
				// Preserve multi-line format: goccy renders plain strings
				// containing newlines in literal style.
				return str, nil
			}
			return rawScalar(strconv.Quote(str)), nil

		case shouldQuote(str):
			return rawScalar(strconv.Quote(str)), nil

		default:
			return str, nil
		}

	default:
		return nil, errors.Newf(b.Pos(), "unknown literal type %v", b.Kind)
	}
}

// yamlNumber returns the YAML form of a CUE number literal: the literal
// itself when YAML would parse it back as a number, or its normalized
// form otherwise (for example, 1K becomes 1000).
func yamlNumber(b *ast.BasicLit) (string, error) {
	s := b.Value
	if yamlIsNumber(s) {
		return s, nil
	}
	var ni literal.NumInfo
	if err := literal.ParseNum(s, &ni); err != nil {
		return "", errors.Newf(b.Pos(), "invalid number literal %q: %v", s, err)
	}
	return ni.String(), nil
}

// yamlIsNumber reports whether YAML parses s as a single number scalar.
func yamlIsNumber(s string) bool {
	toks := glexer.Tokenize(s)
	if len(toks) != 1 {
		return false
	}
	switch toks[0].Type {
	case gtoken.IntegerType, gtoken.BinaryIntegerType, gtoken.OctetIntegerType,
		gtoken.HexIntegerType, gtoken.FloatType:
		return true
	}
	return false
}

// shouldQuote indicates that a string must be double quoted to remain a
// string: it may be a YAML 1.1 legacy value, or a scalar that YAML
// decodes as another type such as a number or an infinity.
func shouldQuote(str string) bool {
	return str == "" || legacyStrings[str] || useQuote().MatchString(str) || decodesAsNonString(str)
}

// decodesAsNonString reports whether a plain (unquoted) scalar with the
// given content would be decoded by this package as a non-string value.
func decodesAsNonString(s string) bool {
	toks := glexer.Tokenize(s)
	if len(toks) != 1 {
		return false
	}
	switch toks[0].Type {
	case gtoken.IntegerType, gtoken.BinaryIntegerType, gtoken.OctetIntegerType,
		gtoken.HexIntegerType, gtoken.FloatType, gtoken.BoolType,
		gtoken.NullType, gtoken.ImplicitNullType, gtoken.InfinityType,
		gtoken.NanType, gtoken.MergeKeyType:
		return true
	case gtoken.StringType:
		if toks[0].Value != s {
			// The lexer transformed the value, e.g. folding newlines.
			return false
		}
		// Mirror the decoder's handling of plain string scalars, which
		// recognizes some more numbers. See [decoder.scalarString].
		switch s {
		case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF",
			"-.inf", "-.Inf", "-.INF", ".nan", ".NaN", ".NAN":
			return true
		}
		var info literal.NumInfo
		return literal.ParseNum(s, &info) == nil
	default:
		return false
	}
}

// This regular expression conservatively matches any date, time string,
// or base60 float.
var useQuote = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[\-+0-9:\. \t]+([-:]|[tT])[\-+0-9:\. \t]+[zZ]?$|^0x[a-fA-F0-9]+$`)
})

// legacyStrings contains a map of fixed strings with special meaning for any
// type in the YAML Tag registry (https://yaml.org/type/index.html) as used
// in YAML 1.1.
//
// These strings are always quoted upon export to allow for backward
// compatibility with YAML 1.1 parsers.
var legacyStrings = map[string]bool{
	"y":     true,
	"Y":     true,
	"yes":   true,
	"Yes":   true,
	"YES":   true,
	"n":     true,
	"N":     true,
	"t":     true,
	"T":     true,
	"f":     true,
	"F":     true,
	"no":    true,
	"No":    true,
	"NO":    true,
	"true":  true,
	"True":  true,
	"TRUE":  true,
	"false": true,
	"False": true,
	"FALSE": true,
	"on":    true,
	"On":    true,
	"ON":    true,
	"off":   true,
	"Off":   true,
	"OFF":   true,

	// Non-standard.
	".Nan": true,
}

func encodeExprs(exprs []ast.Expr) ([]any, *encInfo, error) {
	info := &encInfo{}
	vs := make([]any, 0, len(exprs))
	for _, elem := range exprs {
		e, elemInfo, err := encode(elem)
		if err != nil {
			return nil, nil, err
		}
		vs = append(vs, e)
		info.entries = append(info.entries, elemInfo)
	}
	return vs, info, nil
}

// extractYAMLTag looks for @yaml(,tag="...") attribute and returns the tag value.
// Returns an empty string if no @yaml attribute or no tag argument is found.
// Returns an error if the attribute is malformed.
func extractYAMLTag(attrs []*ast.Attribute) (string, error) {
	for _, attr := range attrs {
		if attr.Name() != "yaml" {
			continue
		}
		parsed := internal.ParseAttr(attr)
		if parsed.Err != nil {
			return "", parsed.Err
		}
		if val, found, err := parsed.Lookup(1, "tag"); err != nil {
			return "", err
		} else if found {
			return val, nil
		}
	}
	return "", nil
}

// encodeDecls converts a sequence of declarations to a value. If it encounters
// an embedded value, it will return this expression. This is more relaxed for
// structs than is currently allowed for CUE, but the expectation is that this
// will be allowed at some point. The input would still be illegal CUE.
func encodeDecls(decls []ast.Decl) (any, *encInfo, error) {
	info := &encInfo{}
	var m gyaml.MapSlice

	// docForNext collects the comment groups between fields, which
	// become head comments of the next field.
	var docForNext []commentLine
	hasEmbed := false
	var embedValue any
	var embedInfo *encInfo
	for _, d := range decls {
		switch x := d.(type) {
		default:
			return nil, nil, errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", astinternal.DebugStr(x), x)

		case *ast.Package:
			if len(m) > 0 {
				return nil, nil, errors.Newf(x.Pos(), "invalid package clause")
			}
			continue

		case *ast.CommentGroup:
			docForNext = appendCommentGroup(docForNext, x)
			continue

		case *ast.Attribute:
			continue

		case *ast.Field:
			if !internal.IsRegularField(x) {
				return nil, nil, errors.Newf(x.TokenPos, "yaml: definition or hidden fields not allowed")
			}
			if x.Constraint != token.ILLEGAL {
				return nil, nil, errors.Newf(x.TokenPos, "yaml: optional fields not allowed")
			}
			if hasEmbed {
				return nil, nil, errors.Newf(x.TokenPos, "yaml: embedding mixed with fields")
			}
			name, _, err := ast.LabelName(x.Label)
			if err != nil {
				return nil, nil, errors.Newf(x.Label.Pos(), "yaml: only literal labels allowed")
			}

			value, valueInfo, err := encode(x.Value)
			if err != nil {
				return nil, nil, err
			}

			if shouldQuote(name) {
				valueInfo.quoteKey = true
				valueInfo.keyName = name
			}

			yamlTag, err := extractYAMLTag(x.Attrs)
			if err != nil {
				return nil, nil, err
			}
			valueInfo.tag = yamlTag

			addDocs(x.Label, valueInfo)
			addDocs(x, valueInfo)
			if len(docForNext) > 0 {
				if len(valueInfo.head) > 0 {
					valueInfo.head[0].gap = true
				} else {
					// Standalone comment groups are separated from the
					// field below them by a blank line.
					valueInfo.headGap = true
				}
				valueInfo.head = append(docForNext, valueInfo.head...)
				docForNext = nil
			}

			m = append(m, gyaml.MapItem{Key: name, Value: value})
			info.entries = append(info.entries, valueInfo)

		case *ast.EmbedDecl:
			if hasEmbed {
				return nil, nil, errors.Newf(x.Pos(), "yaml: multiple embedded values")
			}
			hasEmbed = true
			e, eInfo, err := encode(x.Expr)
			if err != nil {
				return nil, nil, err
			}
			addDocs(x, eInfo)
			if len(docForNext) > 0 {
				// Standalone comment groups are separated from the value.
				eInfo.rootHeadGap = true
			}
			eInfo.head = append(docForNext, eInfo.head...)
			docForNext = nil
			embedValue, embedInfo = e, eInfo
		}
	}

	if hasEmbed {
		// Comments around an embedded root value cannot be attached to
		// the YAML syntax tree; keep them for rendering around the
		// printed document instead.
		embedInfo.rootHead = embedInfo.head
		embedInfo.rootHeadGap = len(docForNext) > 0 || embedInfo.rootHeadGap
		embedInfo.rootFoot = append(embedInfo.foot, docForNext...)
		embedInfo.head, embedInfo.foot = nil, nil
		return embedValue, embedInfo, nil
	}

	if len(docForNext) > 0 && len(info.entries) > 0 {
		// Trailing comments after the last field.
		last := info.entries[len(info.entries)-1]
		last.foot = append(last.foot, docForNext...)
		last.footBlank = true
	}

	// Fold each entry's foot comments into the next entry's head
	// comments, so that a blank line separates them: this matches how
	// the yaml.v3 based encoder used to lay out foot comments.
	for i := 0; i+1 < len(info.entries); i++ {
		e := info.entries[i]
		if len(e.foot) == 0 {
			continue
		}
		next := info.entries[i+1]
		foot := e.foot
		if e.footBlank {
			foot[0].gap = true
		}
		if len(next.head) > 0 {
			next.head[0].gap = true
		}
		next.head = append(foot, next.head...)
		e.foot, e.footBlank = nil, false
	}

	if m == nil {
		m = gyaml.MapSlice{}
	}
	return m, info, nil
}

// addDocs records a CUE node's comments: head (doc) comments, line
// comments, and foot comments.
func addDocs(n ast.Node, info *encInfo) {
	for _, c := range ast.Comments(n) {
		switch {
		case c.Line:
			info.line = commentLines(nil, c)

		case c.Position > 0:
			if len(info.foot) == 0 && c.Pos().RelPos() == token.NewSection {
				info.footBlank = true
			}
			info.foot = commentLines(info.foot, c)

		default:
			if len(info.head) > 0 || !c.Doc {
				// A non-doc comment is separated from the value below it.
				info.rootHeadGap = true
			}
			info.head = commentLines(info.head, c)
		}
	}
}

// appendCommentGroup appends a CUE comment group to a list of comment
// lines, separating consecutive groups by a blank line.
func appendCommentGroup(lines []commentLine, c *ast.CommentGroup) []commentLine {
	return commentLines(lines, c)
}

// commentLines converts a CUE comment group to YAML comment lines,
// appending to lines with a blank separator when it is non-empty.
func commentLines(lines []commentLine, c *ast.CommentGroup) []commentLine {
	s := strings.TrimSuffix(c.Text(), "\n") // always trims
	for i, l := range strings.Split(s, "\n") {
		cl := commentLine{}
		if l != "" {
			cl.text = " " + l
		}
		if i == 0 && len(lines) > 0 {
			cl.gap = true
		}
		lines = append(lines, cl)
	}
	return lines
}
