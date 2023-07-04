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

// Package dep analyzes dependencies between values.
package dep

import (
	"errors"

	"cuelang.org/go/internal/core/adt"
)

// TODO: for a public API, a better approach seems to be to have a single
// Visit method, with a configuration to set a bunch of orthogonal options.
// Here are some examples of the options:
//   - Dynamic:   evaluate and descend into computed fields.
//   - Recurse:   evaluate dependencies of subfields as well.
//   - Inner:     report dependencies within the root being visited.
//   - NonRooted: report dependencies that do not have a path to the root.
//
//   - ContinueOnError:  continue visiting even if there are errors.
//   [add more as they come up]

// A Dependency is a reference and the node that reference resolves to.
type Dependency struct {
	// Node is the referenced node.
	Node *adt.Vertex

	// Reference is the expression that referenced the node.
	Reference adt.Resolver

	pkg *adt.ImportReference

	top bool
}

// Import returns the import reference or nil if the reference was within
// the same package as the visited Vertex.
func (d *Dependency) Import() *adt.ImportReference {
	return d.pkg
}

// IsRoot reports whether the dependency is referenced by the root of the
// original Vertex passed to any of the Visit* functions, and not one of its
// descendent arcs. This always returns true for Visit().
func (d *Dependency) IsRoot() bool {
	return d.top
}

func (d *Dependency) Path() []adt.Feature {
	return nil
}

func importRef(r adt.Expr) *adt.ImportReference {
	switch x := r.(type) {
	case *adt.ImportReference:
		return x
	case *adt.SelectorExpr:
		return importRef(x.X)
	case *adt.IndexExpr:
		return importRef(x.X)
	}
	return nil
}

// VisitFunc is used for reporting dependencies.
type VisitFunc func(Dependency) error

// Visit calls f for all vertices referenced by the conjuncts of n without
// descending into the elements of list or fields of structs. Only references
// that do not refer to the conjuncts of n itself are reported. pkg indicates
// the the package within which n is contained, which is used for reporting
// purposes. It may be nil, indicating the main package.
func Visit(c *adt.OpContext, pkg *adt.ImportReference, n *adt.Vertex, f VisitFunc) error {
	return visit(c, pkg, n, f, false, true)
}

// VisitAll calls f for all vertices referenced by the conjuncts of n including
// those of descendant fields and elements. Only references that do not refer to
// the conjuncts of n itself are reported. pkg indicates the current
// package, which is used for reporting purposes.
func VisitAll(c *adt.OpContext, pkg *adt.ImportReference, n *adt.Vertex, f VisitFunc) error {
	return visit(c, pkg, n, f, true, true)
}

// VisitFields calls f for n and all its descendent arcs that have a conjunct
// that originates from a conjunct in n. Only the conjuncts of n that ended up
// as a conjunct in an actual field are visited and they are visited for each
// field in which the occurs. pkg indicates the current package, which is
// used for reporting purposes.
func VisitFields(c *adt.OpContext, pkg *adt.ImportReference, n *adt.Vertex, f VisitFunc) error {
	m := marked{}

	m.markExpr(n)

	dynamic(c, pkg, n, f, m, true)
	return nil
}

var empty *adt.Vertex

func init() {
	// TODO: Consider setting a non-nil BaseValue.
	empty = &adt.Vertex{}
	empty.ForceDone()
}

func visit(c *adt.OpContext, pkg *adt.ImportReference, n *adt.Vertex, f VisitFunc, all, top bool) (err error) {
	if c == nil {
		panic("nil context")
	}
	v := visitor{
		ctxt:  c,
		visit: f,
		node:  n,
		pkg:   pkg,
		all:   all,
		top:   top,
	}

	defer func() {
		switch x := recover(); x {
		case nil:
		case aborted:
			err = v.err
		default:
			panic(x)
		}
	}()

	for _, x := range n.Conjuncts {
		v.markExpr(x.Env, x.Elem())
	}

	return nil
}

var aborted = errors.New("aborted")

type visitor struct {
	ctxt  *adt.OpContext
	visit VisitFunc
	node  *adt.Vertex
	err   error
	pkg   *adt.ImportReference
	all   bool
	top   bool
}

// TODO: factor out the below logic as either a low-level dependency analyzer or
// some walk functionality.

