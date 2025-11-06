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

package adt

// TODO:
//   - result should be nodeContext: this allows optionals info to be extracted
//     and computed.
//

import (
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/layer"
)

// TODO TODO TODO TODO TODO TODO  TODO TODO TODO  TODO TODO TODO  TODO TODO TODO
//
// - Reuse work from previous cycles. For instance, if we can guarantee that a
//   value is always correct for partial results, we can just process the arcs
//   going from Partial to Finalized, without having to reevaluate the value.
//
// - Test closedness far more thoroughly.
//

func (c *OpContext) Stats() *stats.Counts {
	return &c.stats
}

// TODO: Note: NewContext takes essentially a cue.Value. By making this
// type more central, we can perhaps avoid context creation.

// func NewContext(r Runtime, v *Vertex) *OpContext {
// 	e := NewUnifier(r)
// 	return e.NewContext(v)
// }

// evaluate returns the evaluated value associated with v. It may return a
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
func (c *OpContext) evaluate(v *Vertex, r Resolver, state Flags) Value {
	if v.isUndefined() {
		// Use node itself to allow for cycle detection.
		c.unify(v, state)

		if v.ArcType == ArcPending {
			if v.status == evaluating {
				for ; v.Parent != nil && v.ArcType == ArcPending; v = v.Parent {
				}
				err := c.Newf("cycle with field %v", r)
				b := &Bottom{
					Code: CycleError,
					Err:  err,
					Node: v,
				}
				v.setValue(c, v.status, b)
				return b
				// TODO: use this instead, as is usual for incomplete errors,
				// and also move this block one scope up to also apply to
				// defined arcs. In both cases, though, doing so results in
				// some errors to be misclassified as evaluation error.
				// c.AddBottom(b)
				// return nil
			}
			c.undefinedFieldError(v, IncompleteError)
			return nil
		}
	}

	if n := v.state; n != nil {
		n.assertInitialized()

		if n.errs != nil && !n.errs.IsIncomplete() {
			return n.errs
		}
		if n.scalar != nil && isCyclePlaceholder(v.BaseValue) {
			return n.scalar
		}
	}

	switch x := v.BaseValue.(type) {
	case *Bottom:
		if x.IsIncomplete() {
			c.AddBottom(x)
			return nil
		}
		return x

	case nil:
		if v.state != nil {
			switch x := v.state.getValidators(finalized).(type) {
			case Value:
				return x
			default:
				w := *v
				w.BaseValue = x
				return &w
			}
		}
		// This may happen if the evaluator is invoked outside of regular
		// evaluation, such as in dependency analysis.
		return nil
	}

	return v
}

// unify unifies values of a Vertex to and stores the result in the Vertex. If
// unify was called on v before it returns the cached results.
// state can be used to indicate to which extent processing should continue.
// state == finalized means it is evaluated to completion. See vertexStatus
// for more details.
func (c *OpContext) unify(v *Vertex, flags Flags) {
	v.unify(c, flags)
}

