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
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
)

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
func Encode(n ast.Node) (b []byte, err error) {
	e := &yamlWriter{}
	if err := e.encodeTop(n); err != nil {
		return nil, err
	}
	return e.buf.Bytes(), nil
}

// yamlWriter emits YAML text from CUE AST nodes.
type yamlWriter struct {
	buf    bytes.Buffer
	indent int // current indentation level in spaces
}

func (w *yamlWriter) writeIndent() {
	for i := 0; i < w.indent; i++ {
		w.buf.WriteByte(' ')
	}
}

func (w *yamlWriter) encodeTop(n ast.Node) error {
	switch x := n.(type) {
	case *ast.File:
		return w.encodeDecls(x.Decls)
	case *ast.StructLit:
		// A top-level struct with braces on same line is flow style.
		line := x.Lbrace.Line()
		if line > 0 && line == x.Rbrace.Line() {
			return w.encodeFlowMapping(x.Elts)
		}
		return w.encodeDecls(x.Elts)
	default:
		return w.encodeValue(n)
	}
}

func (w *yamlWriter) encodeDecls(decls []ast.Decl) error {
	docForNext := strings.Builder{}
	hasEmbed := false
	for _, d := range decls {
		switch x := d.(type) {
		default:
			return errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", astinternal.DebugStr(x), x)

		case *ast.Package:
			continue

		case *ast.CommentGroup:
			// Accumulate standalone comment groups to be written before the next field.
			if docForNext.Len() > 0 {
				docForNext.WriteString("\n\n")
			}
			docForNext.WriteString(docToYAMLComment(x))
			continue

		case *ast.Attribute:
			continue

		case *ast.Field:
			if !internal.IsRegularField(x) {
				return errors.Newf(x.TokenPos, "yaml: definition or hidden fields not allowed")
			}
			if x.Constraint != token.ILLEGAL {
				return errors.Newf(x.TokenPos, "yaml: optional fields not allowed")
			}
			if hasEmbed {
				return errors.Newf(x.TokenPos, "yaml: embedding mixed with fields")
			}
			name, _, err := ast.LabelName(x.Label)
			if err != nil {
				return errors.Newf(x.Label.Pos(), "yaml: only literal labels allowed")
			}

			// Write head comments from the field's label.
			w.writeNodeHeadComments(x.Label)

			// Write head comments accumulated from comment groups.
			if docForNext.Len() > 0 {
				w.writeIndent()
				w.buf.WriteString(docForNext.String())
				w.buf.WriteByte('\n')
				docForNext.Reset()
			}

			// Write head comments from the field itself.
			w.writeNodeHeadComments(x)

			// Write the key.
			w.writeIndent()
			w.writeYAMLKey(name)
			w.buf.WriteString(":")

			// Check for @yaml(,tag="...") attribute.
			yamlTag, err := extractYAMLTag(x.Attrs)
			if err != nil {
				return err
			}
			if yamlTag != "" {
				w.buf.WriteByte(' ')
				w.buf.WriteString(yamlTag)
			}

			// Write the value.
			if err := w.encodeFieldValue(x.Value); err != nil {
				return err
			}

			// Write line comment.
			w.writeNodeLineComment(x)
			w.writeNodeLineComment(x.Value)
			w.buf.WriteByte('\n')

			// Write foot comments.
			w.writeNodeFootComments(x)

		case *ast.EmbedDecl:
			if hasEmbed {
				return errors.Newf(x.Pos(), "yaml: multiple embedded values")
			}
			hasEmbed = true
			if docForNext.Len() > 0 {
				w.buf.WriteString(docForNext.String())
				docForNext.Reset()
			}
			w.writeNodeHeadComments(x)
			if err := w.encodeValue(x.Expr); err != nil {
				return err
			}
			w.writeNodeLineComment(x)
			w.buf.WriteByte('\n')
			w.writeNodeFootComments(x)
		}
	}

	// Trailing standalone comments.
	if docForNext.Len() > 0 {
		w.buf.WriteString(docForNext.String())
	}

	return nil
}

// encodeFieldValue writes the value portion of a mapping entry.
// For block-style structs/lists, it writes a newline then indented content.
// For flow-style or scalars, it writes on the same line.
func (w *yamlWriter) encodeFieldValue(n ast.Node) error {
	switch x := n.(type) {
	case *ast.StructLit:
		line := x.Lbrace.Line()
		if line > 0 && line == x.Rbrace.Line() {
			// Flow style: {key: val, ...}
			w.buf.WriteByte(' ')
			return w.encodeFlowMapping(x.Elts)
		}
		// Block style: indented on next lines.
		w.buf.WriteByte('\n')
		w.indent += 2
		err := w.encodeDecls(x.Elts)
		w.indent -= 2
		return err

	case *ast.ListLit:
		line := x.Lbrack.Line()
		if line > 0 && line == x.Rbrack.Line() {
			// Flow style: [val, ...]
			w.buf.WriteByte(' ')
			return w.encodeFlowSequence(x.Elts)
		}
		// Block style: - entries. Use yaml.v3-compatible compact notation
		// where the `-` is indented to the same level as the key.
		w.buf.WriteByte('\n')
		w.indent += 2
		err := w.encodeBlockSequence(x.Elts)
		w.indent -= 2
		return err

	default:
		w.buf.WriteByte(' ')
		return w.encodeValue(n)
	}
}

