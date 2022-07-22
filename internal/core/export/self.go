// Copyright 2022 CUE Authors
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
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/dep"
)

// This file contains the algorithm to contain a self-contained CUE file.

// TODO:
// - Handle below edge cases where a reference directly references the root
//   of the exported main tree.
// - Inline smallish structs that themselves do not have outside
//   references.
// - Overall better inlining.
// - Consider a shorthand for the `let X = { #x: foo }` annotation. Possibly
//   allow `#{}`, or allow "let definitions", like `let #X = {}`.

// initSelfContainedCloser initializes a selfContainedCloser if either a subtree
// is exported or imports need to be removed. It will not initialize one if
// neither is the case.
func (e *exporter) initSelfContainedCloser(v *adt.Vertex) {
	if !e.cfg.ExpandImports &&
		!e.cfg.SelfContained &&
		v.Parent == nil {
		return
	}

	s := &selfContainedCloser{}
	e.selfContainedCloser = s
	s.x = e

	s.depsMap = map[*adt.Vertex]*depData{}
	s.refMap = map[adt.Resolver]*refData{}

	s.linkDependencies(v)
}

func (e *exporter) completeSelfContained(f *ast.File) {
	s := e.selfContainedCloser
	if s == nil {
		return
	}
	for _, d := range s.deps {
		if !d.isRootArg() {
			continue
		}
		s.addExternal(d)
	}
	f.Decls = append(f.Decls, s.decls...)
}

type selfContainedCloser struct {
	x *exporter

	deps    []*depData
	depsMap map[*adt.Vertex]*depData

	refs   []*refData
	refMap map[adt.Resolver]*refData

	decls []ast.Decl
}

type depData struct {
	parent *depData

	dstNode   *adt.Vertex
	dstImport *adt.ImportReference

	ident        adt.Feature
	path         []adt.Feature
	useCount     int // Other reference using this vertex
	included     bool
	needTopLevel bool
}

func (d *depData) isRootArg() bool {
	return d.ident != 0
}

func (d *depData) usageCount() int {
	return getParent(d).useCount
}

type refData struct {
	dst *depData
	ref ast.Expr
}

func (v *depData) node() *adt.Vertex {
	return v.dstNode
}

func (s *selfContainedCloser) linkDependencies(v *adt.Vertex) {
	s.markDeps(v)

	// Explicitly add the root of the configuration.
	s.markIncluded(v)

	// Link one parent up
	for _, d := range s.depsMap {
		s.markParentsPass1(d)
	}

	// Get transitive closure of parents.
	for _, d := range s.depsMap {
		if d.parent != nil {
			d.parent = getParent(d)
			d.parent.useCount++
		}
	}

	// Compute the paths for the parent nodes.
	for _, d := range s.deps {
		if d.parent == nil {
			s.makeParentPath(d)
		}
	}
}

func getParent(d *depData) *depData {
	for ; d.parent != nil; d = d.parent {
	}
	return d
}

func (s *selfContainedCloser) markDeps(v *adt.Vertex) {
	// TODO: sweep all child nodes and mark as no need for recursive checks.

	dep.VisitAll(s.x.ctx, v, func(d dep.Dependency) error {
		node := d.Node

		switch {
		case
			// Already done.
			s.refMap[d.Reference] != nil,

			// Only record nodes within import if we want to expand imports.
			!s.x.cfg.ExpandImports && d.Import() != nil,

			// This may happen for DynamicReferences.
			node.Status() == adt.Unprocessed:

			return nil
		}

		data, ok := s.depsMap[node]
		if !ok {
			data = &depData{
				dstNode:   node,
				dstImport: d.Import(),
			}
			s.depsMap[node] = data
			s.deps = append(s.deps, data)
		}
		data.useCount++

		ref := &refData{dst: data}
		s.refs = append(s.refs, ref)
		s.refMap[d.Reference] = ref

		if !ok {
			s.markDeps(node)
		}

		return nil
	})
}

// markIncluded marks all referred nodes that are within the normal tree to be
// exported.
func (s *selfContainedCloser) markIncluded(v *adt.Vertex) {
	d, ok := s.depsMap[v]
	if !ok {
		d = &depData{dstNode: v}
		s.depsMap[v] = d
	}
	d.included = true

	for _, a := range v.Arcs {
		s.markIncluded(a)
	}
}

// markParentPass1 marks the furthest ancestor node for which there is a
// dependency as its parent. Only dependencies that do not have a parent
// will be assigned to hidden reference.
func (s *selfContainedCloser) markParentsPass1(d *depData) {
	for p := d.node().Parent; p != nil; p = p.Parent {
		if v, ok := s.depsMap[p]; ok {
			d.parent = v
		}
	}
}

