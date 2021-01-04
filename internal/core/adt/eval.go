// Copyright 2021 CUE Authors
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

// Package eval contains the high level CUE evaluation strategy.
//
// CUE allows for a significant amount of freedom in order of evaluation due to
// the commutativity of the unification operation. This package implements one
// of the possible strategies.
package adt

// TODO:
//   - result should be nodeContext: this allows optionals info to be extracted
//     and computed.
//

import (
	"fmt"
	"html/template"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// TODO TODO TODO TODO TODO TODO  TODO TODO TODO  TODO TODO TODO  TODO TODO TODO
//
// - Reuse work from previous cycles. For instance, if we can guarantee that a
//   value is always correct for partial results, we can just process the arcs
//   going from Partial to Finalized, without having to reevaluate the value.
//
// - Test closedness far more thoroughly.
//

func NewUnifier(r Runtime) *Unifier {
	return &Unifier{r: r, index: r}
}

type Stats struct {
	DisjunctCount int
	UnifyCount    int

	Freed  int
	Reused int
	Allocs int
}

var stats = template.Must(template.New("stats").Parse(`{{"" -}}
Freed:  {{.Freed}}
Reused: {{.Reused}}
Allocs: {{.Allocs}}

Unifications: {{.UnifyCount}}
Disjuncts:    {{.DisjunctCount}}`))

func (s *Stats) String() string {
	buf := &strings.Builder{}
	_ = stats.Execute(buf, s)
	return buf.String()
}

func (e *Unifier) Stats() *Stats {
	return &e.stats
}

// TODO: Note: NewContext takes essentially a cue.Value. By making this
// type more central, we can perhaps avoid context creation.

// func NewContext(r Runtime, v *Vertex) *OpContext {
// 	e := NewUnifier(r)
// 	return e.NewContext(v)
// }

var structSentinel = &StructMarker{}

var incompleteSentinel = &Bottom{
	Code: IncompleteError,
	Err:  errors.Newf(token.NoPos, "incomplete"),
}

// A Unifier implements a strategy for CUE's unification operation. It must
// handle the following aspects of CUE evaluation:
//
//    - Structural and reference cycles
//    - Non-monotic validation
//    - Fixed-point computation of comprehension
//
type Unifier struct {
	r     Runtime
	index StringIndexer

	stats Stats

	freeListNode   *nodeContext
	freeListShared *nodeShared
}

func (e *Unifier) Eval(v *Vertex) errors.Error {
	if v.BaseValue == nil {
		ctx := NewContext(e.r, v)
		e.Unify(ctx, v, Partial)
	}

	// extract error if needed.
	return nil
}

// Evaluate returns the evaluated value associated with v. It may return a
// partial result. That is, if v was not yet unified, it may return a
// concrete value that must be the result assuming the configuration has no
// errors.
//
// This semantics allows CUE to break reference cycles in a straightforward
// manner.
//
// Vertex v must still be evaluated at some point to catch the underlying
// error.
//
// TODO: return *Vertex
func (e *Unifier) Evaluate(c *OpContext, v *Vertex) Value {
	var result Vertex

	if b, _ := v.BaseValue.(*Bottom); b != nil {
		return b
	}

	if v.BaseValue == nil {
		save := *v
		// Use node itself to allow for cycle detection.
		s := e.evalVertex(c, v, Partial)
		defer e.freeSharedNode(s)

		result = *v

		if s.result_.BaseValue != nil { // There is a complete result.
			*v = s.result_
			// result = *v
		} else if b, ok := v.BaseValue.(*Bottom); ok {
			*v = save
			return b
		} else {
			*v = save
		}

		switch {
		case !s.touched:

		case len(s.disjuncts) == 1 || s.numDefaults == 1:
			// TODO: this seems unnecessary as long as we have a better way
			// to handle incomplete, and perhaps referenced. nodes.
			if c.IsTentative() && isStruct(v) {
				// TODO(perf): do something more efficient perhaps? This discards
				// the computed arcs so far. Instead, we could have a separate
				// marker to accumulate results. As this only happens within
				// comprehensions, the effect is likely minimal, though.
				arcs := v.Arcs
				w := &Vertex{
					Parent:    v.Parent,
					BaseValue: &StructMarker{},
					Arcs:      arcs,
					Conjuncts: v.Conjuncts,
				}
				w.UpdateStatus(v.Status())
				*v = save
				return w
			}

		default:
			d := s.createDisjunct()
			last := len(d.Values) - 1
			clone := *(d.Values[last])
			d.Values[last] = &clone

			v.UpdateStatus(Finalized)
			v.BaseValue = d
			v.Arcs = nil
			v.Structs = nil // TODO: maybe not do this.
			// The conjuncts will have too much information. Better have no
			// information than incorrect information.
			for _, d := range d.Values {
				d.Conjuncts = nil
				for _, a := range d.Arcs {
					for _, x := range a.Conjuncts {
						// All the environments for embedded structs need to be
						// dereferenced.
						for env := x.Env; env != nil && env.Vertex == v; env = env.Up {
							env.Vertex = d
						}
					}
				}
			}

			return d
		}

		err, _ := result.BaseValue.(*Bottom)
		// BEFORE RESTORING, copy the value to return one
		// with the temporary arcs.
		if !s.done() && (err == nil || err.IsIncomplete()) {
			// Clear values afterwards
			*v = save
		}
		if s.hasResult() {
			if b, _ := v.BaseValue.(*Bottom); b != nil {
				*v = save
				return b
			}
			// TODO: Only use result when not a cycle.
			w := s.result()
			v = &w
		}
		// TODO: Store if concrete and fully resolved.
	}

	// TODO: Use this and ensure that each use of Evaluate handles
	// struct numbers correctly. E.g. by using a function that
	// gets the concrete value.
	//
	if v.BaseValue == nil {
		if result.BaseValue == nil {
			panic("nil basevalue")
		}
		return &result
	}
	return v
}

// Unify fully unifies all values of a Vertex to completion and stores
// the result in the Vertex. If Unify was called on v before it returns
// the cached results.
func (e *Unifier) Unify(c *OpContext, v *Vertex, state VertexStatus) {
	// defer c.PopVertex(c.PushVertex(v))

	if state <= v.Status() {
		return
	}

	if x := v.BaseValue; x != nil {
		// if state == Partial || x == cycle {
		// 	return
		// }
		return
	}

	n := e.evalVertex(c, v, state)
	defer e.freeSharedNode(n)

	switch {
	case len(n.disjuncts) == 1:
		*v = n.disjuncts[0].result

	case len(n.disjuncts) > 0:
		d := n.createDisjunct()
		v.BaseValue = d
		// The conjuncts will have too much information. Better have no
		// information than incorrect information.
		for _, d := range d.Values {
			// We clear the conjuncts for now. As these disjuncts are for API
			// use only, we will fill them out when necessary (using Defaults).
			d.Conjuncts = nil

			// TODO: use a more principled form of dereferencing. For instance,
			// disjuncts could already be assumed to be the given Vertex, and
			// the the main vertex could be dereferenced during evaluation.
			for _, a := range d.Arcs {
				for _, x := range a.Conjuncts {
					// All the environments for embedded structs need to be
					// dereferenced.
					for env := x.Env; env != nil && env.Vertex == v; env = env.Up {
						env.Vertex = d
					}
				}
			}
		}
		v.Arcs = nil
		// v.Structs = nil // TODO: should we keep or discard the Structs?
		// TODO: how to represent closedness information? Do we need it?

	default:
		if r := n.result(); r.BaseValue != nil {
			*v = r
		}
	}

	// Else set it to something.

	if v.BaseValue == nil {
		panic("error")
	}

	// Check whether result is done.
}

// evalVertex computes the vertex results. The state indicates the minimum
// status to which this vertex should be evaluated. It should be either
// Finalized or Partial.
func (e *Unifier) evalVertex(c *OpContext, v *Vertex, state VertexStatus) *nodeShared {
	shared := e.newSharedNode(c, v)

	if v.Label.IsDef() {
		v.Closed = true
	}

	ignore := false
	if v.Parent != nil {
		if v.Parent.Closed {
			v.Closed = true
		}
	}

	if !v.Label.IsInt() && v.Parent != nil && !ignore && state == Finalized {
		// Visit arcs recursively to validate and compute error.
		if _, err := verifyArc2(c, v.Label, v, v.Closed); err != nil {
			// Record error in child node to allow recording multiple
			// conflicts at the appropriate place, to allow valid fields to
			// be represented normally and, most importantly, to avoid
			// recursive processing of a disallowed field.
			v.SetValue(c, Finalized, err)
			return shared
		}
	}

	defer c.PopArc(c.PushArc(v))

	e.stats.UnifyCount++

	// Clear any remaining error.
	if err := c.Err(); err != nil {
		panic("uncaught error")
	}

	// Set the cache to a cycle error to ensure a cyclic reference will result
	// in an error if applicable. A cyclic error may be ignored for
	// non-expression references. The cycle error may also be removed as soon
	// as there is evidence what a correct value must be, but before all
	// validation has taken place.
	v.BaseValue = cycle

	v.UpdateStatus(Evaluating)

	// If the result is a struct, it needs to be closed if:
	//   1) this node introduces a definition
	//   2) this node is a child of a node that introduces a definition,
	//      recursively.
	//   3) this node embeds a closed struct.
	n := e.newNodeContext(shared)

	for _, x := range v.Conjuncts {
		// TODO: needed for reentrancy. Investigate usefulness for cycle
		// detection.
		n.addExprConjunct(x)
	}

	// Use maybeSetCache for cycle breaking
	for n.maybeSetCache(); n.expandOne(); n.maybeSetCache() {
	}
	if v.Status() > Evaluating && state <= Partial {
		// We have found a partial result. There may still be errors
		// down the line which may result from further evaluating this
		// field, but that will be caught when evaluating this field
		// for real.
		shared.setResult(v)
		e.freeNodeContext(n)
		return shared
	}

	if !n.done() && len(n.disjunctions) > 0 && isEvaluating(v) {
		// We disallow entering computations of disjunctions with
		// incomplete data.
		b := c.NewErrf("incomplete cause disjunction")
		b.Code = IncompleteError
		v.SetValue(n.ctx, Finalized, b)
		shared.setResult(v)
		e.freeNodeContext(n)
		return shared
	}

	n.expandDisjuncts(state, nil, maybeDefault, false)
	// TODO: reorganize nodeShared and nodeContext
	n.nodeShared.disjuncts = append(n.nodeShared.disjuncts, n.disjuncts...)
	if v.BaseValue == nil {
		v.BaseValue = n.getValidators()
	}
	// e.freeNodeContext(n) freed in freeSharedNode
	return shared
}

func isStruct(v *Vertex) bool {
	_, ok := v.BaseValue.(*StructMarker)
	return ok
}

func (n *nodeContext) postDisjunct(state VertexStatus) {
	ctx := n.ctx

	for {
		// Use maybeSetCache for cycle breaking
		for n.maybeSetCache(); n.expandOne(); n.maybeSetCache() {
		}

		if aList, id := n.addLists(ctx); aList != nil {
			n.updateNodeType(ListKind, aList, id)
		} else {
			break
		}
	}

	if n.aStruct != nil {
		n.updateNodeType(StructKind, n.aStruct, n.aStructID)
	}

	switch err := n.getErr(); {
	case err != nil:
		n.node.BaseValue = err
		n.errs = nil

	default:
		if isEvaluating(n.node) {
			if !n.done() { // && !ctx.IsTentative() {
				// collect incomplete errors.
				var err *Bottom // n.incomplete
				for _, d := range n.dynamicFields {
					err = CombineErrors(nil, err, d.err)
				}
				for _, c := range n.forClauses {
					err = CombineErrors(nil, err, c.err)
				}
				for _, c := range n.ifClauses {
					err = CombineErrors(nil, err, c.err)
				}
				for _, x := range n.exprs {
					err = CombineErrors(nil, err, x.err)
				}
				if err == nil {
					// safeguard.
					err = incompleteSentinel
				}
				n.node.BaseValue = err
			} else {
				n.node.BaseValue = nil
			}
		}

		// We are no longer evaluating.
		n.node.UpdateStatus(Partial)

		// Either set to Conjunction or error.
		// TODO: verify and simplify the below code to determine whether
		// something is a struct.
		markStruct := false
		if n.aStruct != nil {
			markStruct = true
		} else if len(n.node.Structs) > 0 {
			markStruct = n.kind&StructKind != 0 && !n.hasTop
		}
		v := n.node.Value()
		if n.node.BaseValue == nil && markStruct {
			n.node.BaseValue = &StructMarker{}
			v = n.node
		}
		if v != nil && IsConcrete(v) {
			// Also check when we already have errors as we may find more
			// serious errors and would like to know about all errors anyway.

			if n.lowerBound != nil {
				if b := ctx.Validate(n.lowerBound, v); b != nil {
					// TODO(errors): make Validate return boolean and generate
					// optimized conflict message. Also track and inject IDs
					// to determine origin location.s
					if e, _ := b.Err.(*ValueError); e != nil {
						e.AddPosition(n.lowerBound)
						e.AddPosition(v)
					}
					n.addBottom(b)
				}
			}
			if n.upperBound != nil {
				if b := ctx.Validate(n.upperBound, v); b != nil {
					// TODO(errors): make Validate return boolean and generate
					// optimized conflict message. Also track and inject IDs
					// to determine origin location.s
					if e, _ := b.Err.(*ValueError); e != nil {
						e.AddPosition(n.upperBound)
						e.AddPosition(v)
					}
					n.addBottom(b)
				}
			}
			for _, v := range n.checks {
				// TODO(errors): make Validate return bottom and generate
				// optimized conflict message. Also track and inject IDs
				// to determine origin location.s
				if b := ctx.Validate(v, n.node); b != nil {
					n.addBottom(b)
				}
			}

		} else if !ctx.IsTentative() {
			n.node.BaseValue = n.getValidators()
		}
		// else if v == nil {
		// 	n.node.Value = incompleteSentinel
		// }

		if v == nil {
			break
		}

		switch {
		case v.Kind() == ListKind:
			for _, a := range n.node.Arcs {
				if a.Label.Typ() == StringLabel {
					n.addErr(ctx.Newf("list may not have regular fields"))
					// TODO(errors): add positions for list and arc definitions.

				}
			}

			// case !isStruct(n.node) && v.Kind() != BottomKind:
			// 	for _, a := range n.node.Arcs {
			// 		if a.Label.IsRegular() {
			// 			n.addErr(errors.Newf(token.NoPos,
			// 				// TODO(errors): add positions of non-struct values and arcs.
			// 				"cannot combine scalar values with arcs"))
			// 		}
			// 	}
		}
	}

	if err := n.getErr(); err != nil {
		if b, _ := n.node.BaseValue.(*Bottom); b != nil {
			err = CombineErrors(nil, b, err)
		}
		n.node.BaseValue = err
		// TODO: add return: if evaluation of arcs is important it can be done
		// later. Logically we're done.
	}

	n.completeArcs(state)
}

func (n *nodeContext) completeArcs(state VertexStatus) {
	ctx := n.ctx

	if cyclic := n.hasCycle && !n.hasNonCycle; cyclic {
		n.node.BaseValue = CombineErrors(nil,
			n.node.Value(),
			&Bottom{
				Code:  StructuralCycleError,
				Err:   ctx.Newf("structural cycle"),
				Value: n.node.Value(),
				// TODO: probably, this should have the referenced arc.
			})
	} else {
		// Visit arcs recursively to validate and compute error.
		for _, a := range n.node.Arcs {
			// Call UpdateStatus here to be absolutely sure the status is set
			// correctly and that we are not regressing.
			if state == Finalized {
				n.node.UpdateStatus(EvaluatingArcs)
			}
			n.eval.Unify(ctx, a, state)
			if err, _ := a.BaseValue.(*Bottom); err != nil {
				n.node.AddChildError(err)
			}
		}
	}

	n.node.UpdateStatus(Finalized)
}

// TODO: this is now a sentinel. Use a user-facing error that traces where
// the cycle originates.
var cycle = &Bottom{
	Err:  errors.Newf(token.NoPos, "cycle error"),
	Code: CycleError,
}

func isEvaluating(v *Vertex) bool {
	isCycle := v.Status() == Evaluating
	if isCycle != (v.BaseValue == cycle) {
		panic(fmt.Sprintf("cycle data of sync %d vs %#v", v.Status(), v.BaseValue))
	}
	return isCycle
}

// TODO(perf): merge this type with nodeContext once we're certain we can
// remove the distinction (after other optimizations).
type nodeShared struct {
	nextFree *nodeShared

	eval *Unifier
	ctx  *OpContext
	node *Vertex

	// Disjunction handling
	touched      bool
	disjuncts    []*nodeContext
	disjunctErrs []*Bottom
	numDefaults  int

	result_ Vertex
	isDone  bool
}

func (e *Unifier) newSharedNode(ctx *OpContext, node *Vertex) *nodeShared {
	if n := e.freeListShared; n != nil {
		e.stats.Reused++
		e.freeListShared = n.nextFree

		*n = nodeShared{
			eval: e,
			ctx:  ctx,
			node: node,

			disjuncts:    n.disjuncts[:0],
			disjunctErrs: n.disjunctErrs[:0],
		}

		return n
	}
	e.stats.Allocs++

	return &nodeShared{
		ctx:  ctx,
		eval: e,
		node: node,
	}
}

func (e *Unifier) freeSharedNode(n *nodeShared) {
	e.stats.Freed++
	n.nextFree = e.freeListShared
	e.freeListShared = n

	for _, d := range n.disjuncts {
		e.freeNodeContext(d)
	}
}

func (n *nodeShared) createDisjunct() *Disjunction {
	a := make([]*Vertex, len(n.disjuncts))
	p := 0
	for i, x := range n.disjuncts {
		v := x.result
		if x.defaultMode == isDefault {
			a[i] = a[p]
			a[p] = &v
			p++
		} else {
			a[i] = &v
		}
	}
	return &Disjunction{
		Values:      a,
		NumDefaults: p,
	}
}

func (n *nodeShared) result() Vertex {
	return n.result_
}

func (n *nodeShared) setResult(v *Vertex) {
	n.result_ = *v
}

func (n *nodeShared) hasResult() bool {
	return len(n.disjuncts) > 0
}

func (n *nodeShared) done() bool {
	return n.isDone
}

type arcKey struct {
	arc *Vertex
	id  CloseInfo
}

// A nodeContext is used to collate all conjuncts of a value to facilitate
// unification. Conceptually order of unification does not matter. However,
// order has relevance when performing checks of non-monotic properities. Such
// checks should only be performed once the full value is known.
type nodeContext struct {
	nextFree *nodeContext

	*nodeShared

	// TODO: (this is CL is first step)
	// filter *Vertex a subset of composite with concrete fields for
	// bloom-like filtering of disjuncts. We should first verify, however,
	// whether some breath-first search gives sufficient performance, as this
	// should already ensure a quick-fail for struct disjunctions with
	// discriminators.

	arcMap []arcKey

	// snapshot holds the last value of the vertex before calling postDisjunct.
	snapshot Vertex

	// Result holds the last evaluated value of the vertex after calling
	// postDisjunct.
	result Vertex

	// Current value (may be under construction)
	scalar   Value // TODO: use Value in node.
	scalarID CloseInfo

	// Concrete conjuncts
	kind       Kind
	kindExpr   Expr        // expr that adjust last value (for error reporting)
	kindID     CloseInfo   // for error tracing
	lowerBound *BoundValue // > or >=
	upperBound *BoundValue // < or <=
	checks     []Validator // BuiltinValidator, other bound values.
	errs       *Bottom

	// Struct information
	dynamicFields []envDynamic
	ifClauses     []envYield
	forClauses    []envYield
	aStruct       Expr
	aStructID     CloseInfo

	// Expression conjuncts
	lists  []envList
	vLists []*Vertex
	exprs  []envExpr

	hasTop      bool
	hasCycle    bool // has conjunct with structural cycle
	hasNonCycle bool // has conjunct without structural cycle

	// Disjunction handling
	disjunctions []envDisjunct
	defaultMode  defaultMode
	disjuncts    []*nodeContext
	buffer       []*nodeContext
}

func (n *nodeContext) clone() *nodeContext {
	d := n.eval.newNodeContext(n.nodeShared)

	d.scalar = n.scalar
	d.scalarID = n.scalarID
	d.kind = n.kind
	d.kindExpr = n.kindExpr
	d.kindID = n.kindID
	d.aStruct = n.aStruct
	d.aStructID = n.aStructID
	d.hasTop = n.hasTop

	d.lowerBound = n.lowerBound
	d.upperBound = n.upperBound
	d.errs = n.errs
	d.hasTop = n.hasTop
	d.hasCycle = n.hasCycle
	d.hasNonCycle = n.hasNonCycle

	// d.arcMap = append(d.arcMap, n.arcMap...) // XXX add?
	d.checks = append(d.checks, n.checks...)
	d.dynamicFields = append(d.dynamicFields, n.dynamicFields...)
	d.ifClauses = append(d.ifClauses, n.ifClauses...)
	d.forClauses = append(d.forClauses, n.forClauses...)
	d.lists = append(d.lists, n.lists...)
	d.vLists = append(d.vLists, n.vLists...)
	d.exprs = append(d.exprs, n.exprs...)
	// No need to clone d.disjunctions

	return d
}

func (e *Unifier) newNodeContext(shared *nodeShared) *nodeContext {
	if n := e.freeListNode; n != nil {
		e.stats.Reused++
		e.freeListNode = n.nextFree

		*n = nodeContext{
			kind:          TopKind,
			nodeShared:    shared,
			arcMap:        n.arcMap[:0],
			checks:        n.checks[:0],
			dynamicFields: n.dynamicFields[:0],
			ifClauses:     n.ifClauses[:0],
			forClauses:    n.forClauses[:0],
			lists:         n.lists[:0],
			vLists:        n.vLists[:0],
			exprs:         n.exprs[:0],
			disjunctions:  n.disjunctions[:0],
			disjuncts:     n.disjuncts[:0],
			buffer:        n.buffer[:0],
		}

		return n
	}
	e.stats.Allocs++

	return &nodeContext{
		kind:       TopKind,
		nodeShared: shared,
	}
}

func (e *Unifier) freeNodeContext(n *nodeContext) {
	e.stats.Freed++
	n.nextFree = e.freeListNode
	e.freeListNode = n
}

// TODO(perf): return a dedicated ConflictError that can track original
// positions on demand.
func (n *nodeContext) addConflict(
	v1, v2 Node,
	k1, k2 Kind,
	id1, id2 CloseInfo) {

	ctx := n.ctx

	var err *ValueError
	if k1 == k2 {
		err = ctx.NewPosf(token.NoPos,
			"conflicting values %s and %s", ctx.Str(v1), ctx.Str(v2))
	} else {
		err = ctx.NewPosf(token.NoPos,
			"conflicting values %s and %s (mismatched types %s and %s)",
			ctx.Str(v1), ctx.Str(v2), k1, k2)
	}

	err.AddPosition(v1)
	err.AddPosition(v2)
	err.AddClosedPositions(id1)
	err.AddClosedPositions(id2)

	n.addErr(err)
}

func (n *nodeContext) updateNodeType(k Kind, v Expr, id CloseInfo) bool {
	ctx := n.ctx
	kind := n.kind & k

	switch {
	case n.kind == BottomKind,
		k == BottomKind:
		return false

	case kind == BottomKind:
		if n.kindExpr != nil {
			n.addConflict(n.kindExpr, v, n.kind, k, n.kindID, id)
		} else {
			n.addErr(ctx.Newf(
				"conflicting value %s (mismatched types %s and %s)",
				ctx.Str(v), n.kind, k))
		}
	}

	if n.kind != kind || n.kindExpr == nil {
		n.kindExpr = v
	}
	n.kind = kind
	return kind != BottomKind
}

func (n *nodeContext) done() bool {
	return len(n.dynamicFields) == 0 &&
		len(n.ifClauses) == 0 &&
		len(n.forClauses) == 0 &&
		len(n.exprs) == 0
}

// hasErr is used to determine if an evaluation path, for instance a single
// path after expanding all disjunctions, has an error.
func (n *nodeContext) hasErr() bool {
	if n.node.ChildErrors != nil {
		return true
	}
	if n.node.Status() > Evaluating && n.node.IsErr() {
		return true
	}
	return n.ctx.HasErr() || n.errs != nil
}

func (n *nodeContext) getErr() *Bottom {
	n.errs = CombineErrors(nil, n.errs, n.ctx.Err())
	return n.errs
}

// getValidators sets the vertex' Value in case there was no concrete value.
func (n *nodeContext) getValidators() BaseValue {
	ctx := n.ctx

	a := []Value{}
	// if n.node.Value != nil {
	// 	a = append(a, n.node.Value)
	// }
	kind := TopKind
	if n.lowerBound != nil {
		a = append(a, n.lowerBound)
		kind &= n.lowerBound.Kind()
	}
	if n.upperBound != nil {
		a = append(a, n.upperBound)
		kind &= n.upperBound.Kind()
	}
	for _, c := range n.checks {
		// Drop !=x if x is out of bounds with another bound.
		if b, _ := c.(*BoundValue); b != nil && b.Op == NotEqualOp {
			if n.upperBound != nil &&
				SimplifyBounds(ctx, n.kind, n.upperBound, b) != nil {
				continue
			}
			if n.lowerBound != nil &&
				SimplifyBounds(ctx, n.kind, n.lowerBound, b) != nil {
				continue
			}
		}
		a = append(a, c)
		kind &= c.Kind()
	}
	if kind&^n.kind != 0 {
		a = append(a, &BasicType{K: n.kind})
	}

	var v BaseValue
	switch len(a) {
	case 0:
		// Src is the combined input.
		v = &BasicType{K: n.kind}

		if len(n.node.Structs) > 0 {
			v = structSentinel

		}

	case 1:
		v = a[0].(Value) // remove cast

	default:
		v = &Conjunction{Values: a}
	}

	return v
}

func (n *nodeContext) maybeSetCache() {
	if n.node.Status() > Evaluating { // n.node.BaseValue != nil
		return
	}
	if n.scalar != nil {
		n.node.SetValue(n.ctx, Partial, n.scalar)
	}
	if n.errs != nil {
		n.node.SetValue(n.ctx, Partial, n.errs)
	}
}

type envExpr struct {
	c   Conjunct
	err *Bottom
}

type envDynamic struct {
	env   *Environment
	field *DynamicField
	id    CloseInfo
	err   *Bottom
}

type envYield struct {
	env   *Environment
	yield Yielder
	id    CloseInfo
	err   *Bottom
}

type envList struct {
	env     *Environment
	list    *ListLit
	n       int64 // recorded length after evaluator
	elipsis *Ellipsis
	id      CloseInfo
}

func (n *nodeContext) addBottom(b *Bottom) {
	n.errs = CombineErrors(nil, n.errs, b)
	// TODO(errors): consider doing this
	// n.kindExpr = n.errs
	// n.kind = 0
}

func (n *nodeContext) addErr(err errors.Error) {
	if err != nil {
		n.addBottom(&Bottom{Err: err})
	}
}

// addExprConjuncts will attempt to evaluate an Expr and insert the value
// into the nodeContext if successful or queue it for later evaluation if it is
// incomplete or is not value.
func (n *nodeContext) addExprConjunct(v Conjunct) {
	env := v.Env
	id := v.CloseInfo

	switch x := v.Expr().(type) {
	case *Vertex:
		if x.IsData() {
			n.addValueConjunct(env, x, id)
		} else {
			n.addVertexConjuncts(env, id, x, x)
		}

	case Value:
		n.addValueConjunct(env, x, id)

	case *BinaryExpr:
		if x.Op == AndOp {
			n.addExprConjunct(MakeConjunct(env, x.X, id))
			n.addExprConjunct(MakeConjunct(env, x.Y, id))
		} else {
			n.evalExpr(v)
		}

	case *StructLit:
		n.addStruct(env, x, id)

	case *ListLit:
		n.lists = append(n.lists, envList{env: env, list: x, id: id})

	case *DisjunctionExpr:
		if n.disjunctions != nil {
			_ = n.disjunctions
		}
		n.addDisjunction(env, x, id)

	default:
		// Must be Resolver or Evaluator.
		n.evalExpr(v)
	}
}

// evalExpr is only called by addExprConjunct. If an error occurs, it records
// the error in n and returns nil.
func (n *nodeContext) evalExpr(v Conjunct) {
	// Require an Environment.
	ctx := n.ctx

	closeID := v.CloseInfo

	// TODO: see if we can do without these counters.
	for _, d := range v.Env.Deref {
		d.EvalCount++
	}
	for _, d := range v.Env.Cycles {
		d.SelfCount++
	}
	defer func() {
		for _, d := range v.Env.Deref {
			d.EvalCount--
		}
		for _, d := range v.Env.Cycles {
			d.SelfCount++
		}
	}()

	switch x := v.Expr().(type) {
	case Resolver:
		arc, err := ctx.Resolve(v.Env, x)
		if err != nil && !err.IsIncomplete() {
			n.addBottom(err)
			break
		}
		if arc == nil {
			n.exprs = append(n.exprs, envExpr{v, err})
			break
		}

		n.addVertexConjuncts(v.Env, v.CloseInfo, v.Expr(), arc)

	case Evaluator:
		// Interpolation, UnaryExpr, BinaryExpr, CallExpr
		// Could be unify?
		val, complete := ctx.Evaluate(v.Env, v.Expr())
		if !complete {
			b, _ := val.(*Bottom)
			n.exprs = append(n.exprs, envExpr{v, b})
			break
		}

		if v, ok := val.(*Vertex); ok {
			// Handle generated disjunctions (as in the 'or' builtin).
			// These come as a Vertex, but should not be added as a value.
			b, ok := v.BaseValue.(*Bottom)
			if ok && b.IsIncomplete() && len(v.Conjuncts) > 0 {
				for _, c := range v.Conjuncts {
					c.CloseInfo = closeID
					n.addExprConjunct(c)
				}
				break
			}
		}

		// TODO: also to through normal Vertex handling here. At the moment
		// addValueConjunct handles StructMarker.NeedsClose, as this is always
		// only needed when evaluation an Evaluator, and not a Resolver.
		// The two code paths should ideally be merged once this separate
		// mechanism is eliminated.
		//
		// if arc, ok := val.(*Vertex); ok && !arc.IsData() {
		// 	n.addVertexConjuncts(v.Env, closeID, v.Expr(), arc)
		// 	break
		// }

		// TODO: insert in vertex as well
		n.addValueConjunct(v.Env, val, closeID)

	default:
		panic(fmt.Sprintf("unknown expression of type %T", x))
	}
}

func (n *nodeContext) addVertexConjuncts(env *Environment, closeInfo CloseInfo, x Expr, arc *Vertex) {

	// We need to ensure that each arc is only unified once (or at least) a
	// bounded time, witch each conjunct. Comprehensions, for instance, may
	// distribute a value across many values that get unified back into the
	// same value. If such a value is a disjunction, than a disjunction of N
	// disjuncts will result in a factor N more unifications for each
	// occurrence of such value, resulting in exponential running time. This
	// is especially common values that are used as a type.
	//
	// However, unification is idempotent, so each such conjunct only needs
	// to be unified once. This cache checks for this and prevents an
	// exponential blowup in such case.
	//
	// TODO(perf): this cache ensures the conjuncts of an arc at most once
	// per ID. However, we really need to add the conjuncts of an arc only
	// once total, and then add the close information once per close ID
	// (pointer can probably be shared). Aside from being more performant,
	// this is probably the best way to guarantee that conjunctions are
	// linear in this case.
	key := arcKey{arc, closeInfo}
	for _, k := range n.arcMap {
		if key == k {
			return
		}
	}
	n.arcMap = append(n.arcMap, key)

	// Pass detection of structural cycles from parent to children.
	cyclic := false
	if env != nil {
		// If a reference is in a tainted set, so is the value it refers to.
		cyclic = env.Cyclic
	}

	status := arc.Status()

	switch status {
	case Evaluating:
		// Reference cycle detected. We have reached a fixed point and
		// adding conjuncts at this point will not change the value. Also,
		// continuing to pursue this value will result in an infinite loop.

		// TODO: add a mechanism so that the computation will only have to
		// be done once?

		if arc == n.node {
			// TODO: we could use node sharing here. This may avoid an
			// exponential blowup during evaluation, like is possible with
			// YAML.
			return
		}

	case EvaluatingArcs:
		// Structural cycle detected. Continue evaluation as usual, but
		// keep track of whether any other conjuncts without structural
		// cycles are added. If not, evaluation of child arcs will end
		// with this node.

		// For the purpose of determining whether at least one non-cyclic
		// conjuncts exists, we consider all conjuncts of a cyclic conjuncts
		// also cyclic.

		cyclic = true
		n.hasCycle = true

		// As the EvaluatingArcs mechanism bypasses the self-reference
		// mechanism, we need to separately keep track of it here.
		// If this (originally) is a self-reference node, adding them
		// will result in recursively adding the same reference. For this
		// we also mark the node as evaluating.
		if arc.SelfCount > 0 {
			return
		}

		// This count is added for values that are directly added below.
		// The count is handled separately for delayed values.
		arc.SelfCount++
		defer func() { arc.SelfCount-- }()
	}

	closeInfo = closeInfo.SpawnRef(arc, IsDef(x), x)

	// TODO: uncommenting the following almost works, but causes some
	// faulty results in complex cycle handling between disjunctions.
	// The reason is that disjunctions must be eliminated if checks in
	// values on which they depend fail.
	ctx := n.ctx
	ctx.Unify(ctx, arc, Finalized)

	for _, c := range arc.Conjuncts {
		var a []*Vertex
		if env != nil {
			a = env.Deref
		}
		c = updateCyclic(c, cyclic, arc, a)

		// Note that we are resetting the tree here. We hereby assume that
		// closedness conflicts resulting from unifying the referenced arc were
		// already caught there and that we can ignore further errors here.
		c.CloseInfo = closeInfo
		n.addExprConjunct(c)
	}
}

// isDef reports whether an expressions is a reference that references a
// definition anywhere in its selection path.
//
// TODO(performance): this should be merged with resolve(). But for now keeping
// this code isolated makes it easier to see what it is for.
func isDef(x Expr) bool {
	switch r := x.(type) {
	case *FieldReference:
		return r.Label.IsDef()

	case *SelectorExpr:
		if r.Sel.IsDef() {
			return true
		}
		return isDef(r.X)

	case *IndexExpr:
		return isDef(r.X)
	}
	return false
}

// updateCyclicStatus looks for proof of non-cyclic conjuncts to override
// a structural cycle.
func (n *nodeContext) updateCyclicStatus(env *Environment) {
	if env == nil || !env.Cyclic {
		n.hasNonCycle = true
	}
}

func updateCyclic(c Conjunct, cyclic bool, deref *Vertex, a []*Vertex) Conjunct {
	env := c.Env
	switch {
	case env == nil:
		if !cyclic && deref == nil {
			return c
		}
		env = &Environment{Cyclic: cyclic}
	case deref == nil && env.Cyclic == cyclic && len(a) == 0:
		return c
	default:
		// The conjunct may still be in use in other fields, so we should
		// make a new copy to mark Cyclic only for this case.
		e := *env
		e.Cyclic = e.Cyclic || cyclic
		env = &e
	}
	if deref != nil || len(a) > 0 {
		cp := make([]*Vertex, 0, len(a)+1)
		cp = append(cp, a...)
		if deref != nil {
			cp = append(cp, deref)
		}
		env.Deref = cp
	}
	if deref != nil {
		env.Cycles = append(env.Cycles, deref)
	}
	return MakeConjunct(env, c.Expr(), c.CloseInfo)
}

func (n *nodeContext) addValueConjunct(env *Environment, v Value, id CloseInfo) {
	n.updateCyclicStatus(env)

	ctx := n.ctx

	if x, ok := v.(*Vertex); ok {
		if m, ok := x.BaseValue.(*StructMarker); ok {
			n.aStruct = x
			n.aStructID = id
			if m.NeedClose {
				n.node.Closed = true // TODO: remove.
				id = id.SpawnRef(x, IsDef(x), x)
				id.IsClosed = true
			}
		}

		cyclic := env != nil && env.Cyclic

		if !x.IsData() {
			// TODO: this really shouldn't happen anymore.
			if isComplexStruct(ctx, x) {
				// This really shouldn't happen, but just in case.
				n.addVertexConjuncts(env, id, x, x)
				return
			}

			for _, c := range x.Conjuncts {
				c = updateCyclic(c, cyclic, nil, nil)
				c.CloseInfo = id
				n.addExprConjunct(c) // TODO: Pass from eval
			}
			return
		}

		// TODO: evaluate value?
		switch v := x.BaseValue.(type) {
		default:
			panic(fmt.Sprintf("invalid type %T", x.BaseValue))

		case *ListMarker:
			n.vLists = append(n.vLists, x)
			return

		case *StructMarker:
			// TODO: this would not be necessary if acceptor.isClose were
			// not used. See comment at acceptor.
			s := &StructLit{}

			// Keep ordering of Go struct for topological sort.
			n.node.AddStruct(s, env, id)
			n.node.Structs = append(n.node.Structs, x.Structs...)

			for _, a := range x.Arcs {
				c := MakeConjunct(nil, a, id)
				c = updateCyclic(c, cyclic, nil, nil)
				n.insertField(a.Label, c)
				s.MarkField(a.Label)
			}

		case Value:
			n.addValueConjunct(env, v, id)

			// TODO: this would not be necessary if acceptor.isClose were
			// not used. See comment at acceptor.
			s := &StructLit{}
			n.node.AddStruct(s, env, id)

			for _, a := range x.Arcs {
				// TODO(errors): report error when this is a regular field.
				c := MakeConjunct(nil, a, id)
				c = updateCyclic(c, cyclic, nil, nil)
				n.insertField(a.Label, c)
				s.MarkField(a.Label)
			}
		}

		return
		// TODO: Use the Closer to close other fields as well?
	}

	switch b := v.(type) {
	case *Bottom:
		n.addBottom(b)
		return
	case *Builtin:
		if v := b.BareValidator(); v != nil {
			n.addValueConjunct(env, v, id)
			return
		}
	}

	if !n.updateNodeType(v.Kind(), v, id) {
		return
	}

	switch x := v.(type) {
	case *Disjunction:
		n.addDisjunctionValue(env, x, id)

	case *Conjunction:
		for _, x := range x.Values {
			n.addValueConjunct(env, x, id)
		}

	case *Top:
		n.hasTop = true

	case *BasicType:
		// handled above

	case *BoundValue:
		switch x.Op {
		case LessThanOp, LessEqualOp:
			if y := n.upperBound; y != nil {
				n.upperBound = nil
				v := SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.upperBound)
					err.AddClosedPositions(id)
				}
				n.addValueConjunct(env, v, id)
				return
			}
			n.upperBound = x

		case GreaterThanOp, GreaterEqualOp:
			if y := n.lowerBound; y != nil {
				n.lowerBound = nil
				v := SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.lowerBound)
					err.AddClosedPositions(id)
				}
				n.addValueConjunct(env, v, id)
				return
			}
			n.lowerBound = x

		case EqualOp, NotEqualOp, MatchOp, NotMatchOp:
			// This check serves as simplifier, but also to remove duplicates.
			k := 0
			match := false
			for _, c := range n.checks {
				if y, ok := c.(*BoundValue); ok {
					switch z := SimplifyBounds(ctx, n.kind, x, y); {
					case z == y:
						match = true
					case z == x:
						continue
					}
				}
				n.checks[k] = c
				k++
			}
			n.checks = n.checks[:k]
			if !match {
				n.checks = append(n.checks, x)
			}
			return
		}

	case Validator:
		// This check serves as simplifier, but also to remove duplicates.
		for i, y := range n.checks {
			if b := SimplifyValidator(ctx, x, y); b != nil {
				n.checks[i] = b
				return
			}
		}
		n.updateNodeType(x.Kind(), x, id)
		n.checks = append(n.checks, x)

	case *Vertex:
	// handled above.

	case Value: // *NullLit, *BoolLit, *NumLit, *StringLit, *BytesLit, *Builtin
		if y := n.scalar; y != nil {
			if b, ok := BinOp(ctx, EqualOp, x, y).(*Bool); !ok || !b.B {
				n.addConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id)
			}
			// TODO: do we need to explicitly add again?
			// n.scalar = nil
			// n.addValueConjunct(c, BinOp(c, EqualOp, x, y))
			break
		}
		n.scalar = x
		n.scalarID = id

	default:
		panic(fmt.Sprintf("unknown value type %T", x))
	}

	if n.lowerBound != nil && n.upperBound != nil {
		if u := SimplifyBounds(ctx, n.kind, n.lowerBound, n.upperBound); u != nil {
			if err := valueError(u); err != nil {
				err.AddPosition(n.lowerBound)
				err.AddPosition(n.upperBound)
				err.AddClosedPositions(id)
			}
			n.lowerBound = nil
			n.upperBound = nil
			n.addValueConjunct(env, u, id)
		}
	}
}

