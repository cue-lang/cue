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

package adt

// TODO:
// - compiler support for detecting cross-pattern references.
// - handle propagation of cyclic references to root across disjunctions.

// # Cycle detection algorithm V3
//
// The cycle detection algorithm detects the following kind of cycles:
//
// - Structural cycles: cycles where a field, directly or indirectly, ends up
//   referring to an ancestor node. For instance:
//
//      a: b: a
//
//      a: b: c
//      c: a
//
//      T: a?: T
//      T: a: {}
//
// - Reference cycles: cycles where a field, directly or indirectly, end up
//   referring to itself:
//      a: a
//
//      a: b
//      b: a
//
// - Inline cycles: cycles within an expression, for instance:
//
//      x: {y: x}.out
//
// Note that it is possible for the unification of two non-cyclic structs to be
// cyclic:
//
//     y: {
//         f: h: g
//         g: _
//     }
//     x: {
//         f: _
//         g: f
//     }
//
// Even though the above contains no cycles, the result of `x & y` is cyclic:
//
//     f: h: g
//     g: f
//
// Cycle detection is inherently a dynamic process.
//
// ## ALGORITHM OVERVIEW
//
//  1.  Traversal with Path Tracking:
//      •   Perform a depth-first traversal of the CUE value graph.
//      •   Maintain a path (call stack) of ancestor nodes during traversal.
//          For this purpose, we separately track the parent relation as well
//          as marking nodes that are currently being processed.
//  2.  Per-Conjunct Cycle Tracking:
//      •   For each conjunct in a node’s value (i.e., c1 & c2 & ... & cn),
//          track cycles independently.
//      •   A node is considered non-cyclic if any of its conjuncts is
//          non-cyclic.
//  3.  Handling References:
//      •   When encountering a reference, check if it points to any node in the
//          current path.
//          •   If yes, mark the conjunct as cyclic.
//          •   If no, add the referenced node to the path and continue traversal.
//  4.  Handling Optional Constructs:
//      •   Conjuncts originating from optional fields, pattern constraints, and
//          disjunctions are marked as optional.
//      •   Cycle tracking for optional conjuncts is identical to conjuncts for
//          conjuncts not marked as optional up to the point a cycle is detected
//          (i.e. all conjuncts are cyclic).
//      •   When a cycle is detected, the lists of referenced nodes are cleared
//          for each conjuncts, which thereby are afforded one additional level
//          of cycles. This allows for any optional paths to terminate.
//
//
// ## CALL STACK
//
// There are two key types of structural cycles: referencing an ancestor and
// repeated mixing in of cyclic types. We track these separately.
//
// We also keep track the non-cyclicity of conjuncts a bit differently for these
// cases.
//
// ### Ancestor References
//
// Ancestor references are relatively easy to detect by simply checking if a
// resolved reference is a direct parent, or is a node that is currently under
// evaluation.
//
// An ancestor cycle is considered to be a structural cycle if there are no
// new sibling conjuncts associated with new structure.
//
// ### Reoccurring references
//
// For reoccuring references, we need to maintain a per-conjunct list of
// references. When a reference was previously resolved in a conjunct, we may
// have a cycle and will mark the conjunct as such.
//
// A cycle from a reoccurring reference is a structural cycle if there are
// no incoming arcs from any non-cyclic conjunct. The need for this subtle
// distinction can be clarified by an example;
//
// 		crossRefNoCycle: t4: {
// 			T: X={
// 				y: X.x
// 			}
//			// Here C.x.y must consider any incoming arc: here T originates from
//			// a non-cyclic conjunct, but once evaluated it becomes cyclic and
//			// will be the only conjunct. This is not a cycle, though. We must
//			// take into account that T was introduced from a non-cyclic
//			// conjunct.
// 			C: T & { x: T }
// 		}
//
//
// ## OPTIONAL PATHS
//
// Cyclic references for conjuncts that originate from an "optional" path, such
// as optional fields and pattern constraints, may not necessary be cyclic, as
// on a next iteration such conjuncts _may_ still terminate.
//
// To allow for this kind of eventuality, optional conjuncts are processed in
// two phases:
//
//  - they behave as normal conjuncts up to the point a cycle is detected
//  - afterwards, their reference history is cleared and they are afforded to
//    proceed until the next cycle is detected.
//
// Note that this means we may allow processing to proceed deeper than strictly
// necessary in some cases.
//
// Note that we only allow this for references: for cycles with ancestor nodes
// we immediately terminate for optional fields. This simplifies the algorithm.
// But it is also correct: in such cases either the whole node is in an optional
// path, in which case reporting an error is benign (as they are allowed), or
// the node corresponds to a non-optional field, in which case a cycle can be
// expected to reproduce another non-optional cycle, which will be an error.
//
// ### Examples
//
// These are not cyclic:
//
//  1. The structure is cyclic, but he optional field needs to be "fed" to
//     continue the cycle:
//
//      a: b?: a        // a: {}
//
//      b: [string]: b  // b: {}
//
//      c: 1 | {d: c}   // c: 1
//
//  2. The structure is cyclic. Conjunct `x: a` keeps detecting cycles, but
//     is fed with new structure up until x.b.c.b.c.b. After this, this
//     (optional) conjunct is allowed to proceed until the next cycle, which
//     not be reached, as the `b?` is not unified with a concrete value.
//     So the result of `x` is `{b: c: b: c: b: c: {}}`.
//
//      a: b?: c: a
//      x: a
//      x: b: c: b: c: b: {}
//
// These are cyclic:
//
//  3. Here the optional conjunct triggers a new cycle of itself, but also
//     of a conjunct that turns `b` into a regular field. It is thus a self-
//     feeding cycle.
//
//      a: b?: a
//      a: b: _
//
//      c: [string]: c
//      c: b: _
//
//  4.  Here two optional conjuncts end up feeding each other, resulting in a
//      cycle.
//
//      a: c: a | int
//      a: a | int
//
//      y1: c?: c: y1
//      x1: y1
//      x1: c: y1
//
//      y2: [string]: b: y2
//      x2: y2
//      x2: b: y2
//
//
// ## INLINE CYCLES
//
// The semantics for treating inline cycles can be derived by rewriting CUE of
// the form
//
//      x: {...}.out
//
// as
//
//      x:  _x.out
//      _x: {...}
//
// A key difference is that as such structs are not "rooted" (they have no path
// from the root of the configuration tree) and thus any error should be caught
// and evaluated before doing a lookup in such structs to be correct. For the
// purpose of this algorithm, this especially pertains to structural cycles.
//
// Note that the scope in which scope the "helper" field is defined may
// determine whether or not there is a structural cycle. Consider, for instance,
//
//      X: {in: a, out: in}
//      a: b: (X & {in: a}).out
//
// Two possible rewrites are:
//
//      X: {in: a, out: in}
//      a: b: _a.out
//      _a: X & {in: a}
//
// and
//
//      X: {in: a, out: in}
//      a: {
//          b: _b.out
//          _b: X & {in: a}
//      }
//
// The former prevents a structural cycle, the later results in a structural
// cycle.
//
// The current implementation takes the former approach, which more closely
// mimics the V2 implementation. Note that other approaches are possible.
//
// ### Examples
//
// Expanding these out with the above rules should give the same results.
//
// Cyclic:
//
//  1. This is an example of mutual recursion, triggered by n >= 2.
//
//      fibRec: {
//          nn: int,
//          out: (fib & {n: nn}).out
//      }
//      fib: {
//          n: int
//          if n >= 2 { out: (fibRec & {nn: n - 2}).out }
//          if n < 2  { out: n }
//      }
//      fib2: fib & {n: 2}
//
// is equivalent to
//
//      fibRec: {
//          nn:   int,
//          out:  _out.out
//          _out: fib & {n: nn}
//      }
//      fib: {
//          n: int
//          if n >= 2 {
//              out:  _out.out
//              _out: fibRec & {nn: n - 2}
//          }
//          if n < 2  { out: n }
//      }
//      fib2: fib & {n: 2}
//
// Non-cyclic:
//
//  2. This is not dissimilar to the previous example, but since additions are
//     done on separate lines, each field is only visited once and no cycle is
//     triggered.
//
//      f: { in:  number, out: in }
//      k00: 0
//      k10: (f & {in: k00}).out
//      k20: (f & {in: k10}).out
//      k10: (f & {in: k20}).out
//
// which is equivalent to
//
//      f: { in:  number, out: in }
//      k0:   0
//      k1:  _k1.out
//      k2:  _k2.out
//      k1:  _k3.out
//      _k1: f
//      _k2: f
//      _k3: f
//      _k1: in: k0
//      _k2: in: k1
//      _k3: in: k2
//
// and thus is non-cyclic.
//
// ## EDGE CASES
//
// This section lists several edge cases, including interactions with the
// detection of self-reference cycles.
//
// Self-reference cycles, like `a: a`, evaluate to top. The evaluator detects
// this cases and drop such conjuncts, effectively treating them as top.
//
// ### Self-referencing patterns
//
// Self-references in patterns are typically handled automatically. But there
// are some edge cases where the are not:
//
// 		_self: x: [...and(x)]
// 		_self
// 		x: [1]
//
// Patterns are recorded in Vertex values that are themselves evaluated to
// allow them to be compared, such as in subsumption or filtering disjunctions.
// In the above case, `x` may be evaluated to be inserted in the pattern
// Vertex, but because the pattern is not itself `x`, node identity cannot be
// used to detect a self-reference.
//
// The current solution is to mark a node as a pattern constraint and treat
// structural cycles to such nodes as "reference cycles". As pattern constraints
// are optional, it is safe to ignore such errors.
//
// ### Lookups in inline cycles
//
// A lookup, especially in inline cycles, should be considered evidence of
// non-cyclicity. Consider the following example:
//
// 		{ p: { x: p, y: 1 } }.p.x.y
//
// without considering a lookup as evidence of non-cyclicity, this would be
// resulting in a structural cycle.
//
// ## CORRECTNESS
//
// ### The algorithm will terminate
//
// First consider the algorithm without optional conjuncts. If a parent node is
// referenced, it will obviously be caught. The more interesting case is if a
// reference to a node is made which is later reintroduced.
//
// When a conjunct splits into multiple conjuncts, its entire cycle history is
// copied. This means that any cyclic conjunct will be marked as cyclic in
// perpetuity. Non-cyclic conjuncts will either remain non-cyclic or be turned
// into a cycle. A conjunct can only remain non-cyclic for a maximum of the
// number of nodes in a graph. For any structure to repeat, it must have a
// repeated reference. This means that eventually either all conjuncts will
// either terminate or become cyclic.
//
// Optional conjuncts do not materially alter this property. The only difference
// is that when a node-level cycle is detected, we continue processing of some
// conjuncts until this next cycle is reached.
//
//
// ## TODO
//
//  - treatment of let fields
//  - tighter termination for some mutual cycles in optional conjuncts.