func (w *yamlWriter) encodeValue(n ast.Node) error {
	switch x := n.(type) {
	case *ast.BasicLit:
		return w.encodeScalar(x)
	case *ast.UnaryExpr:
		b, ok := x.X.(*ast.BasicLit)
		if ok && x.Op == token.SUB && (b.Kind == token.INT || b.Kind == token.FLOAT) {
			w.buf.WriteByte('-')
			return w.encodeScalar(b)
		}
		return errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", astinternal.DebugStr(x), x)
	case *ast.StructLit:
		line := x.Lbrace.Line()
		if line > 0 && line == x.Rbrace.Line() {
			return w.encodeFlowMapping(x.Elts)
		}
		return w.encodeDecls(x.Elts)
	case *ast.ListLit:
		line := x.Lbrack.Line()
		if line > 0 && line == x.Rbrack.Line() {
			return w.encodeFlowSequence(x.Elts)
		}
		return w.encodeBlockSequence(x.Elts)
	default:
		return errors.Newf(n.Pos(), "yaml: unsupported node %s (%T)", astinternal.DebugStr(n), n)
	}
}

func (w *yamlWriter) encodeScalar(b *ast.BasicLit) error {
	switch b.Kind {
	case token.INT:
		value := b.Value
		// Convert CUE number formats to YAML-compatible form.
		// CUE supports SI suffixes (1K → 1000) and underscores that YAML doesn't.
		// Binary (0b), hex (0x), and octal (0o) are preserved as yaml.v3 did.
		var ni literal.NumInfo
		if err := literal.ParseNum(value, &ni); err != nil {
			return err
		}
		// ni.String() normalizes everything to decimal. We only want to
		// normalize CUE-specific features (SI suffixes like 1K → 1000,
		// and underscores like 1_000 → 1000), while preserving the base
		// (0b, 0x, 0o) which YAML understands.
		if ni.Multiplier() != 0 || ni.UseSep {
			// Has SI suffix or separators: use fully normalized form.
			w.buf.WriteString(ni.String())
		} else {
			// Preserve original format (including 0b, 0x, 0o prefix).
			w.buf.WriteString(value)
		}
		return nil

	case token.FLOAT:
		w.buf.WriteString(b.Value)
		return nil

	case token.TRUE:
		w.buf.WriteString("true")
		return nil

	case token.FALSE:
		w.buf.WriteString("false")
		return nil

	case token.NULL:
		w.buf.WriteString("null")
		return nil

	case token.STRING:
		info, nStart, _, err := literal.ParseQuotes(b.Value, b.Value)
		if err != nil {
			return err
		}
		str, err := info.Unquote(b.Value[nStart:])
		if err != nil {
			return fmt.Errorf("invalid string: %v", err)
		}

		switch {
		case !info.IsDouble():
			// Bytes literal → !!binary.
			w.buf.WriteString("!!binary ")
			w.buf.WriteString(base64.StdEncoding.EncodeToString([]byte(str)))

		case info.IsMulti():
			// Multi-line string → literal block scalar.
			w.buf.WriteString("|-")
			lines := strings.Split(str, "\n")
			for _, line := range lines {
				w.buf.WriteByte('\n')
				w.writeIndent()
				w.buf.WriteString("  ")
				w.buf.WriteString(line)
			}

		default:
			if shouldQuote(str) {
				w.buf.WriteByte('\'')
				w.buf.WriteString(strings.ReplaceAll(str, "'", "''"))
				w.buf.WriteByte('\'')
			} else {
				w.buf.WriteString(str)
			}
		}
		return nil

	default:
		return errors.Newf(b.Pos(), "unknown literal type %v", b.Kind)
	}
}

func (w *yamlWriter) encodeFlowMapping(decls []ast.Decl) error {
	w.buf.WriteByte('{')
	first := true
	for _, d := range decls {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		if !first {
			w.buf.WriteString(", ")
		}
		first = false
		name, _, err := ast.LabelName(f.Label)
		if err != nil {
			return err
		}
		w.writeYAMLKey(name)
		w.buf.WriteString(": ")
		if err := w.encodeValue(f.Value); err != nil {
			return err
		}
	}
	w.buf.WriteByte('}')
	return nil
}

