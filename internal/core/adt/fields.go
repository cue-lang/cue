// Copyright 2023 CUE Authors
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

import (
	"fmt"
)

// This file holds the logic for the insertion of fields and pattern
// constraints, including tracking closedness.
//
//
// DESIGN GOALS
//
// Key to performance is to fail early during evaluation. This is especially
// true for disjunctions. In CUE evaluation, conjuncts may be evaluated in a
// fairly arbitrary order. We want to retain this flexibility while also failing
// on disallowed fields as soon as we have enough data to tell for certain.
//
// Keeping track of which fields are allowed means keeping provenance data on
// whether certain conjuncts originate from embeddings or definitions, as well
// as how they group together with other conjuncts. These data structure should
// allow for a "mark and unwind" approach to allow for backtracking when
// computing disjunctions.
//
// References to the same CUE value may be added as conjuncts through various
// paths. For instance, a reference to a definition may be added directly, or
// through embedding. How they are added affects which set of fields are
// allowed. This can make the removal of duplicate conjuncts hard. A solution
// should make it straightforward to deduplicate conjuncts if they have the same
// impact on field inclusion.
//
// All conjuncts associated with field constraints, including optional fields
// and pattern constraints, should be collated, deduplicated, and evaluated as
// if they were regular fields. This allows comparisons between values to be
// meaningful and helps to filter disjuncts.
//
// The provenance data generated by this algorithm should ideally be easily
// usable in external APIs.
//
//
// DATA STRUCTURES
//
// Conjuncts
//
// To keep track of conjunct provenance, each conjunct has a few flags that
// indicates whether it orignates from
//   - an embedding
//   - a definition
//   - a reference (optional and unimplemented)
//
// Conjuncts with the same origin are represented as a single Conjunct in the
// Vertex, where this conjunct is a list of these conjuncts. In other words, the
// conjuncts of a Vertex are really a forest (group of trees) of conjuncts that,
// recursively, reflect the provenance of the conjuncts contained within it.
//
// The current implementation uses a Vertex for listing conjuncts with the same
// origin. This Vertex is marked as "Dynamic", as it does not have a CUE path
// that leads to them.
//
//
// Constraints
//
// Vertex values separately keep track of pattern constraints. These consist of
// a list of patterns with associated conjuncts, and a CUE expression that
// represents the set of allowed fields. This information is mostly for equality
// checking: by the time this data is produced, conjuncts associated with
// patterns are already inserted into the computed subfields.
//
// Note that this representation assumes that patterns are always accrued
// cumulatively: a field that is allowed will accrue the conjuncts of any
// matched pattern, even if it originates from an embedding that itself does not
// allow this field.
//
//
// ALGORITHM
//
// When processing the conjuncts of a Vertex, subfields are tracked per
// "grouping" (the list of conjuncts of the same origin). Each grouping keeps a
// counter of the number of unprocessed conjuncts and subgroups associated with
// it. Field inclusion (closedness) can be computed as soon as all subconjuncts
// and subgroups are processed.
//
// Conjuncts of subfields are inserted in such a way that they reflect the same
// grouping as the parent Vertex, plus any grouping that may be added by the
// subfield itself.
//
// It would be possible, though, to collapse certain (combinations of) groups
// that contain only a single conjunct. This can limit the size of such conjunct
// trees.
//
// As conjuncts are added within their grouping context, it is possible to
// uniquely identify conjuncts only by Vertex and expression pointer,
// disregarding the Environment.
//
//
// EXAMPLE DATA STRUCTURE
//
//    a: #A
//    #A: {
//        #B
//        x: r1
//    }
//    #B: y: r2
//    r1: z: r3
//    r2: 2
//    r3: foo: 2
//
// gets evaluated into:
//
//    V_a: Arcs{
//        x: V_x [ V_def(#A)[ r1 ] ]
//        y: V_y [ V_def(#A)[ V_embed(#B)[ r2 ] ] ]
//    }
//
// When evaluating V_x, its Arcs, in turn become:
//
//    V_x: Arcs{
//        z: V_z [ V_def(#A)[ V_ref(r1)[ r3 ]) ]]
//    }
//
// The V_def(#A) is necessary here to ensure that closedness information can be
// computed, if necessary. The V_ref's, however, are optional, and can be
// omitted if provenance is less important:
//
//    V_x: Arcs{
//        z: V_z [ V_def(#A)[ r3 ]]
//    }
//
// Another possible optimization is to eliminate Vertices if there is only one
// conjunct: the embedding and definition flags in the conjunct can be
// sufficient in that case. The provenance data could potentially be derived
// from the Environment in that case. If an embedding conjunct is itself the
// only conjunct in a list, the embedding bit can be eliminated. So V_y in the
// above example could be reduced to
//
//    V_y [ V_def(#A)[ r2 ] ]
//