// validateValue checks collected bound validators and checks them against
// the current value. If there is no value, it sets the current value
// to these validators itself.
//
// Before it does this, it also checks whether n is of another incompatible
// type, like struct. This prevents validators from being inadvertently set.
// TODO(evalv3): optimize this function for new implementation.
func (n *nodeContext) validateValue(state vertexStatus) {
	ctx := n.ctx

	// Either set to Conjunction or error.
	// TODO: verify and simplify the below code to determine whether
	// something is a struct.
	markStruct := false
	if n.aStruct != nil {
		markStruct = true
	} else if len(n.node.Structs) > 0 {
		// TODO: do something more principled here.
		// Here we collect evidence that a value is a struct. If a struct has
		// an embedding, it may evaluate to an embedded scalar value, in which
		// case it is not a struct. Right now this is tracked at the node level,
		// but it really should be at the struct level. For instance:
		//
		// 		A: matchN(1, [>10])
		// 		A: {
		// 			if true {c: 1}
		// 		}
		//
		// Here A is marked as Top by matchN. The other struct also has an
		// embedding (the comprehension), and thus does not force it either.
		// So the resulting kind is top, not struct.
		// As an approximation, we at least mark the node as a struct if it has
		// any regular fields.
		markStruct = n.kind&StructKind != 0 && !n.hasTop
		for _, a := range n.node.Arcs {
			// TODO(spec): we generally allow optional fields alongside embedded
			// scalars. We probably should not. Either way this is not entirely
			// accurate, as a Pending arc may still be optional. We should
			// collect the arcType noted in adt.Comprehension in a nodeContext
			// as well so that we know what the potential arc of this node may
			// be.
			//
			// TODO(evalv3): even better would be to ensure that all
			// comprehensions are done before calling this.
			if a.Label.IsRegular() && a.ArcType != ArcOptional {
				markStruct = true
				break
			}
		}
	}
	v := n.node.DerefValue().Value()
	if n.node.BaseValue == nil && markStruct {
		n.node.BaseValue = &StructMarker{}
		v = n.node
	}
	if v != nil && IsConcrete(v) {
		// Also check when we already have errors as we may find more
		// serious errors and would like to know about all errors anyway.
		for _, bound := range []*BoundValue{n.lowerBound, n.upperBound} {
			if bound == nil {
				continue
			}
			c := MakeRootConjunct(nil, bound)
			if b := ctx.Validate(c, v); b != nil {
				// TODO(errors): make Validate return boolean and generate
				// optimized conflict message. Also track and inject IDs
				// to determine origin location.s
				if e, _ := b.Err.(*ValueError); e != nil {
					e.AddPosition(bound)
					e.AddPosition(v)
				}
				n.addBottom(b)
			}
		}

	} else if state == finalized {
		n.node.BaseValue = n.getValidators(finalized)
	}
}

// TODO: this is now a sentinel. Use a user-facing error that traces where
// the cycle originates.
var cycle = &Bottom{
	Err:  errors.Newf(token.NoPos, "cycle error"),
	Code: CycleError,
}

func isCyclePlaceholder(v BaseValue) bool {
	// TODO: do not mark cycle in BaseValue.
	if a, _ := v.(*Vertex); a != nil {
		v = a.DerefValue().BaseValue
	}
	return v == cycle
}

type arcKey struct {
	arc *Vertex
	id  CloseInfo
}

