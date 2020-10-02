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

// Package eval contains the high level CUE evaluation strategy.
//
// CUE allows for a significant amount of freedom in order of evaluation due to
// the commutativity of the unification operation. This package implements one
// of the possible strategies.
package eval

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
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
)

var Debug = false

// TODO TODO TODO TODO TODO TODO  TODO TODO TODO  TODO TODO TODO  TODO TODO TODO
//
// - Reuse work from previous cycles. For instance, if we can guarantee that a
//   value is always correct for partial results, we can just process the arcs
//   going from Partial to Finalized, without having to reevaluate the value.
//
// - Test closedness far more thoroughly.
//

func Evaluate(r adt.Runtime, v *adt.Vertex) {
	format := func(n adt.Node) string {
		return debug.NodeString(r, n, printConfig)
	}
	e := New(r)
	c := adt.New(v, &adt.Config{
		Runtime: r,
		Unifier: e,
		Format:  format,
	})
	e.Unify(c, v, adt.Finalized)
}

func New(r adt.Runtime) *Evaluator {
	return &Evaluator{r: r, index: r}
}

type Stats struct {
	DisjunctCount int
	UnifyCount    int
}

var stats = template.Must(template.New("stats").Parse(`{{"" -}}
Unifications: {{.UnifyCount}}
Disjuncts:    {{.DisjunctCount}}`))

func (s *Stats) String() string {
	buf := strings.Builder{}
	_ = stats.Execute(&buf, s)
	return buf.String()
}

func (e *Evaluator) Stats() *Stats {
	return &e.stats
}

// TODO: Note: NewContext takes essentially a cue.Value. By making this
// type more central, we can perhaps avoid context creation.

func NewContext(r adt.Runtime, v *adt.Vertex) *adt.OpContext {
	e := New(r)
	return e.NewContext(v)
}

var printConfig = &debug.Config{Compact: true}

func (e *Evaluator) NewContext(v *adt.Vertex) *adt.OpContext {
	format := func(n adt.Node) string {
		return debug.NodeString(e.r, n, printConfig)
	}
	return adt.New(v, &adt.Config{
		Runtime: e.r,
		Unifier: e,
		Format:  format,
	})
}

var structSentinel = &adt.StructMarker{}

var incompleteSentinel = &adt.Bottom{
	Code: adt.IncompleteError,
	Err:  errors.Newf(token.NoPos, "incomplete"),
}

type Evaluator struct {
	r     adt.Runtime
	index adt.StringIndexer

	stats Stats
}

func (e *Evaluator) Eval(v *adt.Vertex) errors.Error {
	if v.Value == nil {
		ctx := adt.NewContext(e.r, e, v)
		e.Unify(ctx, v, adt.Finalized)
	}

	// extract error if needed.
	return nil
}

// Evaluate is used to evaluate a sub expression while evaluating a Vertex
// with Unify. It may or may not return the original Vertex. It may also
// terminate evaluation early if it has enough evidence that a certain value
// can be the only value in a valid configuration. This means that an error
// may go undetected at this point, as long as it is caught later.
//
func (e *Evaluator) Evaluate(c *adt.OpContext, v *adt.Vertex) adt.Value {
	var resultValue adt.Value
	if v.Value == nil {
		save := *v
		// Use node itself to allow for cycle detection.
		s := e.evalVertex(c, v, adt.Partial, nil)

		if d := s.disjunct; d != nil && len(d.Values) > 1 && d.NumDefaults != 1 {
			v.Value = d
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
		}

		resultValue = v.Value

		result := s.result()
		*v = save

		if result.Value != nil {
			*v = *result
			resultValue = result.Value
		}

		// TODO: this seems unnecessary as long as we have a better way
		// to handle incomplete, and perhaps referenced. nodes.
		if c.IsTentative() && isStruct(v) {
			// TODO(perf): do something more efficient perhaps? This discards
			// the computed arcs so far. Instead, we could have a separate
			// marker to accumulate results. As this only happens within
			// comprehensions, the effect is likely minimal, though.
			arcs := v.Arcs
			*v = save
			return &adt.Vertex{
				Parent: v.Parent,
				Value:  &adt.StructMarker{},
				Arcs:   arcs,
				Closed: result.Closed,
			}
		}
		// *v = save // DO NOT ADD.
		err, _ := resultValue.(*adt.Bottom)
		// BEFORE RESTORING, copy the value to return one
		// with the temporary arcs.
		if !s.done() && (err == nil || err.IsIncomplete()) {
			// Clear values afterwards
			*v = save
		}
		if !s.done() && s.hasDisjunction() {
			return &adt.Bottom{Code: adt.IncompleteError}
		}
		if s.hasResult() {
			if b, _ := v.Value.(*adt.Bottom); b != nil {
				*v = save
				return b
			}
			// TODO: Only use result when not a cycle.
			v = result
		}
		// TODO: Store if concrete and fully resolved.

	} else {
		b, _ := v.Value.(*adt.Bottom)
		if b != nil {
			return b
		}
	}

	switch v.Value.(type) {
	case nil:
		// Error saved in result.
		return resultValue // incomplete

	case *adt.ListMarker, *adt.StructMarker:
		return v

	default:
		return v.Value
	}
}

// Unify implements adt.Unifier.
//
// May not evaluate the entire value, but just enough to be able to compute.
//
// Phase one: record everything concrete
// Phase two: record incomplete
// Phase three: record cycle.
func (e *Evaluator) Unify(c *adt.OpContext, v *adt.Vertex, state adt.VertexStatus) {
	e.UnifyAccept(c, v, state, nil) //v.Closed)
}