// TODO(perf):
// - the data structures could probably be collapsed with Conjunct. and the
//   Vertex inserted into the Conjuncts could be a special ConjunctGroup.

type closeContext struct {
	// Used to recursively insert Vertices.
	parent *closeContext

	dependencies []*ccDep // For testing only. See debug.go

	// child links to a sequence which additional patterns need to be verified
	// against (&&). If there are more than one, these additional nodes are
	// linked with next. Only closed nodes with patterns are added. Arc sets are
	// already merged during processing.
	child *closeContext

	// next holds a linked list of nodes to process.
	// See comments above and see linkPatterns.
	next *closeContext

	// if conjunctCount is 0, pattern constraints can be merged and the
	// closedness can be checked. To ensure that this is true, there should
	// be an additional increment at the start before any processing is done.
	conjunctCount int

	src *Vertex

	// isDef indicates whether the closeContext is created as part of a
	// definition.
	isDef bool

	// isEmbed indicates whether the closeContext is created as part of an
	// embedding.
	isEmbed bool

	// isClosed is true if a node is a def, it became closed because of a
	// reference or if it is closed by the close builtin.
	//
	// isClosed must only be set to true if all fields and pattern constraints
	// that define the domain of the node have been added.
	isClosed bool

	// isTotal is true if a node contains an ellipsis and is defined for all
	// values.
	isTotal bool

	// done is true if all dependencies have been decremented.
	done bool

	// needsCloseInSchedule is non-nil if a closeContext that was created
	// as an arc still needs to be decremented. It points to the creating arc
	// for reporting purposes.
	needsCloseInSchedule *closeContext

	// parentConjuncts represent the parent of this embedding or definition.
	// Any closeContext is represented by a ConjunctGroup in parent of the
	// expression tree.
	parentConjuncts conjunctGrouper
	// TODO: Only needed if more than one conjuncts.

	// arcs represents closeContexts for sub fields and notification targets
	// associated with this node that reflect the same point in the expression
	// tree as this closeContext. In both cases the are keyed by Vertex.
	arcs []ccArc

	// parentIndex is the position in the parent's arcs slice that corresponds
	// to this closeContext. This is currently unused. The intention is to use
	// this to allow groups with single elements (which will be the majority)
	// to be represented in place in the parent.
	parentIndex int

	group *ConjunctGroup

	// Patterns contains all patterns of the current closeContext.
	// It is used in the construction of Expr.
	Patterns []Value

	// Expr contains the Expr that is used for checking whether a Feature
	// is allowed in this context. It is only complete after the full
	// context has been completed, but it can be used for initial checking
	// once isClosed is true.
	Expr Value
}

// Label is a convenience function to return the label of the associated Vertex.
func (c *closeContext) Label() Feature {
	return c.src.Label
}

type ccArc struct {
	kind depKind
	key  *closeContext
	cc   *closeContext
}

type conjunctGrouper interface {
	assignConjunct(root *closeContext, c Conjunct, check bool) (arc *closeContext, pos int, added bool)
}

func (n *nodeContext) getArc(f Feature, mode ArcType) (arc *Vertex, isNew bool) {
	v := n.node
	for _, a := range v.Arcs {
		if a.Label == f {
			if f.IsLet() {
				a.MultiLet = true
				// TODO: add return here?
			}
			a.updateArcType(mode)
			return a, false
		}
	}

	arc = &Vertex{Parent: v, Label: f, ArcType: mode}
	if n.scheduler.frozen&fieldSetKnown != 0 {
		b := n.ctx.NewErrf("field %v not allowed by earlier comprehension or reference cycle", f)
		n.ctx.AddBottom(b)
		// This may panic for list arithmetic. Safer to leave out for now.
		arc.ArcType = ArcNotPresent
	}
	v.Arcs = append(v.Arcs, arc)
	return arc, true
}

