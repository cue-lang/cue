// Copyright 2019 CUE Authors
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

package jsonschema

// TODO:
// - replace converter from YAML to CUE to CUE (schema) to CUE.
// - define OpenAPI definitions als CUE.

import (
	"fmt"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

const rootDefs = "def"

// A decoder converts JSON schema to CUE.
type decoder struct {
	cfg *Config

	errs    errors.Error
	imports map[string]*ast.Ident

	definitions []ast.Decl
}

// addImport registers
func (d *decoder) addImport(pkg string) *ast.Ident {
	ident, ok := d.imports[pkg]
	if !ok {
		ident = ast.NewIdent(pkg)
		d.imports[pkg] = ident
	}
	return ident
}

func (d *decoder) decode(inst *cue.Instance) *ast.File {
	root := state{decoder: d}
	expr, state := root.schemaState(inst.Value())

	var a []ast.Decl

	if pkgName := d.cfg.PkgName; pkgName != "" {
		pkg := &ast.Package{Name: ast.NewIdent(pkgName)}
		state.doc(pkg)

		a = append(a, pkg)
	} else if doc := state.comment(); doc != nil {
		a = append(a, doc)
	}

	var imports []string
	for k := range d.imports {
		imports = append(imports, k)
	}
	sort.Strings(imports)

	if len(imports) > 0 {
		x := &ast.ImportDecl{}
		for _, p := range imports {
			x.Specs = append(x.Specs, ast.NewImport(nil, p))
		}
		a = append(a, x)
	}

	tags := []string{}
	if state.jsonschema != "" {
		tags = append(tags, fmt.Sprintf("schema=%q", state.jsonschema))
	}
	if state.id != "" {
		tags = append(tags, fmt.Sprintf("id=%q", state.id))
	}
	if len(tags) > 0 {
		a = append(a, addTag("Schema", "jsonschema", strings.Join(tags, ",")))
	}

	if state.deprecated {
		a = append(a, addTag("Schema", "deprecated", ""))
	}

	f := &ast.Field{
		Label: ast.NewIdent("Schema"),
		Value: expr,
	}

	f.Token = token.ISA
	a = append(a, f)
	a = append(a, d.definitions...)

	return &ast.File{Decls: a}
}

func (d *decoder) errf(n cue.Value, format string, args ...interface{}) ast.Expr {
	d.warnf(n, format, args...)
	return &ast.BadExpr{From: n.Pos()}
}

func (d *decoder) warnf(n cue.Value, format string, args ...interface{}) {
	d.errs = errors.Append(d.errs, errors.Newf(n.Pos(), format, args...))
}

func (d *decoder) number(n cue.Value) ast.Expr {
	return n.Syntax(cue.Final()).(ast.Expr)
}

func (d *decoder) uint(n cue.Value) ast.Expr {
	_, err := n.Uint64()
	if err != nil {
		d.errf(n, "invalid uint")
	}
	return n.Syntax(cue.Final()).(ast.Expr)
}

func (d *decoder) bool(n cue.Value) ast.Expr {
	return n.Syntax(cue.Final()).(ast.Expr)
}

func (d *decoder) boolValue(n cue.Value) bool {
	x, err := n.Bool()
	if err != nil {
		d.errf(n, "invalid bool")
	}
	return x
}

func (d *decoder) string(n cue.Value) ast.Expr {
	return n.Syntax(cue.Final()).(ast.Expr)
}

func (d *decoder) strValue(n cue.Value) (s string, ok bool) {
	s, err := n.String()
	if err != nil {
		d.errf(n, "invalid string")
		return "", false
	}
	return s, true
}

// const draftCutoff = 5

type state struct {
	*decoder

	path []string

	pos cue.Value

	types        []cue.Value
	typeOptional bool
	kind         cue.Kind

	default_    ast.Expr
	examples    []ast.Expr
	title       string
	description string
	deprecated  bool
	jsonschema  string
	id          string

	conjuncts []ast.Expr

	obj         *ast.StructLit
	closeStruct bool
	patterns    []ast.Expr

	list *ast.ListLit
}

// finalize constructs a CUE type from the collected constraints.
func (s *state) finalize() (e ast.Expr) {
	if s.typeOptional || s.kind != 0 {
		if len(s.types) > 1 {
			s.errf(s.pos, "constraints require specific type")
		}
		s.types = nil
	}

	conjuncts := []ast.Expr{}
	disjuncts := []ast.Expr{}
	for _, n := range s.types {
		add := func(e ast.Expr) {
			disjuncts = append(disjuncts, setPos(e, n))
		}
		str, ok := s.strValue(n)
		if !ok {
			s.errf(n, "type value should be a string")
			return
		}
		switch str {
		case "null":
			// TODO: handle OpenAPI restrictions.
			add(ast.NewIdent("null"))
		case "boolean":
			add(ast.NewIdent("bool"))
		case "string":
			add(ast.NewIdent("string"))
		case "number":
			add(ast.NewIdent("number"))
		case "integer":
			add(ast.NewIdent("int"))
		case "array":
			if s.kind&cue.ListKind == 0 {
				add(ast.NewList(&ast.Ellipsis{}))
			}
		case "object":
			add(ast.NewStruct(&ast.Ellipsis{}))
		default:
			s.errf(n, "unknown type %q", n)
		}
	}
	if len(disjuncts) > 0 {
		conjuncts = append(conjuncts, ast.NewBinExpr(token.OR, disjuncts...))
	}

	conjuncts = append(conjuncts, s.conjuncts...)

	if s.obj != nil {
		if !s.closeStruct {
			s.obj.Elts = append(s.obj.Elts, &ast.Ellipsis{})
		}
		conjuncts = append(conjuncts, s.obj)
	}

	if len(conjuncts) == 0 {
		return ast.NewString(fmt.Sprint(s.pos))
	}

	e = ast.NewBinExpr(token.AND, conjuncts...)

	if s.default_ != nil {
		// check conditions where default can be skipped.
		switch x := s.default_.(type) {
		case *ast.ListLit:
			if s.kind == cue.ListKind && len(x.Elts) == 0 {
				return e
			}
		}
		e = ast.NewBinExpr(token.OR, e, &ast.UnaryExpr{Op: token.MUL, X: s.default_})
	}
	return e
}

func (s *state) comment() *ast.CommentGroup {
	// Create documentation.
	doc := strings.TrimSpace(s.title)
	if s.description != "" {
		if doc != "" {
			doc += "\n\n"
		}
		doc += s.description
		doc = strings.TrimSpace(doc)
	}
	// TODO: add examples as well?
	if doc == "" {
		return nil
	}
	return internal.NewComment(true, doc)
}

func (s *state) doc(n ast.Node) {
	doc := s.comment()
	if doc != nil {
		ast.SetComments(n, []*ast.CommentGroup{doc})
	}
}

func (s *state) add(e ast.Expr) {
	s.conjuncts = append(s.conjuncts, e)
}

func (s *state) schema(n cue.Value) ast.Expr {
	expr, _ := s.schemaState(n)
	// TODO: report unused doc.
	return expr
}

func (s *state) schemaState(n cue.Value) (ast.Expr, *state) {
	state := &state{path: s.path, pos: n, decoder: s.decoder}

	if n.Kind() != cue.StructKind {
		return s.errf(n, "schema expects mapping node, found %s", n.Kind()), state
	}

	// do multiple passes over the constraints to ensure they are done in order.
	for pass := 0; pass < 3; pass++ {
		state.processMap(n, func(key string, value cue.Value) {
			// Convert each constraint into a either a value or a functor.
			c := constraintMap[key]
			if c == nil {
				if pass == 0 {
					s.warnf(n, "unsupported constraint %q", key)
				}
				return
			}
			if c.phase == pass {
				c.fn(value, state)
			}
		})
	}

	return state.finalize(), state
}

func (s *state) value(n cue.Value) ast.Expr {
	switch n.Kind() {
	case cue.ListKind:
		a := []ast.Expr{}
		for i, _ := n.List(); i.Next(); {
			a = append(a, s.value(i.Value()))
		}
		return setPos(ast.NewList(a...), n)

	case cue.StructKind:
		a := []ast.Decl{}
		s.processMap(n, func(key string, n cue.Value) {
			a = append(a, &ast.Field{
				Label: ast.NewString(key),
				Value: s.value(n),
			})
		})
		a = append(a, &ast.Ellipsis{})
		return setPos(&ast.StructLit{Elts: a}, n)

	default:
		if !n.IsConcrete() {
			s.errf(n, "invalid non-concerte value")
		}
		return n.Syntax(cue.Final()).(ast.Expr)
	}
}

// processMap processes a yaml node, expanding merges.
//
// TODO: in some cases we can translate merges into CUE embeddings.
// This may also prevent exponential blow-up (as may happen when
// converting YAML to JSON).
func (s *state) processMap(n cue.Value, f func(key string, n cue.Value)) {
	saved := s.path
	defer func() { s.path = saved }()

	// TODO: intercept references to allow for optimized performance.
	for i, _ := n.Fields(); i.Next(); {
		key := i.Label()
		s.path = append(saved, key)
		f(key, i.Value())
	}
}

func list(n cue.Value) (a []cue.Value) {
	for i, _ := n.List(); i.Next(); {
		a = append(a, i.Value())
	}
	return a
}

// excludeFields returns a CUE expression that can be used to exclude the
// fields of the given declaration in a label expression. For instance, for
//
//    { foo: 1, bar: int }
//
// it creates
//
//    "^(foo|bar)$"
//
// which can be used in a label expression to define types for all fields but
// those existing:
//
//   [!~"^(foo|bar)$"]: string
//
func excludeFields(decls []ast.Decl) ast.Expr {
	var a []string
	for _, d := range decls {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		str, _, _ := ast.LabelName(f.Label)
		if str != "" {
			a = append(a, str)
		}
	}
	re := fmt.Sprintf("^(%s)$", strings.Join(a, "|"))
	return &ast.UnaryExpr{Op: token.NMAT, X: ast.NewString(re)}
}

func addTag(field, tag, value string) *ast.Field {
	return &ast.Field{
		Label: ast.NewIdent(field),
		Token: token.ISA,
		Value: ast.NewIdent("_"),
		Attrs: []*ast.Attribute{
			{Text: fmt.Sprintf("@%s(%s)", tag, value)},
		},
	}
}

func setPos(e ast.Expr, v cue.Value) ast.Expr {
	ast.SetPos(e, v.Pos())
	return e
}