// markExpr visits all nodes in an expression to mark dependencies.
func (c *visitor) markExpr(env *adt.Environment, expr adt.Elem) {
	switch x := expr.(type) {
	case nil:
	case adt.Resolver:
		c.markResolver(env, x)

	case *adt.BinaryExpr:
		c.markExpr(env, x.X)
		c.markExpr(env, x.Y)

	case *adt.UnaryExpr:
		c.markExpr(env, x.X)

	case *adt.Interpolation:
		for i := 1; i < len(x.Parts); i += 2 {
			c.markExpr(env, x.Parts[i])
		}

	case *adt.BoundExpr:
		c.markExpr(env, x.Expr)

	case *adt.CallExpr:
		c.markExpr(env, x.Fun)
		saved := c.all
		c.all = true
		for _, a := range x.Args {
			c.markExpr(env, a)
		}
		c.all = saved

	case *adt.DisjunctionExpr:
		for _, d := range x.Values {
			c.markExpr(env, d.Val)
		}

	case *adt.SliceExpr:
		c.markExpr(env, x.X)
		c.markExpr(env, x.Lo)
		c.markExpr(env, x.Hi)
		c.markExpr(env, x.Stride)

	case *adt.ListLit:
		env := &adt.Environment{Up: env, Vertex: empty}
		for _, e := range x.Elems {
			switch x := e.(type) {
			case *adt.Comprehension:
				c.markComprehension(env, x)

			case adt.Expr:
				c.markSubExpr(env, x)

			case *adt.Ellipsis:
				if x.Value != nil {
					c.markSubExpr(env, x.Value)
				}
			}
		}

	case *adt.StructLit:
		env := &adt.Environment{Up: env, Vertex: empty}
		for _, e := range x.Decls {
			c.markDecl(env, e)
		}

	case *adt.Comprehension:
		c.markComprehension(env, x)
	}
}

// markResolve resolves dependencies.
func (c *visitor) markResolver(env *adt.Environment, r adt.Resolver) {
	// Note: it is okay to pass an empty CloseInfo{} here as we assume that
	// all nodes are finalized already and we need neither closedness nor cycle
	// checks.
	ref, _ := c.ctxt.Resolve(adt.MakeConjunct(env, r, adt.CloseInfo{}), r)

	// TODO: consider the case where an inlined composite literal does not
	// resolve, but has references. For instance, {a: k, ref}.b would result
	// in a failure during evaluation if b is not defined within ref. However,
	// ref might still specialize to allow b.

	if ref != nil {
		if ref.Rooted() {
			c.reportDependency(ref, r)
		} else { // Lets and dynamically created structs.
			// Always process all references for non-rooted values, as
			// these references will otherwise not be visited as part of a
			// normal traversal.

			// It is okay to use the given env for lets, as lets that may vary
			// per Environment already have a separate arc associated with them
			// (see Vertex.MultiLet).

			saved := *c
			c.all = true
			c.top = false
			c.traverseNotRooted(env, r.(adt.Expr))
			*c = saved
		}

		return
	}

	// It is possible that a reference cannot be resolved because it is
	// incomplete. In this case, we should check whether subexpressions of the
	// reference can be resolved to mark those dependencies. For instance,
	// prefix paths of selectors and the value or index of an index expression
	// may independently resolve to a valid dependency.

	switch x := r.(type) {
	case *adt.NodeLink:
		panic("unreachable")

	case *adt.IndexExpr:
		c.markExpr(env, x.X)
		c.markExpr(env, x.Index)

	case *adt.SelectorExpr:
		c.markExpr(env, x.X)
	}
}

// reportDependency reports a dependency to the user of this package.
// v must be the value that is obtained after resolving r.
func (c *visitor) reportDependency(v *adt.Vertex, r adt.Resolver) {
	if v == c.node || v == empty {
		return
	}

	pkg := importRef(r.(adt.Expr)) // All resolvers are expressions.
	if pkg == nil {
		pkg = c.pkg
	}

	d := Dependency{
		Node:      v,
		Reference: r,
		pkg:       pkg,
		top:       c.top,
	}
	if err := c.visit(d); err != nil {
		c.err = err
		panic(aborted)
	}
}

// TODO(perf): make this available as a property of vertices to avoid doing
// work repeatedly.
func hasLetParent(v *adt.Vertex) bool {
	for ; v != nil; v = v.Parent {
		if v.Label.IsLet() {
			return true
		}
	}
	return false
}

// traverseNotRooted traverses an expression for which there is no path from
// the root of a tree. Such expressions are typically found within a let or
// when selecting into a composit literal. In such cases, it is important to
// find dependencies that will otherwise not be visited. The algorithm passes
// corrects the found references by further unwinding the path of selectors
// that led to the non-rooted value.
func (c *visitor) traverseNotRooted(env *adt.Environment, ref adt.Expr, path ...adt.Resolver) *adt.Vertex {
	var v *adt.Vertex
	switch x := ref.(type) {
	case nil:
		return nil
	case *adt.SelectorExpr:
		v = c.traverseNotRooted(env, x.X, append(path, x)...)
		if v == nil {
			return nil
		}
		v = v.Lookup(x.Sel)
		return c.traverseNotRooted(env, v, path...)

	case *adt.IndexExpr:
		v = c.traverseNotRooted(env, x.X, append(path, x)...)
		if v == nil {
			return nil
		}
		i, _ := c.ctxt.Evaluate(env, x.Index)
		i = adt.Unwrap(i)
		f := adt.LabelFromValue(c.ctxt, x.Index, i)
		v = v.Lookup(f)
		return c.traverseNotRooted(env, v, path...)

	case adt.Resolver:
		v, _ = c.ctxt.Resolve(adt.MakeConjunct(env, ref, adt.CloseInfo{}), x)
		if v == nil {
			return nil
		}

		if !v.Rooted() {
			if len(path) == 0 {
				for _, x := range v.Conjuncts {
					c.markExpr(x.Env, x.Expr())
				}
			}
			if hasLetParent(v) {
				return v
			}
		}

		if len(path) == 0 {
			c.reportDependency(v, ref.(adt.Resolver))
		} else {
			c.markExprPath(env, v, path...)
		}
		return v

	default:
		value, _ := c.ctxt.Evaluate(env, ref)
		v, _ = value.(*adt.Vertex)
		if v == nil {
			return nil
		}
		// TODO(perf): one level of  evaluation would suffice.
		v.Finalize(c.ctxt)
		for _, x := range v.Conjuncts {
			expr := x.Expr()
			if len(path) == 0 {
				c.markExpr(x.Env, expr)
				continue
			}
			c.markInnerResolvers(x.Env, expr, path...)
		}
		return v
	}
}