func (v *Vertex) assignConjunct(root *closeContext, c Conjunct, check bool) (a *closeContext, pos int, added bool) {
	// TODO: consider clearing CloseInfo.cc.
	// c.CloseInfo.cc = nil

	arc := root.src

	pos = len(arc.Conjuncts) - 1

	added = !check || !arc.hasConjunct(c)
	if added {
		c.CloseInfo.cc = root
		arc.addConjunctUnchecked(c)
	}

	return root, pos, added
}

func (cc *closeContext) getKeyedCC(key *closeContext, c CycleInfo) *closeContext {
	for _, a := range cc.arcs {
		if a.key == key {
			return a.cc
		}
	}

	group := &ConjunctGroup{}

	if cc.parentConjuncts == cc {
		panic("parent is self")
	}

	parent, pos, _ := cc.parentConjuncts.assignConjunct(key, Conjunct{
		CloseInfo: CloseInfo{
			FromDef:   cc.isDef,
			FromEmbed: cc.isEmbed,
			CycleInfo: c,
		},
		x: group,
	}, false)

	arc := &closeContext{
		parent:          parent,
		parentConjuncts: parent,
		parentIndex:     pos,

		src:   key.src,
		group: group,

		isDef:                cc.isDef,
		isEmbed:              cc.isEmbed,
		needsCloseInSchedule: cc,
	}

	arc.parent.incDependent(PARENT, arc)

	// If the parent, w.r.t. the subfield relation was already processed,
	// there is no need to register the notification.
	arc.incDependent(EVAL, cc) // matched in REF(decrement:nodeDone)

	// A let field never depends on its parent. So it is okay to filter here.
	if !arc.Label().IsLet() {
		cc.addDependency(ARC, key, arc)
	}

	v := key.src
	if v.Parent != nil && v.Parent.state != nil {
		v.Parent.state.checkArc(cc, v)
	}

	return arc
}

func (cc *closeContext) linkNotify(key *closeContext, c CycleInfo) {
	for _, a := range cc.arcs {
		if a.key == key {
			return
		}
	}

	cc.addDependency(NOTIFY, key, key)
}

func (cc *closeContext) assignConjunct(root *closeContext, c Conjunct, check bool) (arc *closeContext, pos int, added bool) {
	arc = cc.getKeyedCC(root, c.CloseInfo.CycleInfo)

	pos = len(*arc.group) - 1

	c.CloseInfo.cc = nil
	added = !check || !hasConjunct(*arc.group, c)
	if added {
		c.CloseInfo.cc = arc

		if c.CloseInfo.cc.src != arc.src {
			panic("Inconsistent src")
		}
		*arc.group = append(*arc.group, c)
	}

	return arc, pos, added
}

// spawnCloseContext wraps the closeContext in c with a new one and returns
// this new context along with an updated CloseInfo. The new values reflect
// that the set of fields represented by c are now, for instance, enclosed in
// an embedding or a definition.
//
// This call is used when preparing ADT values for evaluation.
func (c CloseInfo) spawnCloseContext(t closeNodeType) (CloseInfo, *closeContext) {
	cc := c.cc
	if cc == nil {
		panic("nil closeContext")
	}

	c.cc = &closeContext{
		parent:          cc,
		src:             cc.src,
		parentConjuncts: cc,
	}

	cc.incDependent(PARENT, c.cc) // REF(decrement: spawn)

	switch t {
	case closeDef:
		c.cc.isDef = true
	case closeEmbed:
		c.cc.isEmbed = true
	}

	return c, c.cc
}

// addDependency adds a dependent arc to c. If child is an arc, child.src == key
func (c *closeContext) addDependency(kind depKind, key, child *closeContext) {
	// NOTE: do not increment
	// - either root closeContext or otherwise resulting from sub closeContext
	//   all conjuncts will be added now, notified, or scheduled as task.

	child.incDependent(kind, c) // matched in decDependent REF(arcs)

	for _, a := range c.arcs {
		if a.key == key {
			panic("addArc: Label already exists")
		}
	}
	c.arcs = append(c.arcs, ccArc{kind: kind, key: key, cc: child})
}