// TODO: mark references as crossing optional boundaries, rather than
// approximating it during evaluation.

type CycleInfo struct {
	// CycleType is used by the V3 cycle detection algorithm to track whether
	// a cycle is detected and of which type.
	CycleType CyclicType

	// Refs is a linked list of RefNode tracking reference chains for cycle detection.
	// RefNodes are allocated from OpContext.refNodeArena to reduce GC pressure.
	Refs *RefNode
}

// IsCyclic indicates whether this conjunct, or any of its ancestors,
// had a violating cycle.
func (ci CycleInfo) IsCyclic() bool {
	return ci.CycleType == IsCyclic
}

// A RefNode is an element in a linked list of associated references.
// RefNodes are allocated from OpContext.refNodeArena to reduce GC pressure.
type RefNode struct {
	Ref Resolver
	Arc *Vertex // Ref points to this Vertex

	// Node is the Vertex of which Ref is evaluated as a conjunct.
	// If there is a cyclic reference (not structural cycle), then
	// the reference will have the same node. This allows detecting reference
	// cycles for nodes referring to nodes with an evaluation cycle
	// (mode tracked to Evaluating status). Examples:
	//
	//      a: x
	//      Y: x
	//      x: {Y}
	//
	// and
	//
	//      Y: x.b
	//      a: x
	//      x: b: {Y} | null
	//
	// In both cases there are not structural cycles and thus need to be
	// distinguished from regular structural cycles.
	Node *Vertex

	Next  *RefNode
	Depth int32
}