// UnifyAccept is like Unify, but takes an extra argument to override the
// accepted set of fields.
//
// Instead of deriving the allowed set of fields, it verifies this set by
// consulting the given Acceptor. This can be useful when splitting an existing
// values into individual conjuncts and then unifying some of its components
// back into a new value. Under normal circumstances, this may not always
// succeed as the missing context may result in stricter closedness rules.
func (e *Evaluator) UnifyAccept(c *adt.OpContext, v *adt.Vertex, state adt.VertexStatus, accept adt.Acceptor) {

	// defer c.PopVertex(c.PushVertex(v))

	if state <= v.Status()+1 {
		return
	}

	if x := v.Value; x != nil {
		// if state == adt.Partial || x == cycle {
		// 	return
		// }
		return
	}

	n := e.evalVertex(c, v, state, accept)

	switch d := n.disjunct; {
	case d != nil && len(d.Values) == 1:
		*v = *(d.Values[0])

	case d != nil && len(d.Values) > 0:
		v.Value = d
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
		v.Closed = newDisjunctionAcceptor(n.disjunct)

	default:
		if r := n.result(); r.Value != nil {
			*v = *r
		}
	}

	// Else set it to something.

	if v.Value == nil {
		panic("error")
	}

	// Check whether result is done.
}

// evalVertex computes the vertex results. The state indicates the minimum
// status to which this vertex should be evaluated. It should be either
// adt.Finalized or adt.Partial.
func (e *Evaluator) evalVertex(c *adt.OpContext, v *adt.Vertex, state adt.VertexStatus, accept adt.Acceptor) *nodeShared {
	shared := &nodeShared{
		ctx:    c,
		eval:   e,
		node:   v,
		stack:  nil, // silence linter
		accept: accept,
	}
	saved := *v

	var closedInfo *acceptor
	if v.Parent != nil {
		closedInfo, _ = v.Parent.Closed.(*acceptor)
	}

	if !v.Label.IsInt() && closedInfo != nil {
		ci := closedInfo
		// Visit arcs recursively to validate and compute error.
		switch ok, err := ci.verifyArc(c, v.Label, v); {
		case err != nil:
			// Record error in child node to allow recording multiple
			// conflicts at the appropriate place, to allow valid fields to
			// be represented normally and, most importantly, to avoid
			// recursive processing of a disallowed field.
			v.Value = err
			v.SetValue(c, adt.Finalized, err)
			return shared

		case !ok: // hidden field
			// A hidden field is except from checking. Ensure that the
			// closedness doesn't carry over into children.
			// TODO: make this more fine-grained per package, allowing
			// checked restrictions to be defined within the package.
			closedInfo = closedInfo.clone()
			for i := range closedInfo.Canopy {
				closedInfo.Canopy[i].IsDef = false
			}
		}
	}

	defer c.PopArc(c.PushArc(v))

	e.stats.UnifyCount++
	for i := 0; ; i++ {
		e.stats.DisjunctCount++

		// Clear any remaining error.
		if err := c.Err(); err != nil {
			panic("uncaught error")
		}

		// Set the cache to a cycle error to ensure a cyclic reference will result
		// in an error if applicable. A cyclic error may be ignored for
		// non-expression references. The cycle error may also be removed as soon
		// as there is evidence what a correct value must be, but before all
		// validation has taken place.
		*v = saved
		v.Value = cycle

		if closedInfo != nil {
			v.Closed = closedInfo.clone()
		}

		v.UpdateStatus(adt.Evaluating)

		// If the result is a struct, it needs to be closed if:
		//   1) this node introduces a definition
		//   2) this node is a child of a node that introduces a definition,
		//      recursively.
		//   3) this node embeds a closed struct.
		needClose := v.Label.IsDef()
		// if needClose {
		// 	closedInfo.isClosed = true
		// }

		n := &nodeContext{
			arcMap:     map[arcKey]bool{},
			kind:       adt.TopKind,
			nodeShared: shared,
			needClose:  needClose,

			// These get cleared upon proof to the contrary.
			// isDefault: true,
			isFinal: true,
		}

		for _, x := range v.Conjuncts {
			// TODO: needed for reentrancy. Investigate usefulness for cycle
			// detection.
			n.addExprConjunct(x)
		}

		if i == 0 {
			// Use maybeSetCache for cycle breaking
			for n.maybeSetCache(); n.expandOne(); n.maybeSetCache() {
			}
			if v.Status() > adt.Evaluating && state <= adt.Partial {
				// We have found a partial result. There may still be errors
				// down the line which may result from further evaluating this
				// field, but that will be caught when evaluating this field
				// for real.
				shared.setResult(v)
				return shared
			}
			if !n.done() && len(n.disjunctions) > 0 && isEvaluating(v) {
				// We disallow entering computations of disjunctions with
				// incomplete data.
				b := c.NewErrf("incomplete cause disjunction")
				b.Code = adt.IncompleteError
				v.SetValue(n.ctx, adt.Finalized, b)
				shared.setResult(v)
				return shared
			}
		}

		// Handle disjunctions. If there are no disjunctions, this call is
		// equivalent to calling n.postDisjunct.
		if n.tryDisjuncts() {
			if v.Value == nil {
				v.Value = n.getValidators()
			}

			break
		}
	}

	return shared
}

func isStruct(v *adt.Vertex) bool {
	_, ok := v.Value.(*adt.StructMarker)
	return ok
}