// A nodeContext is used to collate all conjuncts of a value to facilitate
// unification. Conceptually order of unification does not matter. However,
// order has relevance when performing checks of non-monotic properties. Such
// checks should only be performed once the full value is known.
type nodeContext struct {
	nextFree *nodeContext

	// opID is assigned the opID of the OpContext upon creation.
	// This allows checking that we are not using stale nodeContexts.
	opID uint64

	// refCount:
	// evalv2: keeps track of all current usages of the node, such that the
	//    node can be freed when the counter reaches zero.
	// evalv3: keeps track of the number points in the code where this
	//.   nodeContext is used for processing. A nodeContext that is being
	//.   processed may not be freed yet.
	refCount int

	// isDisjunct indicates whether this nodeContext is used in a disjunction.
	// Disjunction cross products may call mergeCloseInfo, which assumes all
	// closedness information, which is stored in the nodeContext, is still
	// valid. This means that we need to follow a different approach for freeing
	// disjunctions.
	isDisjunct bool

	// Keep node out of the nodeContextState to make them more accessible
	// for source-level debuggers.
	node *Vertex

	// parent keeps track of the parent Vertex in which a Vertex is being
	// evaluated. This is to keep track of the full path in error messages.
	parent *Vertex

	// underlying is the original Vertex that this node overlays. It should be
	// set for all Vertex values that were cloned.
	underlying *Vertex

	nodeContextState

	scheduler

	// toFree keeps track of inlined vertices that potentially need to be freed
	// after processing the node. This is used to avoid memory leaks when an
	// inlined node is only partially processed to obtain a result.
	toFree []*Vertex

	// Below are slices that need to be managed when cloning and reclaiming
	// nodeContexts for reuse. We want to ensure that, instead of setting
	// slices to nil, we truncate the existing buffers so that they do not
	// need to be reallocated upon reuse of the nodeContext.

	arcMap []arcKey // not copied for cloning

	// vertexMap is used to map vertices in disjunctions.
	vertexMap vertexMap

	// notify is used to communicate errors in cyclic dependencies.
	// TODO: also use this to communicate increasingly more concrete values.
	notify []receiver

	// sharedIDs contains all the CloseInfos that are involved in a shared node.
	// There can be more than one if the same Vertex is shared multiple times.
	// It is important to keep track of each instance as we need to insert each
	// of them separately in case a Vertex is "unshared" to ensure that
	// closedness information is correctly computed in such cases.
	sharedIDs []CloseInfo

	cyclicConjuncts []cyclicConjunct

	// These fields are used to track type checking.
	reqDefIDs          []refInfo
	replaceIDs         []replaceID
	conjunctInfo       []conjunctInfo
	reqSets            reqSets
	containsDefIDCache map[[2]defID]bool // cache for containsDefID results

	// Checks is a list of conjuncts, as we need to preserve the context in
	// which it was evaluated. The conjunct is always a validator (and thus
	// a Value). We need to keep track of the CloseInfo, however, to be able
	// to catch cycles when evaluating BuiltinValidators.
	// TODO: introduce ValueConjunct to get better compile time type checking.
	checks []Conjunct

	postChecks []envCheck // Check non-monotonic constraints, among other things.

	// Disjunction handling
	disjunctions []envDisjunct

	// disjuncts holds disjuncts that evaluated to a non-bottom value.
	// TODO: come up with a better name.
	disjuncts    []*nodeContext
	disjunctErrs []*Bottom
	userErrs     []*Bottom

	// hasDisjunction marks wither any disjunct was added. It is listed here
	// instead of in nodeContextState as it should be cleared when a disjunction
	// is split off. TODO: find something more principled.
	hasDisjunction bool
}

