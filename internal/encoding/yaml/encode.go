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
	"math/big"
	"strings"

	"gopkg.in/yaml.v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// Encode converts a CUE AST to YAML.
//
// The given file must only contain values that can be directly supported by
// YAML:
//    Type          Restrictions
//    BasicLit
//    File          no imports, aliases, or definitions
//    StructLit     no embeddings, aliases, or definitions
//    List
//    Field         must be regular; label must be a BasicLit or Ident
//    CommentGroup
//
//    TODO: support anchors through Ident.
func Encode(n ast.Node) (b []byte, err error) {
	y, err := encode(n)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(y)
}

func encode(n ast.Node) (y *yaml.Node, err error) {
	switch x := n.(type) {
	case *ast.BasicLit:
		y, err = encodeScalar(x)

	case *ast.ListLit:
		y, err = encodeExprs(x.Elts)
		line := x.Lbrack.Line()
		if err == nil && line > 0 && line == x.Rbrack.Line() {
			y.Style = yaml.FlowStyle
		}

	case *ast.StructLit:
		y, err = encodeDecls(x.Elts)
		line := x.Lbrace.Line()
		if err == nil && line > 0 && line == x.Rbrace.Line() {
			y.Style = yaml.FlowStyle
		}

	case *ast.File:
		y, err = encodeDecls(x.Decls)

	case *ast.UnaryExpr:
		b, ok := x.X.(*ast.BasicLit)
		if ok && x.Op == token.SUB && (b.Kind == token.INT || b.Kind == token.FLOAT) {
			y, err = encodeScalar(b)
			if !strings.HasPrefix(y.Value, "-") {
				y.Value = "-" + y.Value
				break
			}
		}
		return nil, errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", internal.DebugStr(x), x)
	default:
		return nil, errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", internal.DebugStr(x), x)
	}
	if err != nil {
		return nil, err
	}
	addDocs(n, y, y)
	return y, nil
}

func encodeScalar(b *ast.BasicLit) (n *yaml.Node, err error) {
	n = &yaml.Node{Kind: yaml.ScalarNode}

	switch b.Kind {
	case token.INT:
		var x big.Int
		if err := setNum(n, b.Value, &x); err != nil {
			return nil, err
		}

	case token.FLOAT:
		var x big.Float
		if err := setNum(n, b.Value, &x); err != nil {
			return nil, err
		}

	case token.TRUE, token.FALSE, token.NULL:
		n.Value = b.Value

	case token.STRING:
		str, err := literal.Unquote(b.Value)
		if err != nil {
			return nil, err
		}
		n.SetString(str)

	default:
		return nil, errors.Newf(b.Pos(), "unknown literal type %v", b.Kind)
	}
	return n, nil
}

func setNum(n *yaml.Node, s string, x interface{}) error {
	if yaml.Unmarshal([]byte(s), x) == nil {
		n.Value = s
		return nil
	}

	var ni literal.NumInfo
	if err := literal.ParseNum(s, &ni); err != nil {
		return err
	}
	n.Value = ni.String()
	return nil
}

func encodeExprs(exprs []ast.Expr) (n *yaml.Node, err error) {
	n = &yaml.Node{Kind: yaml.SequenceNode}

	for _, elem := range exprs {
		e, err := encode(elem)
		if err != nil {
			return nil, err
		}
		n.Content = append(n.Content, e)
	}
	return n, nil
}

// encodeDecls converts a sequence of declarations to a value. If it encounters
// an embedded value, it will return this expression. This is more relaxed for
// structs than is currently allowed for CUE, but the expectation is that this
// will be allowed at some point. The input would still be illegal CUE.
func encodeDecls(decls []ast.Decl) (n *yaml.Node, err error) {
	n = &yaml.Node{Kind: yaml.MappingNode}

	docForNext := strings.Builder{}
	var lastHead, lastFoot *yaml.Node
	hasEmbed := false
	for _, d := range decls {
		switch x := d.(type) {
		default:
			return nil, errors.Newf(x.Pos(), "yaml: unsupported node %s (%T)", internal.DebugStr(x), x)

		case *ast.Package:
			if len(n.Content) > 0 {
				return nil, errors.Newf(x.Pos(), "invalid package clause")
			}
			continue

		case *ast.CommentGroup:
			docForNext.WriteString(docToYAML(x))
			docForNext.WriteString("\n\n")
			continue

		case *ast.Field:
			if x.Token == token.ISA {
				return nil, errors.Newf(x.TokenPos, "yaml: definition not allowed")
			}
			if x.Optional != token.NoPos {
				return nil, errors.Newf(x.Optional, "yaml: optional fields not allowed")
			}
			if hasEmbed {
				return nil, errors.Newf(x.TokenPos, "yaml: embedding mixed with fields")
			}
			name, _, err := ast.LabelName(x.Label)
			if err != nil {
				return nil, errors.Newf(x.Label.Pos(), "yaml: only literal labels allowed")
			}

			label := &yaml.Node{}
			addDocs(x.Label, label, label)
			label.SetString(name)

			value, err := encode(x.Value)
			if err != nil {
				return nil, err
			}
			lastHead = label
			lastFoot = value
			addDocs(x, label, value)
			n.Content = append(n.Content, label)
			n.Content = append(n.Content, value)

		case *ast.EmbedDecl:
			if hasEmbed {
				return nil, errors.Newf(x.Pos(), "yaml: multiple embedded values")
			}
			hasEmbed = true
			e, err := encode(x.Expr)
			if err != nil {
				return nil, err
			}
			addDocs(x, e, e)
			lastHead = e
			lastFoot = e
			n.Content = append(n.Content, e)
		}
		if docForNext.Len() > 0 {
			docForNext.WriteString(lastHead.HeadComment)
			lastHead.HeadComment = docForNext.String()
			docForNext.Reset()
		}
	}

	if docForNext.Len() > 0 && lastFoot != nil {
		if !strings.HasSuffix(lastFoot.FootComment, "\n") {
			lastFoot.FootComment += "\n"
		}
		n := docForNext.Len()
		lastFoot.FootComment += docForNext.String()[:n-1]
	}

	if hasEmbed {
		return n.Content[0], nil
	}

	return n, nil
}

// addDocs prefixes head, replaces line and appends foot comments.
func addDocs(n ast.Node, h, f *yaml.Node) {
	head := ""
	isDoc := false
	for _, c := range ast.Comments(n) {
		switch {
		case c.Line:
			f.LineComment = docToYAML(c)

		case c.Position > 0:
			if f.FootComment != "" {
				f.FootComment += "\n\n"
			} else if relPos := c.Pos().RelPos(); relPos == token.NewSection {
				f.FootComment += "\n"
			}
			f.FootComment += docToYAML(c)

		default:
			if head != "" {
				head += "\n\n"
			}
			head += docToYAML(c)
			isDoc = isDoc || c.Doc
		}
	}

	if head != "" {
		if h.HeadComment != "" || !isDoc {
			head += "\n\n"
		}
		h.HeadComment = head + h.HeadComment
	}
}

// docToYAML converts a CUE CommentGroup to a YAML comment string. This ensures
// that comments with empty lines get properly converted.
func docToYAML(c *ast.CommentGroup) string {
	s := c.Text()
	if strings.HasSuffix(s, "\n") { // always true
		s = s[:len(s)-1]
	}
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