func (s *selfContainedCloser) makeParentPath(d *depData) {
	if d.parent != nil {
		panic("not a parent")
	}

	if d.included || d.isRootArg() {
		return
	}

	var f adt.Feature

	if path := d.dstNode.Path(); len(path) > 0 {
		f = path[len(path)-1]
	} else if imp := d.dstImport; imp != nil {
		f = imp.Label
	} else {
		panic("unexpected zero path length")
	}

	str := f.IdentString(s.x.ctx)
	str = strings.TrimLeft(str, "_#")
	str = strings.ToUpper(str)
	uf, _ := s.x.uniqueFeature(str)

	d.path = []adt.Feature{uf}
	d.ident = uf

	// Make it a definition if we need it.
	if d.dstNode.IsRecursivelyClosed() {
		d.path = append(d.path, adt.MakeIdentLabel(s.x.ctx, "#x", ""))
	}
}

// makeAlternativeReference computes the alternative path for the reference.
func (s *selfContainedCloser) makeAlternativeReference(r *refData) ast.Expr {
	d := r.dst

	// Determine if the reference can be inline.

	var path []adt.Feature
	if d.parent == nil {
		// Get canonical vertexData.
		path = d.path
	} else {
		parent := d.parent.node()
		count := 0
		for p := d.node(); p != parent; p = p.Parent {
			count++
		}

		path = d.node().Path()
		if count > len(path) {
			// We have an internal reference, which does not need to be changed.
			return nil
		}

		path = path[len(path)-count:]
		path = append(d.parent.path, path...)
	}

	if len(path) == 0 {
		path = append(path, s.x.ctx.StringLabel("ROOT"))
	}

	var x ast.Expr = s.x.ident(path[0])

	for _, f := range path[1:] {
		if f.IsInt() {
			x = &ast.IndexExpr{
				X:     x,
				Index: ast.NewLit(token.INT, strconv.Itoa(f.Index())),
			}
		} else {
			x = &ast.SelectorExpr{
				X:   x,
				Sel: s.x.stringLabel(f),
			}
		}
	}

	return x
}

// refExpr returns a substituted expression for a given reference, or nil if
// there are no changes. This function implements most of the policy to decide
// when an expression can be inlined.
func (s *selfContainedCloser) refExpr(r adt.Resolver) ast.Expr {
	ref, ok := s.refMap[r]
	if !ok {
		return nil
	}

	dst := ref.dst
	n := dst.node()

	// Inline value, but only when this may not lead to an exponential
	// expansion. We allow inlining when a value is only used once, or when
	// it is a simple concrete scalar value.
	switch {
	case dst.included:
		// Keep references that point inside the hoisted vertex.
		// TODO: force hoisting. This would be akin to taking the interpretation
		// that references that initially point outside the included vertex
		// are external inputs too, even if they eventually point inside.
	case s.x.inDefinition == 0 && n.IsRecursivelyClosed():
		// We need to wrap the value in a definition.
	case dst.usageCount() == 0:
		// The root value itself.
	case dst.usageCount() == 1 && s.x.inExpression == 0:
		// Used only once.
		fallthrough
	case n.IsConcrete() && len(n.Arcs) == 0:
		// Simple scalar value.
		return s.x.expr(nil, n)
	}

	if r := s.makeAlternativeReference(ref); r != nil {
		dst.needTopLevel = true
		return r
	}

	return nil
}

// addExternal converts a vertex for an external reference.
func (s *selfContainedCloser) addExternal(d *depData) {
	if !d.needTopLevel {
		return
	}

	expr := s.x.expr(nil, d.node())

	if len(d.path) > 1 {
		expr = ast.NewStruct(&ast.Field{
			Label: s.x.ident(d.path[1]),
			Value: expr,
		})
	}
	let := &ast.LetClause{
		Ident: s.x.ident(d.path[0]),
		Expr:  expr,
	}

	ast.SetRelPos(let, token.NewSection)

	path := s.x.ctx.PathToString(s.x.ctx, d.node().Path())
	var msg string
	if d.dstImport == nil {
		msg = fmt.Sprintf("//cue:path: %s", path)
	} else {
		pkg := d.dstImport.ImportPath.SelectorString(s.x.ctx)
		if path == "" {
			msg = fmt.Sprintf("//cue:path: %s", pkg)
		} else {
			msg = fmt.Sprintf("//cue:path: %s.%s", pkg, path)
		}
	}
	cg := &ast.CommentGroup{
		Doc:  true,
		List: []*ast.Comment{{Text: msg}},
	}
	ast.SetRelPos(cg, token.NewSection)
	ast.AddComment(let, cg)

	s.decls = append(s.decls, let)
}