func (n *nodeContext) postDisjunct() {
	ctx := n.ctx

	for {
		// Use maybeSetCache for cycle breaking
		for n.maybeSetCache(); n.expandOne(); n.maybeSetCache() {
		}

		if aList, id := n.addLists(ctx); aList != nil {
			n.updateNodeType(adt.ListKind, aList, id)
		} else {
			break
		}
	}

	if n.aStruct != nil {
		n.updateNodeType(adt.StructKind, n.aStruct, n.aStructID)
	}

	switch err := n.getErr(); {
	case err != nil:
		n.node.Value = err
		n.errs = nil

	default:
		if isEvaluating(n.node) {
			// TODO: this does not yet validate all values.

			if !n.done() { // && !ctx.IsTentative() {
				// collect incomplete errors.
				// 	len(n.ifClauses) == 0 &&
				// 	len(n.forClauses) == 0 &&
				var err *adt.Bottom // n.incomplete
				// err = n.incomplete
				for _, d := range n.dynamicFields {
					x, _ := ctx.Concrete(d.env, d.field.Key, "dynamic field")
					b, _ := x.(*adt.Bottom)
					err = adt.CombineErrors(nil, err, b)
				}
				for _, c := range n.forClauses {
					f := func(env *adt.Environment, st *adt.StructLit) {}
					err = adt.CombineErrors(nil, err, ctx.Yield(c.env, c.yield, f))
				}
				for _, x := range n.exprs {
					x, _ := ctx.Evaluate(x.Env, x.Expr())
					b, _ := x.(*adt.Bottom)
					err = adt.CombineErrors(nil, err, b)
				}
				if err == nil {
					// safeguard.
					err = incompleteSentinel
				}
				n.node.Value = err
			} else {
				n.node.Value = nil
			}
		}

		// We are no longer evaluating.
		n.node.UpdateStatus(adt.Partial)

		// Either set to Conjunction or error.
		var v adt.Value = n.node.Value
		// TODO: verify and simplify the below code to determine whether
		// something is a struct.
		markStruct := false
		if n.aStruct != nil {
			markStruct = true
		} else if len(n.node.Structs) > 0 {
			markStruct = n.kind&adt.StructKind != 0 && !n.hasTop
		}
		if v == nil && markStruct {
			n.node.Value = &adt.StructMarker{}
			v = n.node
		}
		if v != nil && adt.IsConcrete(v) {
			if n.lowerBound != nil {
				if b := ctx.Validate(n.lowerBound, v); b != nil {
					// TODO(errors): make Validate return boolean and generate
					// optimized conflict message. Also track and inject IDs
					// to determine origin location.s
					if e, _ := b.Err.(*adt.ValueError); e != nil {
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
					if e, _ := b.Err.(*adt.ValueError); e != nil {
						e.AddPosition(n.upperBound)
						e.AddPosition(v)
					}
					n.addBottom(b)
				}
			}
			for _, v := range n.checks {
				// TODO(errors): make Validate return boolean and generate
				// optimized conflict message. Also track and inject IDs
				// to determine origin location.s
				if b := ctx.Validate(v, n.node); b != nil {
					n.addBottom(b)
				}
			}

		} else if !ctx.IsTentative() {
			n.node.Value = n.getValidators()
		}
		// else if v == nil {
		// 	n.node.Value = incompleteSentinel
		// }

		if v == nil {
			break
		}

		switch {
		case v.Kind() == adt.ListKind:
			for _, a := range n.node.Arcs {
				if a.Label.Typ() == adt.StringLabel {
					n.addErr(ctx.Newf("list may not have regular fields"))
					// TODO(errors): add positions for list and arc definitions.

				}
			}

			// case !isStruct(n.node) && v.Kind() != adt.BottomKind:
			// 	for _, a := range n.node.Arcs {
			// 		if a.Label.IsRegular() {
			// 			n.addErr(errors.Newf(token.NoPos,
			// 				// TODO(errors): add positions of non-struct values and arcs.
			// 				"cannot combine scalar values with arcs"))
			// 		}
			// 	}
		}
	}

	n.updateClosedInfo()

	if cyclic := n.hasCycle && !n.hasNonCycle; cyclic {
		n.node.Value = adt.CombineErrors(nil, n.node.Value, &adt.Bottom{
			Code:  adt.StructuralCycleError,
			Err:   ctx.Newf("structural cycle"),
			Value: n.node.Value,
			// TODO: probably, this should have the referenced arc.
		})
	} else {
		// Visit arcs recursively to validate and compute error.
		for _, a := range n.node.Arcs {
			// Call UpdateStatus here to be absolutely sure the status is set
			// correctly and that we are not regressing.
			n.node.UpdateStatus(adt.EvaluatingArcs)
			n.eval.Unify(ctx, a, adt.Finalized)
			if err, _ := a.Value.(*adt.Bottom); err != nil {
				n.node.AddChildError(err)
			}
		}
	}

	n.node.UpdateStatus(adt.Finalized)
}

func (n *nodeContext) updateClosedInfo() {
	if err := n.getErr(); err != nil {
		if b, _ := n.node.Value.(*adt.Bottom); b != nil {
			err = adt.CombineErrors(nil, b, err)
		}
		n.node.Value = err
		// TODO: add return: if evaluation of arcs is important it can be done
		// later. Logically we're done.
	}

	if n.node.IsList() {
		return
	}

	a, _ := n.node.Closed.(*acceptor)
	if a == nil {
		if !n.node.IsList() && !n.needClose {
			return
		}
		a = closedInfo(n.node)
	}

	// TODO: set this earlier.
	n.needClose = n.needClose || a.isClosed
	a.isClosed = n.needClose

	// TODO: consider removing this. Now checking is done at the top of
	// evalVertex, skipping one level of checks happens naturally again
	// for Value.Unify (API).
	ctx := n.ctx

	if accept := n.nodeShared.accept; accept != nil {
		for _, a := range n.node.Arcs {
			if !accept.Accept(n.ctx, a.Label) {
				label := a.Label.SelectorString(ctx)
				n.node.Value = ctx.NewErrf(
					"field `%s` not allowed by Acceptor", label)
			}
		}
	}
}

// TODO: this is now a sentinel. Use a user-facing error that traces where
// the cycle originates.
var cycle = &adt.Bottom{
	Err:  errors.Newf(token.NoPos, "cycle error"),
	Code: adt.CycleError,
}

func isEvaluating(v *adt.Vertex) bool {
	isCycle := v.Status() == adt.Evaluating
	if isCycle != (v.Value == cycle) {
		panic(fmt.Sprintf("cycle data of sync %d vs %#v", v.Status(), v.Value))
	}
	return isCycle
}

type nodeShared struct {
	eval *Evaluator
	ctx  *adt.OpContext
	sub  []*adt.Environment // Environment cache
	node *adt.Vertex

	// Disjunction handling
	disjunct   *adt.Disjunction
	resultNode *nodeContext
	result_    adt.Vertex
	stack      []int

	// Closedness override.
	accept adt.Acceptor
}

func (n *nodeShared) result() *adt.Vertex {
	return &n.result_
}

func (n *nodeShared) setResult(v *adt.Vertex) {
	n.result_ = *v
}

func (n *nodeShared) hasResult() bool {
	return n.resultNode != nil //|| n.hasResult_
	// return n.resultNode != nil || n.hasResult_
}

func (n *nodeShared) done() bool {
	// if d := n.disjunct; d == nil || len(n.disjunct.Values) == 0 {
	// 	return false
	// }
	if n.resultNode == nil {
		return false
	}
	return n.resultNode.done()
}

func (n *nodeShared) hasDisjunction() bool {
	if n.resultNode == nil {
		return false
	}
	return len(n.resultNode.disjunctions) > 0
}

func (n *nodeShared) isDefault() bool {
	if n.resultNode == nil {
		return false
	}
	return n.resultNode.defaultMode == isDefault
}

type arcKey struct {
	arc *adt.Vertex
	id  adt.ID
}

// A nodeContext is used to collate all conjuncts of a value to facilitate
// unification. Conceptually order of unification does not matter. However,
// order has relevance when performing checks of non-monotic properities. Such
// checks should only be performed once the full value is known.
type nodeContext struct {
	*nodeShared

	// TODO:
	// filter *adt.Vertex a subset of composite with concrete fields for
	// bloom-like filtering of disjuncts. We should first verify, however,
	// whether some breath-first search gives sufficient performance, as this
	// should already ensure a quick-fail for struct disjunctions with
	// discriminators.

	arcMap map[arcKey]bool

	// Current value (may be under construction)
	scalar   adt.Value // TODO: use Value in node.
	scalarID adt.ID

	// Concrete conjuncts
	kind       adt.Kind
	kindExpr   adt.Expr        // expr that adjust last value (for error reporting)
	kindID     adt.ID          // for error tracing
	lowerBound *adt.BoundValue // > or >=
	upperBound *adt.BoundValue // < or <=
	checks     []adt.Validator // BuiltinValidator, other bound values.
	errs       *adt.Bottom
	incomplete *adt.Bottom

	// Struct information
	dynamicFields []envDynamic
	ifClauses     []envYield
	forClauses    []envYield
	// TODO: remove this and annotate directly in acceptor.
	needClose bool
	aStruct   adt.Expr
	aStructID adt.ID
	hasTop    bool

	// Expression conjuncts
	lists  []envList
	vLists []*adt.Vertex
	exprs  []adt.Conjunct

	hasCycle    bool // has conjunct with structural cycle
	hasNonCycle bool // has conjunct without structural cycle

	// Disjunction handling
	disjunctions []envDisjunct
	defaultMode  defaultMode
	isFinal      bool
}

func closedInfo(n *adt.Vertex) *acceptor {
	a, _ := n.Closed.(*acceptor)
	if a == nil {
		a = &acceptor{}
		n.Closed = a
	}
	return a
}

// TODO(errors): return a dedicated ConflictError that can track original
// positions on demand.
//
// Fully detailed origin tracking could be created on the back of CloseIDs
// tracked in acceptor.Canopy. To make this fully functional, the following
// still needs to be implemented:
//
//  - We need to know the ID for every value involved in the conflict.
//    (There may be multiple if the result comes from a simplification).
//  - CloseIDs should be forked not only for definitions, but all references.
//  - Upon forking a CloseID, it should keep track of its enclosing CloseID
//    as well (probably as a pointer to the origin so that the reference
//    persists after compaction).
//
// The modifications to the CloseID algorithm may also benefit the computation
// of `{ #A } == #A`, which currently is not done.
//
func (n *nodeContext) addConflict(
	v1, v2 adt.Node,
	k1, k2 adt.Kind,
	id1, id2 adt.ID) {

	ctx := n.ctx

	var err *adt.ValueError
	if k1 == k2 {
		err = ctx.NewPosf(token.NoPos,
			"conflicting values %s and %s", ctx.Str(v1), ctx.Str(v2))
	} else {
		err = ctx.NewPosf(token.NoPos,
			"conflicting values %s and %s (mismatched types %s and %s)",
			ctx.Str(v1), ctx.Str(v2), k1, k2)
	}

	ci := closedInfo(n.node)
	err.AddPosition(v1)
	err.AddPosition(v2)
	err.AddPosition(ci.node(id1).Src)
	err.AddPosition(ci.node(id2).Src)

	n.addErr(err)
}

func (n *nodeContext) updateNodeType(k adt.Kind, v adt.Expr, id adt.ID) bool {
	ctx := n.ctx
	kind := n.kind & k

	switch {
	case n.kind == adt.BottomKind,
		k == adt.BottomKind:
		return false

	case kind == adt.BottomKind:
		if n.kindExpr != nil {
			n.addConflict(n.kindExpr, v, n.kind, k, n.kindID, id)
		} else {
			n.addErr(ctx.Newf(
				"conflicting value %s (mismatched types %s and %s)",
				ctx.Str(v), n.kind, k))
		}
	}

	n.kind = kind
	n.kindExpr = v
	return kind != adt.BottomKind
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
	if n.node.Status() > adt.Evaluating && n.node.IsErr() {
		return true
	}
	return n.ctx.HasErr() || n.errs != nil
}

func (n *nodeContext) getErr() *adt.Bottom {
	n.errs = adt.CombineErrors(nil, n.errs, n.ctx.Err())
	return n.errs
}

// getValidators sets the vertex' Value in case there was no concrete value.
func (n *nodeContext) getValidators() adt.Value {
	ctx := n.ctx

	a := []adt.Value{}
	// if n.node.Value != nil {
	// 	a = append(a, n.node.Value)
	// }
	kind := adt.TopKind
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
		if b, _ := c.(*adt.BoundValue); b != nil && b.Op == adt.NotEqualOp {
			if n.upperBound != nil &&
				adt.SimplifyBounds(ctx, n.kind, n.upperBound, b) != nil {
				continue
			}
			if n.lowerBound != nil &&
				adt.SimplifyBounds(ctx, n.kind, n.lowerBound, b) != nil {
				continue
			}
		}
		a = append(a, c)
		kind &= c.Kind()
	}
	if kind&^n.kind != 0 {
		a = append(a, &adt.BasicType{K: n.kind})
	}

	var v adt.Value
	switch len(a) {
	case 0:
		// Src is the combined input.
		v = &adt.BasicType{K: n.kind}

		if len(n.node.Structs) > 0 {
			v = structSentinel

		}

	case 1:
		v = a[0].(adt.Value)

	default:
		v = &adt.Conjunction{Values: a}
	}

	return v
}