// allocRefNode allocates a RefNode in the arena and returns a pointer to it.
// This reduces GC pressure by batch-allocating RefNodes.
func (c *OpContext) allocRefNode(arc, node *Vertex, ref Resolver, next *RefNode, depth int32) *RefNode {
	c.refNodeArena = append(c.refNodeArena, RefNode{
		Arc:   arc,
		Node:  node,
		Ref:   ref,
		Next:  next,
		Depth: depth,
	})
	return &c.refNodeArena[len(c.refNodeArena)-1]
}

// cyclicConjunct is used in nodeContext to postpone the computation of
// cyclic conjuncts until a non-cyclic conjunct permits it to be processed.
type cyclicConjunct struct {
	c   Conjunct
	arc *Vertex // cached Vertex
}

// CyclicType indicates the type of cycle detected. The CyclicType is associated
// with a conjunct and may only increase in value for child conjuncts.
type CyclicType uint8

const (
	NoCycle CyclicType = iota

	// like newStructure, but derived from a reference. If this is set, a cycle
	// will move to maybeCyclic instead of isCyclic.
	IsOptional

	// maybeCyclic is set if a cycle is detected within an optional field.
	//
	MaybeCyclic

	// IsCyclic marks that this conjunct has a structural cycle.
	IsCyclic
)