func (w *yamlWriter) encodeFlowSequence(elts []ast.Expr) error {
	w.buf.WriteByte('[')
	for i, e := range elts {
		if i > 0 {
			w.buf.WriteString(", ")
		}
		if err := w.encodeValue(e); err != nil {
			return err
		}
	}
	w.buf.WriteByte(']')
	return nil
}

func (w *yamlWriter) encodeBlockSequence(elts []ast.Expr) error {
	for _, e := range elts {
		w.writeIndent()
		// For struct elements in a list, the first field goes after "- "
		// and subsequent fields are indented to align.
		if sl, ok := e.(*ast.StructLit); ok {
			line := sl.Lbrace.Line()
			isFlow := line > 0 && line == sl.Rbrace.Line()
			if isFlow {
				w.buf.WriteString("- ")
				if err := w.encodeFlowMapping(sl.Elts); err != nil {
					return err
				}
				w.buf.WriteByte('\n')
			} else {
				// Write struct fields with "- " prefix for the first.
				for i, d := range sl.Elts {
					f, ok := d.(*ast.Field)
					if !ok {
						continue
					}
					name, _, err := ast.LabelName(f.Label)
					if err != nil {
						return err
					}
					if i == 0 {
						w.buf.WriteString("- ")
					} else {
						w.writeIndent()
						w.buf.WriteString("  ")
					}
					w.writeYAMLKey(name)
					w.buf.WriteByte(':')
					if err := w.encodeFieldValue(f.Value); err != nil {
						return err
					}
					w.buf.WriteByte('\n')
				}
			}
		} else {
			w.buf.WriteString("- ")
			if err := w.encodeValue(e); err != nil {
				return err
			}
			w.buf.WriteByte('\n')
		}
	}
	return nil
}

// writeYAMLKey writes a mapping key, quoting if necessary.
func (w *yamlWriter) writeYAMLKey(name string) {
	if shouldQuote(name) {
		w.buf.WriteByte('"')
		w.buf.WriteString(name)
		w.buf.WriteByte('"')
	} else {
		w.buf.WriteString(name)
	}
}

// Comment helpers

func (w *yamlWriter) writeNodeHeadComments(n ast.Node) {
	for _, c := range ast.Comments(n) {
		if !c.Doc {
			continue
		}
		w.writeIndent()
		w.buf.WriteString(docToYAMLComment(c))
		w.buf.WriteByte('\n')
	}
}

func (w *yamlWriter) writeNodeLineComment(n ast.Node) {
	for _, c := range ast.Comments(n) {
		if !c.Line {
			continue
		}
		w.buf.WriteByte(' ')
		w.buf.WriteString(docToYAMLComment(c))
	}
}

func (w *yamlWriter) writeNodeFootComments(n ast.Node) {
	for _, c := range ast.Comments(n) {
		if c.Position <= 0 || c.Doc || c.Line {
			continue
		}
		if c.Pos().RelPos() == token.NewSection {
			w.buf.WriteByte('\n')
		}
		w.writeIndent()
		w.buf.WriteString(docToYAMLComment(c))
		w.buf.WriteByte('\n')
	}
}

// shouldQuote indicates that a string may be a YAML 1.1 legacy value and that
// the string should be quoted.
func shouldQuote(str string) bool {
	return legacyStrings[str] || useQuote().MatchString(str)
}

// This regular expression conservatively matches any date, time string,
// or base60 float.
var useQuote = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[\-+0-9:\. \t]+([-:]|[tT])[\-+0-9:\. \t]+[zZ]?$|^0x[a-fA-F0-9]+$`)
})

// legacyStrings contains a map of fixed strings with special meaning for any
// type in the YAML Tag registry as used in YAML 1.1.
var legacyStrings = map[string]bool{
	"y": true, "Y": true, "yes": true, "Yes": true, "YES": true,
	"n": true, "N": true, "t": true, "T": true, "f": true, "F": true,
	"no": true, "No": true, "NO": true,
	"true": true, "True": true, "TRUE": true,
	"false": true, "False": true, "FALSE": true,
	"on": true, "On": true, "ON": true,
	"off": true, "Off": true, "OFF": true,
	".Nan": true,
}

// extractYAMLTag looks for @yaml(,tag="...") attribute and returns the tag value.
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

// docToYAMLComment converts a CUE CommentGroup to a YAML comment string.
func docToYAMLComment(c *ast.CommentGroup) string {
	s := c.Text()
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = "#"
		} else {
			lines[i] = "# " + l
		}
	}
	return strings.Join(lines, "\n")
}