func (n *nodeContext) maybeSetCache() {
	if n.node.Status() > adt.Evaluating { // n.node.Value != nil
		return
	}
	if n.scalar != nil {
		n.node.SetValue(n.ctx, adt.Partial, n.scalar)
	}
	if n.errs != nil {
		n.node.SetValue(n.ctx, adt.Partial, n.errs)
	}
}

type envDynamic struct {
	env   *adt.Environment
	field *adt.DynamicField
	id    adt.ID
}

type envYield struct {
	env   *adt.Environment
	yield adt.Yielder
	id    adt.ID
}

type envList struct {
	env     *adt.Environment
	list    *adt.ListLit
	n       int64 // recorded length after evaluator
	elipsis *adt.Ellipsis
	id      adt.ID
}

func (n *nodeContext) addBottom(b *adt.Bottom) {
	n.errs = adt.CombineErrors(nil, n.errs, b)
}

func (n *nodeContext) addErr(err errors.Error) {
	if err != nil {
		n.errs = adt.CombineErrors(nil, n.errs, &adt.Bottom{
			Err: err,
		})
	}
}

// addExprConjuncts will attempt to evaluate an adt.Expr and insert the value
// into the nodeContext if successful or queue it for later evaluation if it is
// incomplete or is not value.
func (n *nodeContext) addExprConjunct(v adt.Conjunct) {
	env := v.Env
	id := v.CloseID

	switch x := v.Expr().(type) {
	case adt.Value:
		n.addValueConjunct(env, x, id)

	case *adt.BinaryExpr:
		if x.Op == adt.AndOp {
			n.addExprConjunct(adt.MakeConjunct(env, x.X, id))
			n.addExprConjunct(adt.MakeConjunct(env, x.Y, id))
		} else {
			n.evalExpr(v)
		}

	case *adt.StructLit:
		n.addStruct(env, x, id)

	case *adt.ListLit:
		n.lists = append(n.lists, envList{env: env, list: x, id: id})

	case *adt.DisjunctionExpr:
		if n.disjunctions != nil {
			_ = n.disjunctions
		}
		n.addDisjunction(env, x, id)

	default:
		// Must be Resolver or Evaluator.
		n.evalExpr(v)
	}
}