type nodeContextState struct {
	// isInitialized indicates whether conjuncts have been inserted in the node.
	// Use node.isInitialized() to more generally check whether conjuncts have
	// been processed.
	isInitialized bool

	// toComplete marks whether completeNodeTasks needs to be called on this
	// node after a corresponding task has been completed.
	toComplete bool

	// embedsRecursivelyClosed is used to implement __reclose. It must be set
	// when a vertex that is recursively closed is embedded through a spread
	// operator. It is okay to set it if it is just unified with a vertex that
	// is recursively closed, but not added through a spread operator. The
	// result will just be an unnecessary call to __reclose.
	embedsRecursivelyClosed bool

	// isCompleting > 0 indicates whether a call to completeNodeTasks is in
	// progress.
	isCompleting int

	// runMode keeps track of what runMode a disjunct should run as. This is
	// relevant for nested disjunctions, like the 2|3 in (1 | (2|3)) & (1 | 2),
	// where the nested disjunction should _not_ be considered as final, as
	// there is still a disjunction at a higher level to be processed.
	runMode runMode

	// evalDept is a number that is assigned when evaluating arcs and is set to
	// detect structural cycles. This value may be temporarily altered when a
	// node descends into evaluating a value that may be an error (pattern
	// constraints, optional fields, etc.). A non-zero value always indicates
	// that there are cyclic references, though.
	evalDepth int

	// State info

	hasTop               bool
	hasAnyCyclicConjunct bool // has conjunct with structural cycle
	hasAncestorCycle     bool // has conjunct with structural cycle to an ancestor
	hasNonCycle          bool // has material conjuncts without structural cycle
	hasNonCyclic         bool // has non-cyclic conjuncts at start of field processing

	// These simulate the old closeContext logic. TODO: perhaps remove.
	hasStruct        bool // this node has a struct conjunct
	hasOpenValidator bool // this node has an open validator
	isDef            bool // this node is a definition

	dropParentRequirements bool // used for typo checking
	computedCloseInfo      bool // used for typo checking

	isShared         bool       // set if we are currently structure sharing
	noSharing        bool       // set if structure sharing is not allowed
	shared           Conjunct   // the original conjunct that led to sharing
	shareCycleType   CyclicType // keeps track of the cycle type of shared nodes
	origBaseValue    BaseValue  // the BaseValue that structure sharing replaces
	shareDecremented bool       // counters of sharedIDs have been decremented

	depth           int32
	defaultMode     defaultMode    // cumulative default mode
	origDefaultMode defaultMode    // default mode of the original disjunct
	priority        layer.Priority // Priority corresponding to defaultMode
	origPriority    layer.Priority // Priority of the original disjunct

	// has a value filled out before the node splits into a disjunction. Aside
	// from detecting a self-reference cycle when there is otherwise just an
	// other error, this field is not needed. It greatly helps, however, to
	// improve the error messages.
	hasFieldValue bool

	// defaultAttemptInCycle indicates that a value relies on the default value
	// and that it will be an error to remove the default value from the
	// disjunction. It is set to the referring Vertex. Consider for instance:
	//
	//      a: 1 - b
	//      b: 1 - a
	//      a: *0 | 1
	//      b: *0 | 1
	//
	// versus
	//
	//      a: 1 - b
	//      b: 1 - a
	//      a: *1 | 0
	//      b: *0 | 1
	//
	// In both cases there are multiple solutions to the configuration. In the
	// first case there is an ambiguity: if we start with evaluating 'a' and
	// pick the default for 'b', we end up with a value of '1' for 'a'. If,
	// conversely, we start evaluating 'b' and pick the default for 'a', we end
	// up with {a: 0, b: 0}. In the seconds case, however, we do _will_ get the
	// same answer regardless of order.
	//
	// In general, we will allow expressions on cyclic paths to be resolved if
	// in all cases the default value is taken. In order to do that, we do not
	// allow a default value to be removed from a disjunction if such value is
	// depended on.
	//
	// For completeness, note that CUE will NOT solve a solution, even if there
	// is only one solution. Consider for instance:
	//
	//      a: 0 | 1
	//      a: b + 1
	//      b: c - 1
	//      c: a - 1
	//      c: 1 | 2
	//
	// There the only consistent solution is {a: 1, b: 0, c: 1}. CUE, however,
	// will not attempt this solve this as, in general, such solving would be NP
	// complete.
	//
	// NOTE(evalv4): note that this would be easier if we got rid of default
	// values and had pre-selected overridable values instead.
	defaultAttemptInCycle *Vertex

	// Value info

	kind           Kind
	constraintKind Kind
	defaultKind    Kind
	kindExpr       Expr      // expr that adjust last value (for error reporting)
	kindID         CloseInfo // for error tracing

	// Current value (may be under construction)
	scalar   Value // TODO: use Value in node.
	scalarID CloseInfo

	aStruct   Expr
	aStructID CloseInfo

	// List fields
	listIsClosed bool
	maxListLen   int
	maxNode      Expr

	lowerBound *BoundValue // > or >=
	upperBound *BoundValue // < or <=
	errs       *Bottom
}

// A receiver receives notifications.
// cc is used for V3 and is nil in V2.
// v is equal to cc.src._cc in V3.
type receiver struct {
	v *Vertex
	c CloseInfo
}

// Logf substitutes args in format. Arguments of type Feature, Value, and Expr
// are printed in human-friendly formats. The printed string is prefixed and
// indented with the path associated with the current nodeContext.
func (n *nodeContext) Logf(format string, args ...interface{}) {
	n.ctx.Logf(n.node, format, args...)
}

