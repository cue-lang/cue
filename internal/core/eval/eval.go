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

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
)

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
	r       adt.Runtime
	index   adt.StringIndexer
	closeID uint32
}

func (e *Evaluator) nextID() uint32 {
	e.closeID++
	return e.closeID
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
		s := e.evalVertex(c, v, adt.Partial)

		if d := s.disjunct; d != nil && len(d.Values) > 1 && d.NumDefaults != 1 {
			v.Value = d
			v.Arcs = nil
			v.Structs = nil // TODO: maybe not do this.
			// The conjuncts will have too much information. Better have no
			// information than incorrect information.
			for _, d := range d.Values {
				d.Conjuncts = nil
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

	n := e.evalVertex(c, v, state)

	switch d := n.disjunct; {
	case d != nil && len(d.Values) == 1:
		*v = *(d.Values[0])

	case d != nil && len(d.Values) > 0:
		v.Value = d
		v.Arcs = nil
		v.Structs = nil
		// The conjuncts will have too much information. Better have no
		// information than incorrect information.
		for _, d := range d.Values {
			d.Conjuncts = nil
		}

	default:
		if r := n.result(); r.Value != nil {
			*v = *r
		}
	}

	// Else set it to something.

	if v.Value == nil {
		panic("errer")
	}

	// Check whether result is done.
}

// evalVertex computes the vertex results. The state indicates the minimum
// status to which this vertex should be evaluated. It should be either
// adt.Finalized or adt.Partial.
func (e *Evaluator) evalVertex(c *adt.OpContext, v *adt.Vertex, state adt.VertexStatus) *nodeShared {
	// fmt.Println(debug.NodeString(c.StringIndexer, v, nil))
	shared := &nodeShared{
		ctx:   c,
		eval:  e,
		node:  v,
		stack: nil, // silence linter
	}
	saved := *v

	for i := 0; ; i++ {

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
		v.UpdateStatus(adt.Evaluating)

		// If the result is a struct, it needs to be closed if:
		//   1) this node introduces a definition
		//   2) this node is a child of a node that introduces a definition,
		//      recursively.
		//   3) this node embeds a closed struct.
		needClose := v.Label.IsDef()

		n := &nodeContext{
			kind:       adt.TopKind,
			nodeShared: shared,
			needClose:  needClose,

			// These get cleared upon proof to the contrary.
			// isDefault: true,
			isFinal: true,
		}

		closeID := uint32(0)

		for _, x := range v.Conjuncts {
			closeID := closeID
			// TODO: needed for reentrancy. Investigate usefulness for cycle
			// detection.
			if x.Env != nil && x.Env.CloseID != 0 {
				closeID = x.Env.CloseID
			}
			n.addExprConjunct(x, closeID, true)
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

	// Use maybeSetCache for cycle breaking
	for n.maybeSetCache(); n.expandOne(); n.maybeSetCache() {
	}

	// TODO: preparation for association lists:
	// We assume that association types may not be created dynamically for now.
	// So we add lists
	n.addLists(ctx)

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
		kind := n.kind
		markStruct := false
		if n.isStruct {
			if kind != 0 && kind&adt.StructKind == 0 {
				n.node.Value = &adt.Bottom{
					Err: errors.Newf(token.NoPos,
						"conflicting values struct and %s", n.kind),
				}
			}
			markStruct = true
		} else if len(n.node.Structs) > 0 {
			markStruct = kind&adt.StructKind != 0 && !n.hasTop
		}
		if v == nil && markStruct {
			kind = adt.StructKind
			n.node.Value = &adt.StructMarker{}
			v = n.node
		}
		if v != nil && adt.IsConcrete(v) {
			if n.scalar != nil {
				kind = n.scalar.Kind()
			}
			if v.Kind()&^kind != 0 {
				p := token.NoPos
				if src := v.Source(); src != nil {
					p = src.Pos()
				}
				n.addErr(errors.Newf(p,
					// TODO(err): position of all value types.
					"conflicting types",
				))
			}
			if n.lowerBound != nil {
				if b := ctx.Validate(n.lowerBound, v); b != nil {
					n.addBottom(b)
				}
			}
			if n.upperBound != nil {
				if b := ctx.Validate(n.upperBound, v); b != nil {
					n.addBottom(b)
				}
			}
			for _, v := range n.checks {
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
					n.addErr(errors.Newf(token.NoPos,
						// TODO(err): add positions for list and arc definitions.
						"list may not have regular fields"))
				}
			}

			// case !isStruct(n.node) && v.Kind() != adt.BottomKind:
			// 	for _, a := range n.node.Arcs {
			// 		if a.Label.IsRegular() {
			// 			n.addErr(errors.Newf(token.NoPos,
			// 				// TODO(err): add positions of non-struct values and arcs.
			// 				"cannot combine scalar values with arcs"))
			// 		}
			// 	}
		}
	}

	var c *CloseDef
	if a, _ := n.node.Closed.(*acceptor); a != nil {
		c = a.tree
		n.needClose = n.needClose || a.isClosed
	}

	updated := updateClosed(c, n.replace)
	if updated == nil && n.needClose {
		updated = &CloseDef{}
	}

	// TODO retrieve from env.

	if err := n.getErr(); err != nil {
		if b, _ := n.node.Value.(*adt.Bottom); b != nil {
			err = adt.CombineErrors(nil, b, err)
		}
		n.node.Value = err
		// TODO: add return: if evaluation of arcs is important it can be done
		// later. Logically we're done.
	}

	m := &acceptor{
		tree:     updated,
		fields:   n.optionals,
		isClosed: n.needClose,
		openList: n.openList,
		isList:   n.node.IsList(),
	}
	if updated != nil || len(n.optionals) > 0 {
		n.node.Closed = m
	}

	// Visit arcs recursively to validate and compute error.
	for _, a := range n.node.Arcs {
		if updated != nil {
			a.Closed = m
		}
		if updated != nil && m.isClosed {
			if err := m.verifyArcAllowed(n.ctx, a.Label); err != nil {
				n.node.Value = err
			}
			// TODO: use continue to not process already failed fields,
			// or at least don't record recursive error.
			// continue
		}
		// Call UpdateStatus here to be absolutely sure the status is set
		// correctly and that we are not regressing.
		n.node.UpdateStatus(adt.EvaluatingArcs)
		n.eval.Unify(ctx, a, adt.Finalized)
		if err, _ := a.Value.(*adt.Bottom); err != nil {
			n.node.AddChildError(err)
		}
	}

	n.node.UpdateStatus(adt.Finalized)
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

	// Current value (may be under construction)
	scalar adt.Value // TODO: use Value in node.

	// Concrete conjuncts
	kind       adt.Kind
	lowerBound *adt.BoundValue // > or >=
	upperBound *adt.BoundValue // < or <=
	checks     []adt.Validator // BuiltinValidator, other bound values.
	errs       *adt.Bottom
	incomplete *adt.Bottom

	// Struct information
	dynamicFields []envDynamic
	ifClauses     []envYield
	forClauses    []envYield
	optionals     []fieldSet // env + field
	// NeedClose:
	// - node starts definition
	// - embeds a definition
	// - parent node is closing
	needClose bool
	openList  bool
	isStruct  bool
	hasTop    bool
	newClose  *CloseDef
	// closeID   uint32 // from parent, or if not exist, new if introducing a def.
	replace map[uint32]*CloseDef

	// Expression conjuncts
	lists  []envList
	vLists []*adt.Vertex
	exprs  []conjunct

	// Disjunction handling
	disjunctions []envDisjunct
	defaultMode  defaultMode
	isFinal      bool
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

		// TODO: Change to isStruct?
		if len(n.node.Structs) > 0 {
			// n.isStruct = true
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

type conjunct struct {
	adt.Conjunct
	closeID uint32
	top     bool
}

type envDynamic struct {
	env   *adt.Environment
	field *adt.DynamicField
}

type envYield struct {
	env   *adt.Environment
	yield adt.Yielder
}

type envList struct {
	env     *adt.Environment
	list    *adt.ListLit
	n       int64 // recorded length after evaluator
	elipsis *adt.Ellipsis
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
func (n *nodeContext) addExprConjunct(v adt.Conjunct, def uint32, top bool) {
	env := v.Env
	if env != nil && env.CloseID != def {
		e := *env
		e.CloseID = def
		env = &e
	}
	switch x := v.Expr().(type) {
	case adt.Value:
		n.addValueConjunct(env, x)

	case *adt.BinaryExpr:
		if x.Op == adt.AndOp {
			n.addExprConjunct(adt.MakeConjunct(env, x.X), def, false)
			n.addExprConjunct(adt.MakeConjunct(env, x.Y), def, false)
		} else {
			n.evalExpr(v, def, top)
		}

	case *adt.StructLit:
		n.addStruct(env, x, def, top)

	case *adt.ListLit:
		n.lists = append(n.lists, envList{env: env, list: x})

	case *adt.DisjunctionExpr:
		if n.disjunctions != nil {
			_ = n.disjunctions
		}
		n.addDisjunction(env, x, def, top)

	default:
		// Must be Resolver or Evaluator.
		n.evalExpr(v, def, top)
	}

	if top {
		n.updateReplace(v.Env)
	}
}

// evalExpr is only called by addExprConjunct.
func (n *nodeContext) evalExpr(v adt.Conjunct, closeID uint32, top bool) {
	// Require an Environment.
	ctx := n.ctx

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
			n.exprs = append(n.exprs, conjunct{v, closeID, top})
			break
		}

		// If this is a cycle error, we have reached a fixed point and adding
		// conjuncts at this point will not change the value. Also, continuing
		// to pursue this value will result in an infinite loop.
		//
		// TODO: add a mechanism so that the computation will only have to be
		// one once?
		if isEvaluating(arc) {
			break
		}

		// TODO: detect structural cycles here. A structural cycle can occur
		// if it is not a reference cycle, but refers to a parent. node.
		// This should only be allowed if it is unified with a finite structure.

		if arc.Label.IsDef() {
			n.insertClosed(arc)
		} else {
			for _, a := range arc.Conjuncts {
				n.addExprConjunct(a, closeID, top)
			}
		}

	case adt.Evaluator:
		// adt.Interpolation, adt.UnaryExpr, adt.BinaryExpr, adt.CallExpr
		val, complete := ctx.Evaluate(v.Env, v.Expr())
		if !complete {
			n.exprs = append(n.exprs, conjunct{v, closeID, top})
			break
		}

		if v, ok := val.(*adt.Vertex); ok {
			// Handle generated disjunctions (as in the 'or' builtin).
			// These come as a Vertex, but should not be added as a value.
			b, ok := v.Value.(*adt.Bottom)
			if ok && b.IsIncomplete() && len(v.Conjuncts) > 0 {
				for _, c := range v.Conjuncts {
					n.addExprConjunct(c, closeID, top)
				}
				break
			}
		}

		// TODO: insert in vertex as well
		n.addValueConjunct(v.Env, val)

	default:
		panic(fmt.Sprintf("unknown expression of type %T", x))
	}
}

func (n *nodeContext) insertClosed(arc *adt.Vertex) {
	id := n.eval.nextID()
	n.needClose = true

	current := n.newClose
	n.newClose = nil

	for _, a := range arc.Conjuncts {
		n.addExprConjunct(a, id, false)
	}

	current, n.newClose = n.newClose, current

	if current == nil {
		current = &CloseDef{ID: id}
	}
	n.addAnd(current)
}

func (n *nodeContext) addValueConjunct(env *adt.Environment, v adt.Value) {
	if x, ok := v.(*adt.Vertex); ok {
		needClose := false
		if isStruct(x) {
			n.isStruct = true
			// TODO: find better way to mark as struct.
			// For instance, we may want to add a faux
			// Structlit for topological sort.
			// return

			if x.IsClosed(n.ctx) {
				needClose = true
			}

			n.node.AddStructs(x.Structs...)
		}

		if len(x.Conjuncts) > 0 {
			if needClose {
				n.insertClosed(x)
				return
			}
			for _, c := range x.Conjuncts {
				n.addExprConjunct(c, 0, false) // Pass from eval
			}
			return
		}

		if x.IsList() {
			n.vLists = append(n.vLists, x)
			return
		}

		// TODO: evaluate value?
		switch v := x.Value.(type) {
		case *adt.ListMarker:
			panic("unreachable")

		case *adt.StructMarker:
			for _, a := range x.Arcs {
				// TODO, insert here as
				n.insertField(a.Label, adt.MakeConjunct(nil, a))
				// sub, _ := n.node.GetArc(a.Label)
				// sub.Add(a)
			}

		default:
			n.addValueConjunct(env, v)

			for _, a := range x.Arcs {
				// TODO, insert here as
				n.insertField(a.Label, adt.MakeConjunct(nil, a))
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

	ctx := n.ctx
	kind := n.kind & v.Kind()
	if kind == adt.BottomKind {
		// TODO: how to get other conflicting values?
		n.addErr(errors.Newf(token.NoPos,
			"invalid value %s (mismatched types %s and %s)",
			ctx.Str(v), v.Kind(), n.kind))
		return
	}
	n.kind = kind

	switch x := v.(type) {
	case *adt.Disjunction:
		n.addDisjunctionValue(env, x, 0, true)

	case *adt.Conjunction:
		for _, x := range x.Values {
			n.addValueConjunct(env, x)
		}

	case *adt.Top:
		n.hasTop = true
		// TODO: Is this correct. Needed for elipsis, but not sure for others.
		n.optionals = append(n.optionals, fieldSet{env: env, isOpen: true})

	case *adt.BasicType:

	case *adt.BoundValue:
		switch x.Op {
		case adt.LessThanOp, adt.LessEqualOp:
			if y := n.upperBound; y != nil {
				n.upperBound = nil
				n.addValueConjunct(env, adt.SimplifyBounds(ctx, n.kind, x, y))
				return
			}
			n.upperBound = x

		case adt.GreaterThanOp, adt.GreaterEqualOp:
			if y := n.lowerBound; y != nil {
				n.lowerBound = nil
				n.addValueConjunct(env, adt.SimplifyBounds(ctx, n.kind, x, y))
				return
			}
			n.lowerBound = x

		case adt.EqualOp, adt.NotEqualOp, adt.MatchOp, adt.NotMatchOp:
			n.checks = append(n.checks, x)
			return
		}

	case adt.Validator:
		n.checks = append(n.checks, x)

	case *adt.Vertex:
	// handled above.

	case adt.Value: // *NullLit, *BoolLit, *NumLit, *StringLit, *BytesLit
		if y := n.scalar; y != nil {
			if b, ok := adt.BinOp(ctx, adt.EqualOp, x, y).(*adt.Bool); !ok || !b.B {
				n.addErr(errors.Newf(ctx.Pos(), "incompatible values %s and %s", ctx.Str(x), ctx.Str(y)))
			}
			// TODO: do we need to explicitly add again?
			// n.scalar = nil
			// n.addValueConjunct(c, adt.BinOp(c, adt.EqualOp, x, y))
			break
		}
		n.scalar = x

	default:
		panic(fmt.Sprintf("unknown value type %T", x))
	}

	if n.lowerBound != nil && n.upperBound != nil {
		if u := adt.SimplifyBounds(ctx, n.kind, n.lowerBound, n.upperBound); u != nil {
			n.lowerBound = nil
			n.upperBound = nil
			n.addValueConjunct(env, u)
		}
	}
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
	newDef uint32,
	top bool) {

	ctx := n.ctx
	n.node.AddStructs(s)

	// Inherit closeID from environment, unless this is a new definition.
	closeID := newDef
	if closeID == 0 && env != nil {
		closeID = env.CloseID
	}

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
		Up:      env,
		Vertex:  n.node,
		CloseID: closeID,
	}

	var hasOther, hasBulk adt.Node

	opt := fieldSet{env: childEnv}

	for _, d := range s.Decls {
		switch x := d.(type) {
		case *adt.Field:
			opt.MarkField(ctx, x)
			// handle in next iteration.

		case *adt.OptionalField:
			opt.AddOptional(ctx, x)

		case *adt.DynamicField:
			hasOther = x
			n.dynamicFields = append(n.dynamicFields, envDynamic{childEnv, x})
			opt.AddDynamic(ctx, childEnv, x)

		case *adt.ForClause:
			hasOther = x
			n.forClauses = append(n.forClauses, envYield{childEnv, x})

		case adt.Yielder:
			hasOther = x
			n.ifClauses = append(n.ifClauses, envYield{childEnv, x})

		case adt.Expr:
			// push and opo embedding type.
			id := n.eval.nextID()

			current := n.newClose
			n.newClose = nil

			hasOther = x
			n.addExprConjunct(adt.MakeConjunct(childEnv, x), id, false)

			current, n.newClose = n.newClose, current

			if current == nil {
				current = &CloseDef{ID: id} // TODO: isClosed?
			} else {
				// n.needClose = true
			}
			n.addOr(closeID, current)

		case *adt.BulkOptionalField:
			hasBulk = x
			opt.AddBulk(ctx, x)

		case *adt.Ellipsis:
			hasBulk = x
			opt.AddEllipsis(ctx, x)

		default:
			panic("unreachable")
		}
	}

	if hasBulk != nil && hasOther != nil {
		n.addErr(errors.Newf(token.NoPos, "cannot mix bulk optional fields with dynamic fields, embeddings, or comprehensions within the same struct"))
	}

	// Apply existing fields
	for _, arc := range n.node.Arcs {
		// Reuse adt.Acceptor interface.
		opt.MatchAndInsert(ctx, arc)
	}

	n.optionals = append(n.optionals, opt)

	for _, d := range s.Decls {
		switch x := d.(type) {
		case *adt.Field:
			n.insertField(x.Label, adt.MakeConjunct(childEnv, x))
		}
	}
}

func (n *nodeContext) insertField(f adt.Feature, x adt.Conjunct) *adt.Vertex {
	ctx := n.ctx
	arc, isNew := n.node.GetArc(f)

	if f.IsString() {
		n.isStruct = true
	}

	// TODO: disallow adding conjuncts when cache set?
	arc.AddConjunct(x)

	if isNew {
		for _, o := range n.optionals {
			o.MatchAndInsert(ctx, arc)
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
// TODO(error): detect when a field is added to a struct that is already used
// in a for clause.
func (n *nodeContext) expandOne() (done bool) {
	if n.done() {
		return false
	}

	var progress bool

	if progress = n.injectDynamic(); progress {
		return true
	}

	if n.ifClauses, progress = n.injectEmbedded(n.ifClauses); progress {
		return true
	}

	if n.forClauses, progress = n.injectEmbedded(n.forClauses); progress {
		return true
	}

	// Do expressions after comprehensions, as comprehensions can never
	// refer to embedded scalars, whereas expressions may refer to generated
	// fields if we were to allow attributes to be defined alongside
	// scalars.
	exprs := n.exprs
	n.exprs = n.exprs[:0]
	for _, x := range exprs {
		n.addExprConjunct(x.Conjunct, x.closeID, x.top)

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
			n.addValueConjunct(nil, b)
			continue
		}
		f = ctx.Label(v)
		n.insertField(f, adt.MakeConjunct(d.env, d.field))
	}

	progress = k < len(n.dynamicFields)

	n.dynamicFields = a[:k]

	return progress
}

// injectEmbedded evaluates and inserts embeddings. It first evaluates all
// embeddings before inserting the results to ensure that the order of
// evaluation does not matter.
func (n *nodeContext) injectEmbedded(all []envYield) (a []envYield, progress bool) {
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
	for _, d := range all {
		sa = sa[:0]

		if err := ctx.Yield(d.env, d.yield, f); err != nil {
			if err.IsIncomplete() {
				all[k] = d
				k++
			} else {
				// continue to collect other errors.
				n.addBottom(err)
			}
			continue
		}

		for _, st := range sa {
			n.addStruct(st.env, st.s, 0, true)
		}
	}

	return all[:k], k < len(all)
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
func (n *nodeContext) addLists(c *adt.OpContext) {
	if len(n.lists) == 0 && len(n.vLists) == 0 {
		return
	}

	for _, a := range n.node.Arcs {
		if t := a.Label.Typ(); t == adt.StringLabel {
			n.addErr(errors.Newf(token.NoPos, "conflicting types list and struct"))
		}
	}

	// fmt.Println(len(n.lists), "ELNE")

	isOpen := true
	max := 0
	var maxNode adt.Expr

	for _, l := range n.vLists {
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
				n.insertField(a.Label, adt.MakeConjunct(nil, a.Value))
				continue
			}
			for _, c := range a.Conjuncts {
				n.insertField(a.Label, c)
			}
		}
	}

outer:
	for i, l := range n.lists {
		index := int64(0)
		hasComprehension := false
		for j, elem := range l.list.Elems {
			switch x := elem.(type) {
			case adt.Yielder:
				err := c.Yield(l.env, x, func(e *adt.Environment, st *adt.StructLit) {
					label, err := adt.MakeLabel(x.Source(), index, adt.IntLabel)
					n.addErr(err)
					index++
					n.insertField(label, adt.MakeConjunct(e, st))
				})
				hasComprehension = true
				if err.IsIncomplete() {

				}

			case *adt.Ellipsis:
				if j != len(l.list.Elems)-1 {
					n.addErr(errors.Newf(token.NoPos,
						"ellipsis must be last element in list"))
				}

				n.lists[i].elipsis = x

			default:
				label, err := adt.MakeLabel(x.Source(), index, adt.IntLabel)
				n.addErr(err)
				index++ // TODO: don't use insertField.
				n.insertField(label, adt.MakeConjunct(l.env, x))
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

		n.optionals = append(n.optionals, a.fields...)

		for _, arc := range elems[len(newElems):] {
			l.MatchAndInsert(c, arc)
		}
	}

	for _, l := range n.lists {
		if l.elipsis == nil {
			continue
		}

		f := fieldSet{env: l.env}
		f.AddEllipsis(c, l.elipsis)

		n.optionals = append(n.optionals, f)

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

	n.openList = isOpen

	n.node.SetValue(c, adt.Partial, &adt.ListMarker{
		Src:    ast.NewBinExpr(token.AND, sources...),
		IsOpen: isOpen,
	})
}

func (n *nodeContext) invalidListLength(na, nb int, a, b adt.Expr) {
	n.addErr(errors.Newf(n.ctx.Pos(),
		"incompatible list lengths (%d and %d)", na, nb))
}