// evalExpr is only called by addExprConjunct.
func (n *nodeContext) evalExpr(v adt.Conjunct) {
	// Require an Environment.
	ctx := n.ctx

	closeID := v.CloseID

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

outer:
	switch x := v.Expr().(type) {
	case adt.Resolver:
		arc, err := ctx.Resolve(v.Env, x)
		if err != nil {
			if err.IsIncomplete() {
				n.incomplete = adt.CombineErrors(nil, n.incomplete, err)
			} else {
				n.addBottom(err)
				break
			}
		}
		if arc == nil {
			n.exprs = append(n.exprs, v)
			break
		}

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
		if n.arcMap[arcKey{arc, v.CloseID}] {
			return
		}
		n.arcMap[arcKey{arc, v.CloseID}] = true

		// Pass detection of structural cycles from parent to children.
		cyclic := false
		if v.Env != nil {
			// If a reference is in a tainted set, so is the value it refers to.
			cyclic = v.Env.Cyclic
		}

		status := arc.Status()
		for _, d := range v.Env.Deref {
			if d == arc {
				status = adt.EvaluatingArcs
			}
		}

		switch status {
		case adt.Evaluating:
			// Reference cycle detected. We have reached a fixed point and
			// adding conjuncts at this point will not change the value. Also,
			// continuing to pursue this value will result in an infinite loop.

			// TODO: add a mechanism so that the computation will only have to
			// be done once?

			if arc == n.node {
				// TODO: we could use node sharing here. This may avoid an
				// exponential blowup during evaluation, like is possible with
				// YAML.
				break outer
			}

		case adt.EvaluatingArcs:
			// Structural cycle detected. Continue evaluation as usual, but
			// keep track of whether any other conjuncts without structural
			// cycles are added. If not, evaluation of child arcs will end
			// with this node.

			// For the purpose of determining whether at least one non-cyclic
			// conjuncts exists, we consider all conjuncts of a cyclic conjuncts
			// also cyclic.

			cyclic = true
			n.hasCycle = true

			// As the adt.EvaluatingArcs mechanism bypasses the self-reference
			// mechanism, we need to separately keep track of it here.
			// If this (originally) is a self-reference node, adding them
			// will result in recursively adding the same reference. For this
			// we also mark the node as evaluating.
			if arc.SelfCount > 0 {
				break outer
			}

			// This count is added for values that are directly added below.
			// The count is handled separately for delayed values.
			arc.SelfCount++
			defer func() { arc.SelfCount-- }()
		}

		if isDef(v.Expr()) { // should test whether closed, not isDef?
			c := closedInfo(n.node)
			closeID = c.InsertDefinition(v.CloseID, x)
			n.needClose = true // TODO: is this still necessary?
		}

		// TODO: uncommenting the following almost works, but causes some
		// faulty results in complex cycle handling between disjunctions.
		// The reason is that disjunctions must be eliminated if checks in
		// values on which they depend fail.
		ctx.Unify(ctx, arc, adt.Finalized)

		for _, c := range arc.Conjuncts {
			c = updateCyclic(c, cyclic, arc)
			c.CloseID = closeID
			n.addExprConjunct(c)
		}

	case adt.Evaluator:
		// adt.Interpolation, adt.UnaryExpr, adt.BinaryExpr, adt.CallExpr
		// Could be unify?
		val, complete := ctx.Evaluate(v.Env, v.Expr())
		if !complete {
			n.exprs = append(n.exprs, v)
			break
		}

		if v, ok := val.(*adt.Vertex); ok {
			// Handle generated disjunctions (as in the 'or' builtin).
			// These come as a Vertex, but should not be added as a value.
			b, ok := v.Value.(*adt.Bottom)
			if ok && b.IsIncomplete() && len(v.Conjuncts) > 0 {
				for _, c := range v.Conjuncts {
					c.CloseID = closeID
					n.addExprConjunct(c)
				}
				break
			}
		}

		// TODO: insert in vertex as well
		n.addValueConjunct(v.Env, val, closeID)

	default:
		panic(fmt.Sprintf("unknown expression of type %T", x))
	}
}