func (c *OpContext) newNodeContext(node *Vertex) *nodeContext {
	var n *nodeContext
	if n = c.freeListNode; n != nil {
		c.stats.Reused++
		c.freeListNode = n.nextFree

		n.scheduler.clear()
		n.scheduler.ctx = c

		*n = nodeContext{
			scheduler: n.scheduler,
			node:      node,
			nodeContextState: nodeContextState{
				kind:           TopKind,
				constraintKind: TopKind,
				defaultKind:    TopKind,
			},
			toFree:             n.toFree[:0],
			arcMap:             n.arcMap[:0],
			cyclicConjuncts:    n.cyclicConjuncts[:0],
			notify:             n.notify[:0],
			sharedIDs:          n.sharedIDs[:0],
			checks:             n.checks[:0],
			postChecks:         n.postChecks[:0],
			reqDefIDs:          n.reqDefIDs[:0],
			replaceIDs:         n.replaceIDs[:0],
			conjunctInfo:       n.conjunctInfo[:0],
			reqSets:            n.reqSets[:0],
			disjunctions:       n.disjunctions[:0],
			disjunctErrs:       n.disjunctErrs[:0],
			userErrs:           n.userErrs[:0],
			disjuncts:          n.disjuncts[:0],
			containsDefIDCache: n.containsDefIDCache, // cleared below
		}
		clear(n.containsDefIDCache)
		n.scheduler.clear()
	} else {
		c.stats.Allocs++

		n = &nodeContext{
			scheduler: scheduler{
				ctx: c,
			},
			node: node,

			nodeContextState: nodeContextState{
				kind:           TopKind,
				constraintKind: TopKind,
				defaultKind:    TopKind,
			},
		}
	}

	n.opID = c.opID
	n.scheduler.node = n
	n.underlying = node
	if p := node.Parent; p != nil && p.state != nil {
		n.isDisjunct = p.state.isDisjunct
	}
	return n
}

func (n *nodeContext) free() {
	if n.refCount--; n.refCount == 0 {
		n.ctx.freeNodeContext(n)
	}
}

// freeNodeContext unconditionally adds a nodeContext to the free pool. The
// status should only be called for nodes with status finalized. Non-rooted
// vertex values, however, the status may be different. But also unprocessed
// nodes may have an uninitialized nodeContext. TODO(mem): this latter should be
// fixed.
//
// We leave it up to the caller to ensure it is safe to free the nodeContext for
// a given status.
func (c *OpContext) freeNodeContext(n *nodeContext) {
	n.node.state = nil
	c.stats.Freed++
	n.nextFree = c.freeListNode
	c.freeListNode = n
	n.node = nil
	n.refCount = 0
	n.scheduler.clear()
}

// TODO(perf): return a dedicated ConflictError that can track original
// positions on demand.
func (n *nodeContext) reportConflict(
	v1, v2 Node,
	k1, k2 Kind,
	ids ...CloseInfo) {

	ctx := n.ctx

	var err *ValueError
	if k1 == k2 {
		err = ctx.NewPosf(token.NoPos, "conflicting values %s and %s", v1, v2)
	} else {
		err = ctx.NewPosf(token.NoPos,
			"conflicting values %s and %s (mismatched types %s and %s)",
			v1, v2, k1, k2)
	}

	err.AddPosition(v1)
	err.AddPosition(v2)
	for _, id := range ids {
		err.AddClosedPositions(ctx, id)
	}

	n.addErr(err)
}

// reportFieldMismatch reports the mixture of regular fields with non-struct
// values. Either s or f needs to be given.
func (n *nodeContext) reportFieldMismatch(
	p token.Pos,
	s *StructLit,
	f Feature,
	scalar Expr,
	id ...CloseInfo) {

	ctx := n.ctx

	if f == InvalidLabel {
		for _, a := range s.Decls {
			if x, ok := a.(*Field); ok && x.Label.IsRegular() {
				f = x.Label
				p = pos(x)
				break
			}
		}
		if f == InvalidLabel {
			n.reportConflict(scalar, s, n.kind, StructKind, id...)
			return
		}
	}

	err := ctx.NewPosf(p, "cannot combine regular field %q with %v", f, scalar)

	if s != nil {
		err.AddPosition(s)
	}

	for _, ci := range id {
		err.AddClosedPositions(ctx, ci)
	}

	n.addErr(err)
}