// incDependent needs to be called for any conjunct or child closeContext
// scheduled for c that is queued for later processing and not scheduled
// immediately.
func (c *closeContext) incDependent(kind depKind, dependant *closeContext) (debug *ccDep) {
	if c.src == nil {
		panic("incDependent: unexpected nil state")
	}

	debug = c.addDependent(kind, dependant)

	if c.done {
		ctx := c.src.state.ctx
		openDebugGraph(ctx, c.src, "incDependent: already checked")

		panic(fmt.Sprintf("incDependent: already closed: %p", c))
	}

	c.conjunctCount++
	return debug
}

// decDependent needs to be called for any conjunct or child closeContext for
// which a corresponding incDependent was called after it has been successfully
// processed.
func (c *closeContext) decDependent(ctx *OpContext, kind depKind, dependant *closeContext) {
	v := c.src

	c.matchDecrement(v, kind, dependant)

	if c.conjunctCount == 0 {
		panic(fmt.Sprintf("negative reference counter %d %p", c.conjunctCount, c))
	}

	c.conjunctCount--
	if c.conjunctCount > 0 {
		return
	}

	c.done = true

	p := c.parent

	if c.isDef {
		c.isClosed = true
		if p != nil {
			p.isDef = true
		}
	}

	for _, a := range c.arcs {
		cc := a.cc
		cc.decDependent(ctx, a.kind, c) // REF(arcs)
	}

	c.finalizePattern()

	if p == nil {
		// Root pattern, set allowed patterns.
		if pcs := v.PatternConstraints; pcs != nil {
			if pcs.Allowed != nil {
				panic("unexpected allowed set")
			}
			pcs.Allowed = c.Expr
			return
		}
		return
	}

	if !c.isEmbed && c.isClosed {
		// Merge the two closeContexts and ensure that the patterns and fields
		// are mutually compatible according to the closedness rules.
		injectClosed(ctx, c, p)
		p.Expr = mergeConjunctions(p.Expr, c.Expr)
	} else {
		// Do not check closedness of fields for embeddings.
		// The pattern constraints of the embedding still need to be added
		// to the current context.
		p.linkPatterns(c)
	}

	p.decDependent(ctx, PARENT, c) // REF(decrement: spawn)

	// If we have started decrementing a child closeContext, the parent started
	// as well. If it is still marked as needing an EVAL decrement, which can
	// happen if processing started before the node was added, it is safe to
	// decrement it now. In this case the NOTIFY and ARC dependencies will keep
	// the nodes alive until they can be completed.
	if dep := p.needsCloseInSchedule; dep != nil {
		p.needsCloseInSchedule = nil
		p.decDependent(ctx, EVAL, dep)
	}
}

// linkPatterns merges the patterns of child into c, if needed.
func (c *closeContext) linkPatterns(child *closeContext) {
	if len(child.Patterns) > 0 {
		child.next = c.child
		c.child = child
	}
}

// checkArc validates that the node corresponding to cc allows a field with
// label v.Label.
func (n *nodeContext) checkArc(cc *closeContext, v *Vertex) *Vertex {
	f := v.Label
	ctx := n.ctx

	if f.IsHidden() || f.IsLet() {
		return v
	}

	if cc.isClosed && !matchPattern(ctx, cc.Expr, f) {
		ctx.notAllowedError(n.node, v)
	}
	if n.scheduler.frozen&fieldSetKnown != 0 {
		for _, a := range n.node.Arcs {
			if a.Label == f {
				return v
			}
		}
		var b *Bottom
		// TODO: include cycle data and improve error message.
		if f.IsInt() {
			b = ctx.NewErrf(
				"element at index %v not allowed by earlier comprehension or reference cycle", f)
		} else {
			b = ctx.NewErrf(
				"field %v not allowed by earlier comprehension or reference cycle", f)
		}
		v.SetValue(ctx, b)
	}

	return v
}

// insertConjunct inserts conjunct c into cc.
func (cc *closeContext) insertConjunct(key *closeContext, c Conjunct, id CloseInfo, check bool) bool {
	arc, _, added := cc.assignConjunct(key, c, check)
	if key.src != arc.src {
		panic("inconsistent src")
	}

	return added
}