func valueError(v Value) *ValueError {
	if v == nil {
		return nil
	}
	b, _ := v.(*Bottom)
	if b == nil {
		return nil
	}
	err, _ := b.Err.(*ValueError)
	if err == nil {
		return nil
	}
	return err
}

// addStruct collates the declarations of a struct.
//
// addStruct fulfills two additional pivotal functions:
//   1) Implement vertex unification (this happens through De Bruijn indices
//      combined with proper set up of Environments).
//   2) Implied closedness for definitions.
//
func (n *nodeContext) addStruct(
	env *Environment,
	s *StructLit,
	closeInfo CloseInfo) {

	n.updateCyclicStatus(env) // to handle empty structs.

	ctx := n.ctx

	// NOTE: This is a crucial point in the code:
	// Unification derferencing happens here. The child nodes are set to
	// an Environment linked to the current node. Together with the De Bruijn
	// indices, this determines to which Vertex a reference resolves.

	// TODO(perf): consider using environment cache:
	// var childEnv *Environment
	// for _, s := range n.nodeCache.sub {
	// 	if s.Up == env {
	// 		childEnv = s
	// 	}
	// }
	childEnv := &Environment{
		Up:     env,
		Vertex: n.node,
	}
	if env != nil {
		childEnv.Cyclic = env.Cyclic
		childEnv.Deref = env.Deref
	}

	parent := n.node.AddStruct(s, childEnv, closeInfo)
	closeInfo.IsClosed = false
	parent.Disable = true // disable until processing is done.

	hasEmbed := false

	s.Init()

	for _, d := range s.Decls {
		switch x := d.(type) {
		case *Field:
			// handle in next iteration.

		case *OptionalField:
			if x.Label.IsString() {
				n.aStruct = s
				n.aStructID = closeInfo
			}

		case *DynamicField:
			n.aStruct = s
			n.aStructID = closeInfo
			n.dynamicFields = append(n.dynamicFields, envDynamic{childEnv, x, closeInfo, nil})

		case *ForClause:
			// Why is this not an embedding?
			n.forClauses = append(n.forClauses, envYield{childEnv, x, closeInfo, nil})

		case Yielder:
			// Why is this not an embedding?
			n.ifClauses = append(n.ifClauses, envYield{childEnv, x, closeInfo, nil})

		case Expr:
			hasEmbed = true

			// add embedding to optional

			// TODO(perf): only do this if addExprConjunct below will result in
			// a fieldSet. Otherwise the entry will just be removed next.
			id := closeInfo.SpawnEmbed(x)

			// push and opo embedding type.
			n.addExprConjunct(MakeConjunct(childEnv, x, id))

		case *BulkOptionalField:
			n.aStruct = s
			n.aStructID = closeInfo

		case *Ellipsis:
			n.aStruct = s
			n.aStructID = closeInfo

		default:
			panic("unreachable")
		}
	}

	if !hasEmbed {
		n.aStruct = s
		n.aStructID = closeInfo
	}

	// Apply existing fields
	for _, arc := range n.node.Arcs {
		// Reuse Acceptor interface.
		parent.MatchAndInsert(ctx, arc)
	}

	parent.Disable = false

	for _, d := range s.Decls {
		switch x := d.(type) {
		case *Field:
			if x.Label.IsString() {
				n.aStruct = s
				n.aStructID = closeInfo
			}
			n.insertField(x.Label, MakeConjunct(childEnv, x, closeInfo))
		}
	}
}

