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

package export

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
)

type Profile struct {
	Simplify bool

	// TODO:
	// IncludeDocs
}

var Simplified = &Profile{
	Simplify: true,
}

var Raw = &Profile{}

// Concrete

// Def exports v as a definition.
func Def(r adt.Runtime, v *adt.Vertex) (*ast.File, errors.Error) {
	p := Profile{}
	return p.Def(r, v)
}

// Def exports v as a definition.
func (p *Profile) Def(r adt.Runtime, v *adt.Vertex) (*ast.File, errors.Error) {
	e := newExporter(p, r, v)
	expr := e.expr(v)
	return e.toFile(expr)
}

// // TODO: remove: must be able to fall back to arcs if there are no
// // conjuncts.
// func Conjuncts(conjuncts ...adt.Conjunct) (*ast.File, errors.Error) {
// 	var e Exporter
// 	// for now just collect and turn into an big conjunction.
// 	var a []ast.Expr
// 	for _, c := range conjuncts {
// 		a = append(a, e.expr(c.Expr()))
// 	}
// 	return e.toFile(ast.NewBinExpr(token.AND, a...))
// }

func Expr(r adt.Runtime, n adt.Expr) (ast.Expr, errors.Error) {
	return Simplified.Expr(r, n)
}

func (p *Profile) Expr(r adt.Runtime, n adt.Expr) (ast.Expr, errors.Error) {
	e := newExporter(p, r, nil)
	return e.expr(n), nil
}

func (e *exporter) toFile(x ast.Expr) (*ast.File, errors.Error) {
	f := &ast.File{}

	switch st := x.(type) {
	case nil:
		panic("null input")

	case *ast.StructLit:
		f.Decls = st.Elts

	default:
		f.Decls = append(f.Decls, &ast.EmbedDecl{Expr: x})
	}

	if err := astutil.Sanitize(f); err != nil {
		err := errors.Promote(err, "export")
		return f, errors.Append(e.errs, err)
	}

	return f, nil
}

// File

func Vertex(r adt.Runtime, n *adt.Vertex) (*ast.File, errors.Error) {
	return Simplified.Vertex(r, n)
}

func (p *Profile) Vertex(r adt.Runtime, n *adt.Vertex) (*ast.File, errors.Error) {
	e := exporter{
		cfg:   p,
		index: r,
	}
	v := e.value(n, n.Conjuncts...)

	return e.toFile(v)
}

func Value(r adt.Runtime, n adt.Value) (ast.Expr, errors.Error) {
	return Simplified.Value(r, n)
}

func (p *Profile) Value(r adt.Runtime, n adt.Value) (ast.Expr, errors.Error) {
	e := exporter{
		cfg:   p,
		index: r,
	}
	v := e.value(n)
	return v, e.errs
}

type exporter struct {
	cfg      *Profile
	errs     errors.Error
	concrete bool

	ctx *adt.OpContext

	index adt.StringIndexer

	// For resolving up references.
	stack []frame
}

func newExporter(p *Profile, r adt.Runtime, v *adt.Vertex) *exporter {
	return &exporter{
		cfg:   p,
		ctx:   eval.NewContext(r, v),
		index: r,
	}
}

type completeFunc func(scope *ast.StructLit, m adt.Node)

type frame struct {
	scope *ast.StructLit
	todo  []completeFunc

	// field to new field
	mapped map[adt.Node]ast.Node
}

// func (e *Exporter) pushFrame(d *adt.StructLit, s *ast.StructLit) (saved []frame) {
// 	saved := e.stack
// 	e.stack = append(e.stack, frame{scope: s, mapped: map[adt.Node]ast.Node{}})
// 	return saved
// }

// func (e *Exporter) popFrame(saved []frame) {
// 	f := e.stack[len(e.stack)-1]

// 	for _, f

// 	e.stack = saved
// }

// func (e *Exporter) promise(upCount int32, f completeFunc) {
// 	e.todo = append(e.todo, f)
// }

func (e *exporter) errf(format string, args ...interface{}) *ast.BottomLit {
	err := &exporterError{}
	e.errs = errors.Append(e.errs, err)
	return &ast.BottomLit{}
}

type errTODO errors.Error

type exporterError struct {
	errTODO
}