// insertArc inserts conjunct c into n. If check is true it will not add c if it
// was already added.
// Returns the arc of n.node with label f.
func (n *nodeContext) insertArc(f Feature, mode ArcType, c Conjunct, id CloseInfo, check bool) *Vertex {
	if n == nil {
		panic("nil nodeContext")
	}
	if n.node == nil {
		panic("nil node")
	}
	cc := id.cc
	if cc == nil {
		panic("nil closeContext")
	}

	v, insertedArc := n.getArc(f, mode)

	if v.ArcType == ArcNotPresent {
		n.node.reportFieldCycleError(n.ctx, c.Source().Pos(), f)
		return v
	}

	if !cc.insertConjunct(v.rootCloseContext(), c, id, check) {
		return v
	}

	if !insertedArc {
		return v
	}

	// Match and insert patterns.
	if pcs := n.node.PatternConstraints; pcs != nil {
		for _, pc := range pcs.Pairs {
			if matchPattern(n.ctx, pc.Pattern, f) {
				for _, c := range pc.Constraint.Conjuncts {
					n.addConstraint(v, mode, c, check)
				}
			}
		}
	}

	return v
}

// addConstraint adds a constraint to arc of n.
//
// In order to resolve LabelReferences, it is not always possible to walk up
// the parent Vertex chain to determan the label, because a label reference
// may point past a point of referral. For instance,
//
//	test: [ID=_]: name: ID
//	test: A: {}
//	B: test.A & {}  // B.name should be "A", not "B".
//
// The arc must be the node arc to which the conjunct is added.
func (n *nodeContext) addConstraint(arc *Vertex, mode ArcType, c Conjunct, check bool) {
	// TODO(perf): avoid cloning the Environment, if:
	// - the pattern constraint has no LabelReference
	//   (require compile-time support)
	// - there are no references in the conjunct pointing to this node.
	// - consider adding this value to the Conjunct struct
	f := arc.Label
	bulkEnv := *c.Env
	bulkEnv.DynamicLabel = f
	c.Env = &bulkEnv

	cc := c.CloseInfo.cc

	arc, _ = n.getArc(f, mode)

	root := arc.rootCloseContext()
	cc.insertConjunct(root, c, c.CloseInfo, check)
}

func (n *nodeContext) insertPattern(pattern Value, c Conjunct) {
	ctx := n.ctx
	cc := c.CloseInfo.cc

	// Collect patterns in root vertex. This allows comparing disjuncts for
	// equality as well as inserting new arcs down the line as they are
	// inserted.
	if !n.insertConstraint(pattern, c) {
		return
	}

	// Match against full set of arcs from root, but insert in current vertex.
	// Hypothesis: this may not be necessary. Maybe for closedness.
	// TODO: may need to replicate the closedContext for patterns.
	// Also: Conjuncts for matching other arcs in this node may be different
	// for matching arcs using v.foo?, if we need to ensure that conjuncts
	// from arcs and patterns are grouped under the same vertex.
	// TODO: verify. See test Pattern 1b
	for _, a := range n.node.Arcs {
		if matchPattern(n.ctx, pattern, a.Label) {
			// TODO: is it necessary to check for uniqueness here?
			n.addConstraint(a, a.ArcType, c, true)
		}
	}

	if cc.isTotal {
		return
	}
	if isTotal(pattern) {
		cc.isTotal = true
		cc.Patterns = cc.Patterns[:0]
		return
	}

	// insert pattern in current set.
	// TODO: normalize patterns
	// TODO: do we only need to do this for closed contexts?
	for _, pc := range cc.Patterns {
		if Equal(ctx, pc, pattern, 0) {
			return
		}
	}
	cc.Patterns = append(cc.Patterns, pattern)
}

// isTotal reports whether pattern value p represents a full domain, that is,
// whether it is of type BasicType or Top.
func isTotal(p Value) bool {
	switch p.(type) {
	case *BasicType:
		return true
	case *Top:
		return true
	}
	return false
}