func (n *nodeContext) insertField(f Feature, x Conjunct) *Vertex {
	ctx := n.ctx
	arc, isNew := n.node.GetArc(f)

	// TODO: disallow adding conjuncts when cache set?
	arc.AddConjunct(x)

	if isNew {
		for _, s := range n.node.Structs {
			if s.Disable {
				continue
			}
			s.MatchAndInsert(ctx, arc)
		}
	}
	return arc
}

// expandOne adds dynamic fields to a node until a fixed point is reached.
// On each iteration, dynamic fields that cannot resolve due to incomplete
// values are skipped. They will be retried on the next iteration until no
// progress can be made. Note that a dynamic field may add more dynamic fields.
//
// forClauses are processed after all other clauses. A struct may be referenced
// before it is complete, meaning that fields added by other forms of injection
// may influence the result of a for clause _after_ it has already been
// processed. We could instead detect such insertion and feed it to the
// ForClause to generate another entry or have the for clause be recomputed.
// This seems to be too complicated and lead to iffy edge cases.
// TODO(errors): detect when a field is added to a struct that is already used
// in a for clause.
func (n *nodeContext) expandOne() (done bool) {
	// Don't expand incomplete expressions if we detected a cycle.
	if n.done() || (n.hasCycle && !n.hasNonCycle) {
		return false
	}

	var progress bool

	if progress = n.injectDynamic(); progress {
		return true
	}

	if progress = n.injectEmbedded(&(n.ifClauses)); progress {
		return true
	}

	if progress = n.injectEmbedded(&(n.forClauses)); progress {
		return true
	}

	// Do expressions after comprehensions, as comprehensions can never
	// refer to embedded scalars, whereas expressions may refer to generated
	// fields if we were to allow attributes to be defined alongside
	// scalars.
	exprs := n.exprs
	n.exprs = n.exprs[:0]
	for _, x := range exprs {
		n.addExprConjunct(x.c)

		// collect and and or
	}
	if len(n.exprs) < len(exprs) {
		return true
	}

	// No progress, report error later if needed: unification with
	// disjuncts may resolve this later later on.
	return false
}