func (n *nodeContext) detectCycle(arc *Vertex, env *Environment, x Resolver, ci CloseInfo) (_ CloseInfo, skip bool) {
	n.assertInitialized()

	// If we are pointing to a direct ancestor, and we are in an optional arc,
	// we can immediately terminate, as a cycle error within an optional field
	// is okay. If we are pointing to a direct ancestor in a non-optional arc,
	// we also can terminate, as this is a structural cycle.
	// TODO: use depth or check direct ancestry.
	if n.hasAncestor(arc) {
		return n.markCyclic(arc, env, x, ci)
	}

	// As long as a node-wide cycle has not yet been detected, we allow cycles
	// in optional fields to proceed unchecked.
	if n.hasNonCyclic && ci.CycleType == MaybeCyclic {
		return ci, false
	}

	for r := ci.Refs; r != nil; r = r.Next {
		if equalDeref(r.Arc, arc) {
			if equalDeref(r.Node, n.node) {
				// reference cycle
				return ci, true
			}

			// If there are still any non-cyclic conjuncts, and if this conjunct
			// is optional, we allow this to continue one more cycle.
			if ci.CycleType == IsOptional && n.hasNonCyclic {
				ci.CycleType = MaybeCyclic
				// There my still be a cycle if the optional field is a pattern
				// that unifies with itself, as in:
				//
				//		[string]: c
				//		a: b
				//		b: _
				//		c: a: int
				//
				// This is equivalent to a reference cycle.
				if r.Depth == n.depth {
					return ci, true
				}
				ci.Refs = nil
				return ci, false
			}

			if n.hasNonCycle && n.hasNonCyclic && r.Depth != n.depth {
				return ci, false
			}

			return n.markCyclicPath(arc, env, x, ci)
		}
		if equalDeref(r.Node, n.node) && r.Ref == x && arc.nonRooted {
			return n.markCyclicPath(arc, env, x, ci)
		}
	}

	ci.Refs = n.ctx.allocRefNode(deref(arc), deref(n.node), x, ci.Refs, n.depth)

	return ci, false
}

// markNonCyclic records when a non-cyclic conjunct is processed.
func (n *nodeContext) markNonCyclic(id CloseInfo) {
	switch id.CycleType {
	case NoCycle, IsOptional:
		n.hasNonCyclic = true
	}
}

// markCyclic marks a conjunct as being cyclic. Also, it postpones processing
// the conjunct in the absence of evidence of a non-cyclic conjunct.
func (n *nodeContext) markCyclic(arc *Vertex, env *Environment, x Resolver, ci CloseInfo) (CloseInfo, bool) {
	ci.CycleType = IsCyclic

	n.hasAnyCyclicConjunct = true
	n.hasAncestorCycle = true

	if !n.hasNonCycle && env != nil {
		// TODO: investigate if we can get rid of cyclicConjuncts in the new
		// evaluator.
		v := Conjunct{env, x, ci}
		n.cyclicConjuncts = append(n.cyclicConjuncts, cyclicConjunct{v, arc})
		return ci, true
	}
	return ci, false
}

func (n *nodeContext) markCyclicPath(arc *Vertex, env *Environment, x Resolver, ci CloseInfo) (CloseInfo, bool) {
	ci.CycleType = IsCyclic

	n.hasAnyCyclicConjunct = true

	if !n.hasNonCyclic && !n.hasNonCycle && env != nil {
		// TODO: investigate if we can get rid of cyclicConjuncts in the new
		// evaluator.
		v := Conjunct{env, x, ci}
		n.cyclicConjuncts = append(n.cyclicConjuncts, cyclicConjunct{v, arc})
		return ci, true
	}
	return ci, false
}

// combineCycleInfo merges the cycle information collected in the context into
// the given CloseInfo. Note that it only merges the cycle information in its
// entirety, if present, to avoid getting unrelated data.
func (c *OpContext) combineCycleInfo(ci CloseInfo) CloseInfo {
	cc := c.ci.CycleInfo
	if cc.IsCyclic() {
		ci.CycleInfo = cc
	}
	return ci
}

