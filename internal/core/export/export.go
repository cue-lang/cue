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
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
)

const debug = false

type Profile struct {
	Simplify bool

	// TakeDefaults is used in Value mode to drop non-default values.
	TakeDefaults bool

	// TODO:
	// IncludeDocs
	ShowOptional    bool
	ShowDefinitions bool
	ShowHidden      bool
	ShowDocs        bool
	ShowAttributes  bool

	// AllowErrorType
	// Use unevaluated conjuncts for these error types
	// IgnoreRecursive

	// TODO: recurse over entire tree to determine transitive closure
	// of what needs to be printed.
	// IncludeDependencies bool
}

var Simplified = &Profile{
	Simplify: true,
	ShowDocs: true,
}

var Final = &Profile{
	Simplify:     true,
	TakeDefaults: true,
}

var Raw = &Profile{
	ShowOptional:    true,
	ShowDefinitions: true,
	ShowHidden:      true,
	ShowDocs:        true,
}

var All = &Profile{
	Simplify:        true,
	ShowOptional:    true,
	ShowDefinitions: true,
	ShowHidden:      true,
	ShowDocs:        true,
	ShowAttributes:  true,
}

// Concrete

// Def exports v as a definition.
func Def(r adt.Runtime, v *adt.Vertex) (*ast.File, errors.Error) {
	return All.Def(r, v)
}

// Def exports v as a definition.
func (p *Profile) Def(r adt.Runtime, v *adt.Vertex) (*ast.File, errors.Error) {
	e := newExporter(p, r, v)
	if v.Label.IsDef() {
		e.inDefinition++
	}
	expr := e.expr(v)
	if v.Label.IsDef() {
		e.inDefinition--
		if s, ok := expr.(*ast.StructLit); ok {
			expr = ast.NewStruct(
				ast.Embed(ast.NewIdent("#_def")),
				ast.NewIdent("#_def"), s,
			)
		}
	}
	return e.toFile(v, expr)
}

func Expr(r adt.Runtime, n adt.Expr) (ast.Expr, errors.Error) {
	return Simplified.Expr(r, n)
}

func (p *Profile) Expr(r adt.Runtime, n adt.Expr) (ast.Expr, errors.Error) {
	e := newExporter(p, r, nil)
	return e.expr(n), nil
}

func (e *exporter) toFile(v *adt.Vertex, x ast.Expr) (*ast.File, errors.Error) {
	f := &ast.File{}

	pkgName := ""
	pkg := &ast.Package{}
	for _, c := range v.Conjuncts {
		f, _ := c.Source().(*ast.File)
		if f == nil {
			continue
		}

		if _, name, _ := internal.PackageInfo(f); name != "" {
			pkgName = name
		}

		if e.cfg.ShowDocs {
			if doc := internal.FileComment(f); doc != nil {
				ast.AddComment(pkg, doc)
			}
		}
	}

	if pkgName != "" {
		pkg.Name = ast.NewIdent(pkgName)
		f.Decls = append(f.Decls, pkg)
	}

	switch st := x.(type) {
	case nil:
		panic("null input")

	case *ast.StructLit:
		f.Decls = append(f.Decls, st.Elts...)

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

	return e.toFile(n, v)
}

func Value(r adt.Runtime, n adt.Value) (ast.Expr, errors.Error) {
	return Simplified.Value(r, n)
}

// Should take context.
func (p *Profile) Value(r adt.Runtime, n adt.Value) (ast.Expr, errors.Error) {
	e := exporter{
		ctx:   eval.NewContext(r, nil),
		cfg:   p,
		index: r,
	}
	v := e.value(n)
	return v, e.errs
}

type exporter struct {
	cfg  *Profile // Make value todo
	errs errors.Error

	ctx *adt.OpContext

	index adt.StringIndexer

	// For resolving up references.
	stack []frame

	inDefinition int // for close() wrapping.

	unique int
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

	docSources []adt.Conjunct

	// For resolving dynamic fields.
	field     *ast.Field
	labelExpr ast.Expr
	upCount   int32 // for off-by-one handling

	// labeled fields
	fields map[adt.Feature]entry
	let    map[adt.Expr]*ast.LetClause

	// field to new field
	mapped map[adt.Node]ast.Node
}

type entry struct {
	node ast.Node

	references []*ast.Ident
}

func (e *exporter) addField(label adt.Feature, n ast.Node) {
	frame := e.top()
	entry := frame.fields[label]
	entry.node = n
	frame.fields[label] = entry
}

func (e *exporter) pushFrame(conjuncts []adt.Conjunct) (s *ast.StructLit, saved []frame) {
	saved = e.stack
	s = &ast.StructLit{}
	e.stack = append(e.stack, frame{
		scope:      s,
		mapped:     map[adt.Node]ast.Node{},
		fields:     map[adt.Feature]entry{},
		docSources: conjuncts,
	})
	return s, saved
}

func (e *exporter) popFrame(saved []frame) {
	top := e.stack[len(e.stack)-1]

	for _, f := range top.fields {
		for _, r := range f.references {
			r.Node = f.node
		}
	}

	e.stack = saved
}

func (e *exporter) top() *frame {
	return &(e.stack[len(e.stack)-1])
}

func (e *exporter) frame(upCount int32) *frame {
	for i := len(e.stack) - 1; i >= 0; i-- {
		f := &(e.stack[i])
		if upCount <= (f.upCount - 1) {
			return f
		}
		upCount -= f.upCount
	}
	if debug {
		// This may be valid when exporting incomplete references. These are
		// not yet handled though, so find a way to catch them when debugging
		// printing of values that are supposed to be complete.
		panic("unreachable reference")
	}

	return &frame{}
}

func (e *exporter) setDocs(x adt.Node) {
	f := e.stack[len(e.stack)-1]
	f.docSources = []adt.Conjunct{adt.MakeRootConjunct(nil, x)}
	e.stack[len(e.stack)-1] = f
}

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