// injectDynamic evaluates and inserts dynamic declarations.
func (n *nodeContext) injectDynamic() (progress bool) {
	ctx := n.ctx
	k := 0

	a := n.dynamicFields
	for _, d := range n.dynamicFields {
		var f Feature
		v, complete := ctx.Evaluate(d.env, d.field.Key)
		if !complete {
			d.err, _ = v.(*Bottom)
			a[k] = d
			k++
			continue
		}
		if b, _ := v.(*Bottom); b != nil {
			n.addValueConjunct(nil, b, d.id)
			continue
		}
		f = ctx.Label(d.field.Key, v)
		n.insertField(f, MakeConjunct(d.env, d.field, d.id))
	}

	progress = k < len(n.dynamicFields)

	n.dynamicFields = a[:k]

	return progress
}

// injectEmbedded evaluates and inserts embeddings. It first evaluates all
// embeddings before inserting the results to ensure that the order of
// evaluation does not matter.
func (n *nodeContext) injectEmbedded(all *[]envYield) (progress bool) {
	ctx := n.ctx
	type envStruct struct {
		env *Environment
		s   *StructLit
	}
	var sa []envStruct
	f := func(env *Environment, st *StructLit) {
		sa = append(sa, envStruct{env, st})
	}

	k := 0
	for i := 0; i < len(*all); i++ {
		d := (*all)[i]
		sa = sa[:0]

		if err := ctx.Yield(d.env, d.yield, f); err != nil {
			if err.IsIncomplete() {
				d.err = err
				(*all)[k] = d
				k++
			} else {
				// continue to collect other errors.
				n.addBottom(err)
			}
			continue
		}

		for _, st := range sa {
			n.addStruct(st.env, st.s, d.id)
		}
	}

	progress = k < len(*all)

	*all = (*all)[:k]

	return progress
}