// hasDepthCycle uses depth counters to keep track of cycles:
//   - it allows detecting reference cycles as well (state evaluating is
//     no longer used in v3)
//   - it can capture cycles across inline structs, which do not have
//     Parent set.
//
// TODO: ensure that evalDepth is cleared when a node is finalized.
func (c *OpContext) hasDepthCycle(v *Vertex) bool {
	if s := v.state; s != nil && v.status != finalized {
		return s.evalDepth > 0 && s.evalDepth < c.evalDepth
	}
	return false
}

// hasAncestor checks whether a node is currently being processed. The code
// still assumes that is includes any node that is currently being processed.
func (n *nodeContext) hasAncestor(arc *Vertex) bool {
	if n.ctx.hasDepthCycle(arc) {
		return true
	}

	// 	TODO: insert test conditions for Bloom filter that guarantee that all
	// 	parent nodes have been marked as "hot", in which case we can avoid this
	// 	traversal.
	// if n.meets(allAncestorsProcessed)  {
	// 	return false
	// }

	for p := n.node.Parent; p != nil; p = p.Parent {
		// TODO(perf): deref arc only once.
		if equalDeref(p, arc) {
			return true
		}
	}
	return false
}

func (n *nodeContext) hasOnlyCyclicConjuncts() bool {
	return (n.hasAncestorCycle && !n.hasNonCycle) ||
		(n.hasAnyCyclicConjunct && !n.hasNonCyclic)
}

// setOptional marks a conjunct as being optional. The nodeContext is
// currently unused, but allows for checks to be added and to add logging during
// debugging.
func (c *CloseInfo) setOptional(n *nodeContext) {
	_ = n // See comment.
	if c.CycleType == NoCycle {
		c.CycleType = IsOptional
	}
}

// updateCyclicStatus looks for proof of non-cyclic conjuncts to override
// a structural cycle.
func (n *nodeContext) updateCyclicStatus(c CloseInfo) {
	n.hasFieldValue = true
	if !c.IsCyclic() {
		n.hasNonCycle = true
		for _, c := range n.cyclicConjuncts {
			ci := c.c.CloseInfo
			if c.arc != nil {
				n.scheduleVertexConjuncts(c.c, c.arc, ci)
			} else {
				n.scheduleConjunct(c.c, ci)
			}
		}
		n.cyclicConjuncts = n.cyclicConjuncts[:0]
	}
}

func assertStructuralCycle(n *nodeContext) bool {
	n.cyclicConjuncts = n.cyclicConjuncts[:0]

	if n.hasOnlyCyclicConjuncts() {
		n.reportCycleError()
		return true
	}
	return false
}

func (n *nodeContext) reportCycleError() {
	b := &Bottom{
		Code:  StructuralCycleError,
		Err:   n.ctx.Newf("structural cycle"),
		Value: n.node.Value(),
		Node:  n.node,
		// TODO: probably, this should have the referenced arc.
	}
	n.setBaseValue(CombineErrors(nil, n.node.Value(), b))
	// TODO(mem): might still be processing, so only use this when we
	// exclude processed nodes.
	// n.node.clearArcs(n.ctx)
	n.node.Arcs = nil
}

// makeAnonymousConjunct creates a conjunct that tracks self-references when
// evaluating an expression.
//
// Example:
// TODO:
func makeAnonymousConjunct(env *Environment, x Expr, refs *RefNode) Conjunct {
	return Conjunct{
		env, x, CloseInfo{CycleInfo: CycleInfo{
			Refs: refs,
		}},
	}
}

// incDepth increments the evaluation depth. This should typically be called
// before descending into a child node.
func (n *nodeContext) incDepth() {
	n.ctx.evalDepth++
}

// decDepth decrements the evaluation depth. It should be paired with a call to
// incDepth and be called after the processing of child nodes is done.
func (n *nodeContext) decDepth() {
	n.ctx.evalDepth--
}

// markDepth assigns the current evaluation depth to the receiving node.
// Any previously assigned depth is saved and returned and should be restored
// using unmarkDepth after processing n.
//
// When a node is encountered with a depth set to a non-zero value this
// indicates a cycle. The cycle is an evaluation cycle when the node's depth
// is equal to the current depth and a structural cycle otherwise.
func (n *nodeContext) markDepth() (saved int) {
	saved = n.evalDepth
	n.evalDepth = n.ctx.evalDepth
	return saved
}

// See markDepth.
func (n *nodeContext) unmarkDepth(saved int) {
	n.evalDepth = saved
}