// isDef reports whether an expressions is a reference that references a
// definition anywhere in its selection path.
//
// TODO(performance): this should be merged with resolve(). But for now keeping
// this code isolated makes it easier to see what it is for.
func isDef(x adt.Expr) bool {
	switch r := x.(type) {
	case *adt.FieldReference:
		return r.Label.IsDef()

	case *adt.SelectorExpr:
		if r.Sel.IsDef() {
			return true
		}
		return isDef(r.X)

	case *adt.IndexExpr:
		return isDef(r.X)
	}
	return false
}

// updateCyclicStatus looks for proof of non-cyclic conjuncts to override
// a structural cycle.
func (n *nodeContext) updateCyclicStatus(env *adt.Environment) {
	if env == nil || !env.Cyclic {
		// fmt.Printf("%p -- %d hasNonCycle\n", n.node, n.node.Label)
		n.hasNonCycle = true
	}
}

func updateCyclic(c adt.Conjunct, cyclic bool, deref *adt.Vertex) adt.Conjunct {
	env := c.Env
	switch {
	case env == nil:
		if !cyclic && deref == nil {
			return c
		}
		env = &adt.Environment{Cyclic: cyclic}
	case deref == nil && env.Cyclic == cyclic:
		return c
	default:
		// The conjunct may still be in use in other fields, so we should
		// make a new copy to mark Cyclic only for this case.
		e := *env
		e.Cyclic = e.Cyclic || cyclic
		env = &e
	}
	if deref != nil {
		env.Deref = append(env.Deref, deref)
		env.Cycles = append(env.Cycles, deref)
	}
	return adt.MakeConjunct(env, c.Expr(), c.CloseID)
}