// addLists
//
// TODO: association arrays:
// If an association array marker was present in a struct, create a struct node
// instead of a list node. In either case, a node may only have list fields
// or struct fields and not both.
//
// addLists should be run after the fixpoint expansion:
//    - it enforces that comprehensions may not refer to the list itself
//    - there may be no other fields within the list.
//
// TODO(embeddedScalars): for embedded scalars, there should be another pass
// of evaluation expressions after expanding lists.
func (n *nodeContext) addLists(c *OpContext) (oneOfTheLists Expr, anID CloseInfo) {
	if len(n.lists) == 0 && len(n.vLists) == 0 {
		return nil, CloseInfo{}
	}

	isOpen := true
	max := 0
	var maxNode Expr

	if m, ok := n.node.BaseValue.(*ListMarker); ok {
		isOpen = m.IsOpen
		max = len(n.node.Arcs)
	}

	for _, l := range n.vLists {
		oneOfTheLists = l

		elems := l.Elems()
		isClosed := l.IsClosed(c)

		switch {
		case len(elems) < max:
			if isClosed {
				n.invalidListLength(len(elems), max, l, maxNode)
				continue
			}

		case len(elems) > max:
			if !isOpen {
				n.invalidListLength(max, len(elems), maxNode, l)
				continue
			}
			isOpen = !isClosed
			max = len(elems)
			maxNode = l

		case isClosed:
			isOpen = false
			maxNode = l
		}

		for _, a := range elems {
			if a.Conjuncts == nil {
				x := a.BaseValue.(Value)
				n.insertField(a.Label, MakeConjunct(nil, x, CloseInfo{}))
				continue
			}
			for _, c := range a.Conjuncts {
				n.insertField(a.Label, c)
			}
		}
	}

outer:
	for i, l := range n.lists {
		n.updateCyclicStatus(l.env)

		index := int64(0)
		hasComprehension := false
		for j, elem := range l.list.Elems {
			switch x := elem.(type) {
			case Yielder:
				err := c.Yield(l.env, x, func(e *Environment, st *StructLit) {
					label, err := MakeLabel(x.Source(), index, IntLabel)
					n.addErr(err)
					index++
					c := MakeConjunct(e, st, l.id)
					n.insertField(label, c)
				})
				hasComprehension = true
				if err != nil {
					n.addBottom(err)
					continue outer
				}

			case *Ellipsis:
				if j != len(l.list.Elems)-1 {
					n.addErr(c.Newf("ellipsis must be last element in list"))
				}

				n.lists[i].elipsis = x

			default:
				label, err := MakeLabel(x.Source(), index, IntLabel)
				n.addErr(err)
				index++ // TODO: don't use insertField.
				n.insertField(label, MakeConjunct(l.env, x, l.id))
			}

			// Terminate early n case of runaway comprehension.
			if !isOpen && int(index) > max {
				n.invalidListLength(max, int(index), maxNode, l.list)
				continue outer
			}
		}

		oneOfTheLists = l.list
		anID = l.id

		switch closed := n.lists[i].elipsis == nil; {
		case int(index) < max:
			if closed {
				n.invalidListLength(int(index), max, l.list, maxNode)
				continue
			}

		case int(index) > max,
			closed && isOpen,
			(!closed == isOpen) && !hasComprehension:
			max = int(index)
			maxNode = l.list
			isOpen = !closed
		}

		n.lists[i].n = index
	}

	// add additionalItem values to list and construct optionals.
	elems := n.node.Elems()
	for _, l := range n.vLists {
		if !l.IsClosed(c) {
			continue
		}

		newElems := l.Elems()
		if len(newElems) >= len(elems) {
			continue // error generated earlier, if applicable.
		}

		for _, arc := range elems[len(newElems):] {
			l.MatchAndInsert(c, arc)
		}
	}

	for _, l := range n.lists {
		if l.elipsis == nil {
			continue
		}

		s := &StructLit{Decls: []Decl{l.elipsis}}
		s.Init()
		info := n.node.AddStruct(s, l.env, l.id)

		for _, arc := range elems[l.n:] {
			info.MatchAndInsert(c, arc)
		}
	}

	sources := []ast.Expr{}
	// Add conjuncts for additional items.
	for _, l := range n.lists {
		if l.elipsis == nil {
			continue
		}
		if src, _ := l.elipsis.Source().(ast.Expr); src != nil {
			sources = append(sources, src)
		}
	}

	if m, ok := n.node.BaseValue.(*ListMarker); !ok {
		n.node.SetValue(c, Partial, &ListMarker{
			Src:    ast.NewBinExpr(token.AND, sources...),
			IsOpen: isOpen,
		})
	} else {
		if expr, _ := m.Src.(ast.Expr); expr != nil {
			sources = append(sources, expr)
		}
		m.Src = ast.NewBinExpr(token.AND, sources...)
		m.IsOpen = m.IsOpen && isOpen
	}

	n.lists = n.lists[:0]
	n.vLists = n.vLists[:0]

	return oneOfTheLists, anID
}

func (n *nodeContext) invalidListLength(na, nb int, a, b Expr) {
	n.addErr(n.ctx.Newf("incompatible list lengths (%d and %d)", na, nb))
}