// markInnerResolvers marks all references that are inside an non-rooted struct
// as dependencies. It extends the resolvers with the remaining path if
// necessary.
func (c *visitor) markInnerResolvers(env *adt.Environment, expr adt.Expr, path ...adt.Resolver) {
	switch y := expr.(type) {
	case adt.Resolver:
		c.traverseNotRooted(env, expr, path...)

	case *adt.BinaryExpr:
		c.markInnerResolvers(env, y.X, path...)
		c.markInnerResolvers(env, y.Y, path...)
	}
}

func (c *visitor) feature(env *adt.Environment, r adt.Resolver) adt.Feature {
	switch x := r.(type) {
	case *adt.SelectorExpr:
		return x.Sel
	case *adt.IndexExpr:
		v, _ := c.ctxt.Evaluate(env, x.Index)
		v = adt.Unwrap(v)
		return adt.LabelFromValue(c.ctxt, x.Index, v)
	}
	panic("unreachable")
}

func (c *visitor) markExprPath(env *adt.Environment, v *adt.Vertex, path ...adt.Resolver) {
	var r adt.Resolver
	for _, next := range path {
		r = next
		w := v.Lookup(c.feature(env, next))
		if w == nil {
			break
		}
		v = w
	}
	c.reportDependency(v, r)
}

func (c *visitor) markSubExpr(env *adt.Environment, x adt.Expr) {
	if c.all {
		saved := c.top
		c.top = false
		c.markExpr(env, x)
		c.top = saved
	}
}

func (c *visitor) markDecl(env *adt.Environment, d adt.Decl) {
	switch x := d.(type) {
	case *adt.Field:
		c.markSubExpr(env, x.Value)

	case *adt.BulkOptionalField:
		c.markExpr(env, x.Filter)
		// when dynamic, only continue if there is evidence of
		// the field in the parallel actual evaluation.
		c.markSubExpr(env, x.Value)

	case *adt.DynamicField:
		c.markExpr(env, x.Key)
		// when dynamic, only continue if there is evidence of
		// a matching field in the parallel actual evaluation.
		c.markSubExpr(env, x.Value)

	case *adt.Comprehension:
		c.markComprehension(env, x)

	case adt.Expr:
		c.markExpr(env, x)

	case *adt.Ellipsis:
		if x.Value != nil {
			c.markSubExpr(env, x.Value)
		}
	}
}

func (c *visitor) markComprehension(env *adt.Environment, y *adt.Comprehension) {
	env = c.markClauses(env, y.Clauses)

	// Use "live" environments if we have them. This is important if
	// dependencies are computed on a partially evaluated value where a pushed
	// down comprehension is defined outside the root of the dependency
	// analysis. For instance, when analyzing dependencies at path a.b in:
	//
	//  a: {
	//      for value in { test: 1 } {
	//          b: bar: value
	//      }
	//  }
	//
	if envs := y.Envs(); len(envs) > 0 {
		// We use the Environment to get access to the parent chain. It
		// suffices to take any Environment (in this case the first), as all
		// will have the same parent chain.
		env = envs[0]
	}
	for i := y.Nest(); i > 0; i-- {
		env = &adt.Environment{Up: env, Vertex: empty}
	}
	c.markExpr(env, adt.ToExpr(y.Value))
}

func (c *visitor) markClauses(env *adt.Environment, a []adt.Yielder) *adt.Environment {
	for _, y := range a {
		switch x := y.(type) {
		case *adt.ForClause:
			c.markExpr(env, x.Src)
			env = &adt.Environment{Up: env, Vertex: empty}
			// In dynamic mode, iterate over all actual value and
			// evaluate.

		case *adt.LetClause:
			c.markExpr(env, x.Expr)
			env = &adt.Environment{Up: env, Vertex: empty}

		case *adt.IfClause:
			c.markExpr(env, x.Condition)
			// In dynamic mode, only continue if condition is true.

		case *adt.ValueClause:
			env = &adt.Environment{Up: env, Vertex: empty}
		}
	}
	return env
}
