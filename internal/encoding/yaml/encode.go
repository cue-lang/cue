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
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

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
	if info.literal != "" {
		// A literal block as the document root indents its content
		// relative to column one.
		node = literalBlock(node, info.literal, 2)
	}
	if len(info.line) > 0 {
		// A line comment on the root value itself, such as an embedded
		// scalar document.
		node.SetComment(commentGroup(info.line, false))
	}
	var p gprinter.Printer
	out := p.PrintNode(node)
	// goccy pads the blank lines within literal block scalars to the
	// block's indentation. Render them as truly blank lines instead,
	// like the yaml.v3 based encoder did. Lines of only spaces cannot
	// occur otherwise: blockLiteralSafe only admits block content whose
	// lines never end in a space, and every other scalar and comment
	// renders on a single line.
	out = stripBlankLinePadding(out)

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

// stripBlankLinePadding turns lines consisting solely of spaces into
// truly empty lines; see the callers for why this is safe.
func stripBlankLinePadding(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	for i, line := range lines {
		if len(line) > 0 && len(bytes.TrimLeft(line, " ")) == 0 {
			lines[i] = nil
		}
	}
	return bytes.Join(lines, []byte("\n"))
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

// literalString is a single-line string to be rendered as a literal
// block scalar. encode turns it into a plain string in the value tree
// and records the request in [encInfo.literal]; see there for why.
type literalString string

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
	// literal renders this entry's value, a single-line string, as a
	// literal block scalar, preserving the block form of a multi-line
	// CUE string literal whose content is a single line. goccy renders
	// multi-line strings in literal style of its own accord, but has no
	// way to express a literal block with single-line content, so the
	// node is swapped for an explicit block in applyInfo, where the
	// final layout is known. literal holds the raw string value.
	literal string
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
			if e.literal != "" && !n.IsFlowStyle {
				if tk := mv.Key.GetToken(); tk != nil && tk.Position != nil {
					// The block content sits one level deeper than the key.
					mv.Value = literalBlock(mv.Value, e.literal, tk.Position.Column+1)
				}
			}
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
			if e.literal != "" && !n.IsFlowStyle {
				if st := n.Start; st != nil && st.Position != nil {
					// Printing a block sequence re-indents an element's
					// block content to sit two columns past the sequence
					// start; the origin must match for that to round-trip.
					n.Values[i] = literalBlock(n.Values[i], e.literal, st.Position.Column+1)
				}
			}
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

// literalBlock replaces a plain string node with an explicit literal
// block scalar node holding the given single-line value, indented by
// the given number of spaces. Nodes of any other kind, such as inside
// flow collections, are returned unchanged.
func literalBlock(node gast.Node, value string, indent int) gast.Node {
	sn, ok := node.(*gast.StringNode)
	if !ok {
		return node
	}
	pos := func() *gtoken.Position {
		if sn.Token != nil && sn.Token.Position != nil {
			p := *sn.Token.Position
			return &p
		}
		return nil
	}
	lit := gast.Literal(gtoken.New("|-", "|-", pos()))
	// The literal node renders as its start token ("|-") followed by
	// the value token's origin, which must carry the indentation.
	vtok := gtoken.New(value, strings.Repeat(" ", indent)+value, pos())
	lit.Value = gast.String(vtok)
	return lit
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
	if ls, ok := v.(literalString); ok {
		v = string(ls)
		info.literal = string(ls)
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
			if info.IsMulti() && blockLiteralSafe(str) {
				// Preserve multi-line format: goccy renders plain strings
				// containing newlines in literal style.
				return str, nil
			}
			return rawScalar(strconv.Quote(str)), nil

		case info.IsMulti() && blockLiteralSafe(str):
			// A single-line string written as a multi-line CUE literal
			// keeps its block form; see [encInfo.literal].
			return literalString(str), nil

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

// shouldQuote indicates that a string must be double quoted to be
// decoded back unharmed: it may be a YAML 1.1 legacy value, a scalar
// that YAML decodes as another type such as a number or an infinity,
// or contain characters that a plain scalar cannot carry, such as tabs
// or unprintable characters.
func shouldQuote(str string) bool {
	return str == "" || legacyStrings[str] || useQuote().MatchString(str) ||
		decodesAsNonString(str) || strings.ContainsRune(str, '\t') || yamlUnprintable(str)
}

// yamlUnprintable reports whether the string contains characters that
// YAML can only represent inside a double quoted scalar: control
// characters other than tab and newline, line breaks other than '\n'
// (parsers normalize '\r', and YAML 1.1 parsers treat NEL, LS, and PS
// as line breaks too), noncharacters, or bytes that are not valid
// UTF-8.
func yamlUnprintable(s string) bool {
	for i, r := range s {
		switch {
		case r == '\t' || r == '\n':
		case r < 0x20 || r == 0x7F || r == 0x85 || r == 0x2028 || r == 0x2029 || r == 0xFFFE || r == 0xFFFF:
			return true
		case r == utf8.RuneError:
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				return true
			}
		}
	}
	return false
}

// blockLiteralSafe reports whether a string survives a round-trip
// through a YAML literal block scalar unchanged, mirroring the
// conditions under which yaml.v3 allowed block styles. No line may end
// in a space: trailing whitespace is invisible padding in a block
// scalar, and a final all-space line is dropped entirely. Characters
// which require escaping need double quotes instead. A leading space
// or tab would need an explicit indentation indicator, which goccy
// does not emit.
func blockLiteralSafe(s string) bool {
	if len(s) == 0 || s[0] == ' ' || s[0] == '\t' {
		return false
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasSuffix(line, " ") {
			return false
		}
	}
	return !yamlUnprintable(s)
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
		// resolves more numbers than goccy's lexer; see
		// [decoder.scalarString]. Unlike the decoder, we do not except
		// bad YAML 1.1 octals like `0778` (rxAnyOctalYaml11): our decoder
		// reads them back as strings, but decoders such as yaml.v3
		// resolve them as floats, so they must stay quoted.
		if _, ok := yamlV3SpecialFloats[s]; ok {
			return true
		}
		return yamlV3NumberKind(s) != token.ILLEGAL
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