func (n *nodeContext) addValueConjunct(env *adt.Environment, v adt.Value, id adt.ID) {
	n.updateCyclicStatus(env)

	ctx := n.ctx

	if x, ok := v.(*adt.Vertex); ok {
		if m, ok := x.Value.(*adt.StructMarker); ok {
			n.aStruct = x
			n.aStructID = id
			if m.NeedClose {
				ci := closedInfo(n.node)
				ci.isClosed = true // TODO: remove
				id = ci.InsertDefinition(id, x)
				ci.Canopy[id].IsClosed = true
				ci.Canopy[id].IsDef = false
			}
		}

		cyclic := env != nil && env.Cyclic

		if !x.IsData() && len(x.Conjuncts) > 0 {
			if isComplexStruct(x) {
				closedInfo(n.node).InsertSubtree(id, n, x, cyclic)
				return
			}

			for _, c := range x.Conjuncts {
				c = updateCyclic(c, cyclic, nil)
				c.CloseID = id
				n.addExprConjunct(c) // TODO: Pass from eval
			}
			return
		}

		// TODO: evaluate value?
		switch v := x.Value.(type) {
		case *adt.ListMarker:
			n.vLists = append(n.vLists, x)
			return

		case *adt.StructMarker:
			for _, a := range x.Arcs {
				c := adt.MakeConjunct(nil, a, id)
				c = updateCyclic(c, cyclic, nil)
				n.insertField(a.Label, c)
			}

		default:
			n.addValueConjunct(env, v, id)

			for _, a := range x.Arcs {
				// TODO(errors): report error when this is a regular field.
				n.insertField(a.Label, adt.MakeConjunct(nil, a, id))
				// sub, _ := n.node.GetArc(a.Label)
				// sub.Add(a)
			}
		}

		return
		// TODO: Use the Closer to close other fields as well?
	}

	if b, ok := v.(*adt.Bottom); ok {
		n.addBottom(b)
		return
	}

	if !n.updateNodeType(v.Kind(), v, id) {
		return
	}

	switch x := v.(type) {
	case *adt.Disjunction:
		n.addDisjunctionValue(env, x, id)

	case *adt.Conjunction:
		for _, x := range x.Values {
			n.addValueConjunct(env, x, id)
		}

	case *adt.Top:
		n.hasTop = true
		// TODO: Is this correct. Needed for elipsis, but not sure for others.
		// n.optionals = append(n.optionals, fieldSet{env: env, id: id, isOpen: true})
		if a, _ := n.node.Closed.(*acceptor); a != nil {
			// Embedding top indicates that now all possible values are allowed
			// and that we should no longer enforce closedness within this
			// conjunct.
			a.node(id).IsDef = false
		}

	case *adt.BasicType:
		// handled above

	case *adt.BoundValue:
		switch x.Op {
		case adt.LessThanOp, adt.LessEqualOp:
			if y := n.upperBound; y != nil {
				n.upperBound = nil
				v := adt.SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.upperBound)
					err.AddPosition(closedInfo(n.node).node(id).Src)
				}
				n.addValueConjunct(env, v, id)
				return
			}
			n.upperBound = x

		case adt.GreaterThanOp, adt.GreaterEqualOp:
			if y := n.lowerBound; y != nil {
				n.lowerBound = nil
				v := adt.SimplifyBounds(ctx, n.kind, x, y)
				if err := valueError(v); err != nil {
					err.AddPosition(v)
					err.AddPosition(n.lowerBound)
					err.AddPosition(closedInfo(n.node).node(id).Src)
				}
				n.addValueConjunct(env, v, id)
				return
			}
			n.lowerBound = x

		case adt.EqualOp, adt.NotEqualOp, adt.MatchOp, adt.NotMatchOp:
			// This check serves as simplifier, but also to remove duplicates.
			k := 0
			match := false
			for _, c := range n.checks {
				if y, ok := c.(*adt.BoundValue); ok {
					switch z := adt.SimplifyBounds(ctx, n.kind, x, y); {
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

	case adt.Validator:
		// This check serves as simplifier, but also to remove duplicates.
		for i, y := range n.checks {
			if b := adt.SimplifyValidator(ctx, x, y); b != nil {
				n.checks[i] = b
				return
			}
		}
		n.checks = append(n.checks, x)

	case *adt.Vertex:
	// handled above.

	case adt.Value: // *NullLit, *BoolLit, *NumLit, *StringLit, *BytesLit
		if y := n.scalar; y != nil {
			if b, ok := adt.BinOp(ctx, adt.EqualOp, x, y).(*adt.Bool); !ok || !b.B {
				n.addConflict(x, y, x.Kind(), y.Kind(), n.scalarID, id)
			}
			// TODO: do we need to explicitly add again?
			// n.scalar = nil
			// n.addValueConjunct(c, adt.BinOp(c, adt.EqualOp, x, y))
			break
		}
		n.scalar = x
		n.scalarID = id

	default:
		panic(fmt.Sprintf("unknown value type %T", x))
	}

	if n.lowerBound != nil && n.upperBound != nil {
		if u := adt.SimplifyBounds(ctx, n.kind, n.lowerBound, n.upperBound); u != nil {
			if err := valueError(u); err != nil {
				err.AddPosition(n.lowerBound)
				err.AddPosition(n.upperBound)
				err.AddPosition(closedInfo(n.node).node(id).Src)
			}
			n.lowerBound = nil
			n.upperBound = nil
			n.addValueConjunct(env, u, id)
		}
	}
}

func valueError(v adt.Value) *adt.ValueError {
	if v == nil {
		return nil
	}
	b, _ := v.(*adt.Bottom)
	if b == nil {
		return nil
	}
	err, _ := b.Err.(*adt.ValueError)
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
	env *adt.Environment,
	s *adt.StructLit,
	closeID adt.ID) {

	n.updateCyclicStatus(env) // to handle empty structs.

	ctx := n.ctx
	n.node.AddStructs(s)

	// NOTE: This is a crucial point in the code:
	// Unification derferencing happens here. The child nodes are set to
	// an Environment linked to the current node. Together with the De Bruijn
	// indices, this determines to which Vertex a reference resolves.

	// TODO(perf): consider using environment cache:
	// var childEnv *adt.Environment
	// for _, s := range n.nodeCache.sub {
	// 	if s.Up == env {
	// 		childEnv = s
	// 	}
	// }
	childEnv := &adt.Environment{
		Up:     env,
		Vertex: n.node,
	}
	if env != nil {
		childEnv.Cyclic = env.Cyclic
		childEnv.Deref = env.Deref
	}

	var hasOther, hasBulk adt.Node
	hasEmbed := false

	opt := fieldSet{pos: s, env: childEnv, id: closeID}

	for _, d := range s.Decls {
		switch x := d.(type) {
		case *adt.Field:
			opt.MarkField(ctx, x)
			// handle in next iteration.

		case *adt.OptionalField:
			if x.Label.IsString() {
				n.aStruct = s
				n.aStructID = closeID
			}
			opt.AddOptional(ctx, x)

		case *adt.DynamicField:
			n.aStruct = s
			n.aStructID = closeID
			hasOther = x
			n.dynamicFields = append(n.dynamicFields, envDynamic{childEnv, x, closeID})
			opt.AddDynamic(ctx, childEnv, x)

		case *adt.ForClause:
			hasOther = x
			n.forClauses = append(n.forClauses, envYield{childEnv, x, closeID})

		case adt.Yielder:
			hasOther = x
			n.ifClauses = append(n.ifClauses, envYield{childEnv, x, closeID})

		case adt.Expr:
			hasEmbed = true
			hasOther = x

			// add embedding to optional

			// TODO(perf): only do this if addExprConjunct below will result in
			// a fieldSet. Otherwise the entry will just be removed next.
			id := closedInfo(n.node).InsertEmbed(closeID, x)

			// push and opo embedding type.
			n.addExprConjunct(adt.MakeConjunct(childEnv, x, id))

		case *adt.BulkOptionalField:
			n.aStruct = s
			n.aStructID = closeID
			hasBulk = x
			opt.AddBulk(ctx, x)

		case *adt.Ellipsis:
			n.aStruct = s
			n.aStructID = closeID
			hasBulk = x
			opt.AddEllipsis(ctx, x)

		default:
			panic("unreachable")
		}
	}

	if hasBulk != nil && hasOther != nil {
		n.addErr(ctx.Newf("cannot mix bulk optional fields with dynamic fields, embeddings, or comprehensions within the same struct"))
	}

	if !hasEmbed {
		n.aStruct = s
		n.aStructID = closeID
	}

	// Apply existing fields
	for _, arc := range n.node.Arcs {
		// Reuse adt.Acceptor interface.
		opt.MatchAndInsert(ctx, arc)
	}

	closedInfo(n.node).insertFieldSet(closeID, &opt)

	for _, d := range s.Decls {
		switch x := d.(type) {
		case *adt.Field:
			if x.Label.IsString() {
				n.aStruct = s
				n.aStructID = closeID
			}
			n.insertField(x.Label, adt.MakeConjunct(childEnv, x, closeID))
		}
	}
}

func (n *nodeContext) insertField(f adt.Feature, x adt.Conjunct) *adt.Vertex {
	ctx := n.ctx
	arc, isNew := n.node.GetArc(f)

	// TODO: disallow adding conjuncts when cache set?
	arc.AddConjunct(x)

	if isNew {
		closedInfo(n.node).visitAllFieldSets(func(o *fieldSet) {
			o.MatchAndInsert(ctx, arc)
		})
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
	if n.done() {
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
		n.addExprConjunct(x)

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
		var f adt.Feature
		v, complete := ctx.Evaluate(d.env, d.field.Key)
		if !complete {
			a[k] = d
			k++
			continue
		}
		if b, _ := v.(*adt.Bottom); b != nil {
			n.addValueConjunct(nil, b, d.id)
			continue
		}
		f = ctx.Label(v)
		n.insertField(f, adt.MakeConjunct(d.env, d.field, d.id))
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
		env *adt.Environment
		s   *adt.StructLit
	}
	var sa []envStruct
	f := func(env *adt.Environment, st *adt.StructLit) {
		sa = append(sa, envStruct{env, st})
	}

	k := 0
	for i := 0; i < len(*all); i++ {
		d := (*all)[i]
		sa = sa[:0]

		if err := ctx.Yield(d.env, d.yield, f); err != nil {
			if err.IsIncomplete() {
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

	*all = (*all)[:k]
	return k < len(*all)
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
func (n *nodeContext) addLists(c *adt.OpContext) (oneOfTheLists adt.Expr, anID adt.ID) {
	if len(n.lists) == 0 && len(n.vLists) == 0 {
		return nil, 0
	}

	isOpen := true
	max := 0
	var maxNode adt.Expr

	if m, ok := n.node.Value.(*adt.ListMarker); ok {
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
				n.insertField(a.Label, adt.MakeConjunct(nil, a.Value, 0))
				continue
			}
			for _, c := range a.Conjuncts {
				n.insertField(a.Label, c)
			}
		}
	}

outer:
	for i, l := range n.lists {
		oneOfTheLists = l.list
		anID = l.id

		n.updateCyclicStatus(l.env)

		index := int64(0)
		hasComprehension := false
		for j, elem := range l.list.Elems {
			switch x := elem.(type) {
			case adt.Yielder:
				err := c.Yield(l.env, x, func(e *adt.Environment, st *adt.StructLit) {
					label, err := adt.MakeLabel(x.Source(), index, adt.IntLabel)
					n.addErr(err)
					index++
					n.insertField(label, adt.MakeConjunct(e, st, l.id))
				})
				hasComprehension = true
				if err != nil && !err.IsIncomplete() {
					n.addBottom(err)
				}

			case *adt.Ellipsis:
				if j != len(l.list.Elems)-1 {
					n.addErr(c.Newf("ellipsis must be last element in list"))
				}

				n.lists[i].elipsis = x

			default:
				label, err := adt.MakeLabel(x.Source(), index, adt.IntLabel)
				n.addErr(err)
				index++ // TODO: don't use insertField.
				n.insertField(label, adt.MakeConjunct(l.env, x, l.id))
			}

			// Terminate early n case of runaway comprehension.
			if !isOpen && int(index) > max {
				n.invalidListLength(max, int(index), maxNode, l.list)
				continue outer
			}
		}

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
		a, _ := l.Closed.(*acceptor)
		if a == nil {
			continue
		}

		newElems := l.Elems()
		if len(newElems) >= len(elems) {
			continue // error generated earlier, if applicable.
		}

		// TODO: why not necessary?
		// n.optionals = append(n.optionals, a.fields...)

		for _, arc := range elems[len(newElems):] {
			l.MatchAndInsert(c, arc)
		}
	}

	for _, l := range n.lists {
		if l.elipsis == nil {
			continue
		}

		f := &fieldSet{pos: l.list, env: l.env, id: l.id}
		f.AddEllipsis(c, l.elipsis)

		closedInfo(n.node).insertFieldSet(l.id, f)

		for _, arc := range elems[l.n:] {
			f.MatchAndInsert(c, arc)
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

	ci := closedInfo(n.node)
	ci.isList = true
	ci.openList = isOpen

	if m, ok := n.node.Value.(*adt.ListMarker); !ok {
		n.node.SetValue(c, adt.Partial, &adt.ListMarker{
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

func (n *nodeContext) invalidListLength(na, nb int, a, b adt.Expr) {
	n.addErr(n.ctx.Newf("incompatible list lengths (%d and %d)", na, nb))
}