func (n *nodeContext) updateNodeType(k Kind, v Expr, id CloseInfo) bool {
	n.updateConjunctInfo(k, id, 0)

	ctx := n.ctx
	kind := n.kind & k

	switch {
	case n.kind == BottomKind,
		k == BottomKind:
		return false

	case kind != BottomKind:

	// TODO: we could consider changing the reporting for structs, but this
	// makes only sense in case they are for embeddings. Otherwise the type
	// of a struct is more relevant for the failure.
	// case k == StructKind:
	// 	s, _ := v.(*StructLit)
	// 	n.reportFieldMismatch(token.NoPos, s, 0, n.kindExpr, id, n.kindID)

	case n.kindExpr != nil:
		n.reportConflict(n.kindExpr, v, n.kind, k, n.kindID, id)

	default:
		n.addErr(ctx.Newf(
			"conflicting value %s (mismatched types %s and %s)",
			v, n.kind, k))
	}

	if n.kind != kind || n.kindExpr == nil {
		n.kindExpr = v
	}
	n.kind = kind
	n.kindID = id
	return kind != BottomKind
}

// hasErr is used to determine if an evaluation path, for instance a single
// path after expanding all disjunctions, has an error.
func (n *nodeContext) hasErr() bool {
	n.assertInitialized()

	if n.node.ChildErrors != nil {
		return true
	}
	if n.node.Status() > evaluating && n.node.IsErr() {
		return true
	}
	return n.ctx.HasErr() || n.errs != nil
}

func (n *nodeContext) getErr() *Bottom {
	n.assertInitialized()

	n.errs = CombineErrors(nil, n.errs, n.ctx.Err())
	return n.errs
}

// getValidators sets the vertex' Value in case there was no concrete value.
func (n *nodeContext) getValidators(state vertexStatus) BaseValue {
	n.assertInitialized()

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
		if b, _ := c.x.(*BoundValue); b != nil && b.Op == NotEqualOp {
			if n.upperBound != nil &&
				SimplifyBounds(ctx, n.kind, n.upperBound, b) != nil {
				continue
			}
			if n.lowerBound != nil &&
				SimplifyBounds(ctx, n.kind, n.lowerBound, b) != nil {
				continue
			}
		}
		v := c.x.(Value)
		a = append(a, v)
		kind &= v.Kind()
	}

	if kind&^n.kind != 0 {
		a = append(a, &BasicType{
			Src: n.kindExpr.Source(), // TODO:Is this always a BasicType?
			K:   n.kind,
		})
	}

	var v BaseValue
	switch len(a) {
	case 0:
		// Src is the combined input.
		if state >= conjuncts || n.kind&^CompositeKind == 0 {
			v = &BasicType{K: n.kind}
		}

	case 1:
		v = a[0]

	default:
		v = &Conjunction{Values: a}
	}

	return v
}

type envCheck struct {
	env         *Environment
	expr        Expr
	expectError bool
}

func (n *nodeContext) addBottom(b *Bottom) {
	n.assertInitialized()

	n.errs = CombineErrors(nil, n.errs, b)
	// TODO(errors): consider doing this
	// n.kindExpr = n.errs
	// n.kind = 0
}

func (n *nodeContext) addErr(err errors.Error) {
	n.assertInitialized()

	if err != nil {
		n.addBottom(&Bottom{
			Err:  err,
			Node: n.node,
		})
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

func (n *nodeContext) insertFieldUnchecked(f Feature, mode ArcType, x Conjunct) *Vertex {
	return n.insertArc(f, mode, x, x.CloseInfo, false)
}

func (n *nodeContext) invalidListLength(na, nb int, a, b Expr) {
	n.addErr(n.ctx.Newf("incompatible list lengths (%d and %d)", na, nb))
}