// injectClosed updates dst so that it only allows fields allowed by closed.
//
// It first ensures that the fields contained in dst are allowed by the fields
// and patterns defined in closed. It reports an error in the nodeContext if
// this is not the case.
func injectClosed(ctx *OpContext, closed, dst *closeContext) {
	// TODO: check that fields are not void arcs.
outer:
	for _, a := range dst.arcs {
		ca := a.cc
		f := ca.Label()
		if f.IsHidden() || f.IsLet() {
			continue
		}
		for _, b := range closed.arcs {
			cb := b.cc
			if f == cb.Label() {
				continue outer
			}
		}
		if !matchPattern(ctx, closed.Expr, ca.Label()) {
			ctx.notAllowedError(closed.src, ca.src)
			continue
		}
	}

	if !dst.isClosed {
		// Since dst is not closed, it is safe to take all patterns from
		// closed.
		// This is only necessary for passing up patterns into embeddings. For
		// (the conjunction of) definitions the construction is handled
		// elsewhere.
		// TODO(perf): reclaim slice memory
		dst.Patterns = closed.Patterns

		dst.isClosed = true
	}
}

func (ctx *OpContext) addPositions(c Conjunct) {
	if x, ok := c.x.(*ConjunctGroup); ok {
		for _, c := range *x {
			ctx.addPositions(c)
		}
	}
	if pos := c.Field(); pos != nil {
		ctx.AddPosition(pos)
	}
}

// notAllowedError reports a field not allowed error in n and sets the value
// for arc f to that error.
func (ctx *OpContext) notAllowedError(v, arc *Vertex) {
	defer ctx.PopArc(ctx.PushArc(arc))

	defer ctx.ReleasePositions(ctx.MarkPositions())

	for _, c := range arc.Conjuncts {
		ctx.addPositions(c)
	}
	// XXX(0.7): Find another way to get this provenance information. Not
	// currently stored in new evaluator.
	// for _, s := range x.Structs {
	//  s.AddPositions(ctx)
	// }

	if arc.ArcType == ArcPending {
		arc.ArcType = ArcNotPresent
		return
	}
	// TODO: setting arc instead of n.node eliminates subfields. This may be
	// desirable or not, but it differs, at least from <=v0.6 behavior.
	arc.SetValue(ctx, ctx.NewErrf("field not allowed"))

	// TODO: remove? We are now setting it on both fields, which seems to be
	// necessary for now. But we should remove this as it often results in
	// a duplicate error.
	v.SetValue(ctx, ctx.NewErrf("field not allowed"))

	// TODO: create a special kind of error that gets the positions
	// of the relevant locations upon request from the arc.
}

// mergeConjunctions combines two values into one. It never modifies an
// existing conjunction.
func mergeConjunctions(a, b Value) Value {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	ca, _ := a.(*Conjunction)
	cb, _ := b.(*Conjunction)
	n := 2
	if ca != nil {
		n += len(ca.Values) - 1
	}
	if cb != nil {
		n += len(cb.Values) - 1
	}
	vs := make([]Value, 0, n)
	if ca != nil {
		vs = append(vs, ca.Values...)
	} else {
		vs = append(vs, a)
	}
	if cb != nil {
		vs = append(vs, cb.Values...)
	} else {
		vs = append(vs, b)
	}
	// TODO: potentially order conjuncts to make matching more likely.
	return &Conjunction{Values: vs}
}

// finalizePattern updates c.Expr to a CUE Value representing all fields allowed
// by the pattern constraints of c. If this context or any of its direct
// children is closed, the result will be a conjunction of all these closed
// values. Otherwise it will be a disjunction of all its children. A nil value
// represents all values.
func (c *closeContext) finalizePattern() {
	switch {
	case c.Expr != nil: // Patterns and expression are already set.
		if !c.isClosed {
			panic("c.Expr set unexpectedly")
		}
		return
	case c.isTotal: // All values are allowed always.
		return
	}

	// As this context is not closed, the pattern is somewhat meaningless.
	// It may still be useful for analysis.
	or := c.Patterns

	for cc := c.child; cc != nil; cc = cc.next {
		if cc.isTotal {
			return
		}
		// Could be closed, in which case it must also be an embedding.

		// TODO: simplify the values.
		switch x := cc.Expr.(type) {
		case nil:
		case *Disjunction:
			or = append(or, x.Values...)
		default:
			or = append(or, x)
		}
	}

	switch len(or) {
	case 0:
	case 1:
		c.Expr = or[0]
	default:
		// TODO: potentially order conjuncts to make matching more likely.
		c.Expr = &Disjunction{Values: or}
	}
}
