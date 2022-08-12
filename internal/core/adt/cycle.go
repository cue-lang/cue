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

// Cycle detection:
//
// - Current algorithm does not allow for early non-cyclic conjunct detection.
// - Record possibly cyclic references.
// - Mark as cyclic if no evidence is found.
// - Note that this also activates the same reference in other (parent) conjuncts.

// CYCLE DETECTION ALGORITHM
//
// BACKGROUND
//
// The cycle detection is inspired by the cycle detection used by Tomabechi's
// [Tomabechi COLING 1992] and Van Lohuizen's [Van Lohuizen ACL 2000] graph
// unification algorithms.
//
// Unlike with traditional graph unification, however, CUE uses references,
// which, unlike node equivalence, are unidirectional. This means that the
// technique to track equivalence through dereference, as common in graph
// unification algorithms like Tomabechi's, does not work unaltered.
//
// The unidirectional nature of references imply that each reference equates a
// facsimile of the value it points to. This renders the original approach of
// node-pointer equivalence useless.
//
//
// PRINCIPLE OF ALGORITHM
//
// The solution for CUE is based on two observations:
//
// - the CUE algorithm tracks all conjuncts that define a node separately, -
// accumulating used references on a per-conjunct basis causes duplicate
//   references to uniquely identify cycles.
//
// A structural cycle, as defined by the spec, can then be detected if all
// conjuncts are marked as a cycle.
//
// References are accumulated as follows:
//
// 1. If a conjunct is a reference the reference is associated with that
//    conjunct as well as the conjunct corresponding to the value it refers to.
// 2. If a conjunct is a struct (including lists), its references are associated
//    with all embedded values and fields.
//
// To narrow down the specifics of the reference-based cycle detection, let us
// explore structural cycles in a bit more detail.
//
//
// STRUCTURAL CYCLES
//
// See the language specification for a higher-level and more complete overview.
//
// We have to define when a cycle is detected. CUE implementations MUST report
// an error upon a structural cycle, and SHOULD report cycles at the shortest
// possible paths at which they occur, but MAY report these at deeper paths. For
// instance, the following CUE has a structural cycle
//
//     f: g: f
//
// The shortest path at which the cycle can be reported is f.g, but as all
// failed configurations are logically equal, it is fine for implementations to
// report them at f.g.g, for instance.
//
// It is not, however, correct to assume that a reference to a parent is always
// a cycle. Consider this case:
//
//     a: [string]: b: a
//
// Even though reference `a` refers to a parent node, the cycle needs to be fed
// by a concrete field in struct `a` to persist, meaning it cannot result in a
// cycle as defined in the spec as it is defined here. Note however, that a
// specialization of this configuration _can_ result in a cycle. Consider
//
//     a: [string]: b: a
//     a: c: _
//
// Here reference `a` is guaranteed to result in a structural cycle, as field
// `c` will match the pattern constraint unconditionally.
//
// In other words, it is not possible to exclude tracking references across
// pattern constraints from cycle checking.
//
// It is tempting to try to find a complete set of these edge cases with the aim
// to statically determine cases in which this occurs. But as [Carpenter 1992]
// demonstrates, it is possible for cycles to be created as a result of unifying
// two graphs that are themselves acyclic. The following example is a
// translation of Carpenters example to CUE:
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
// This means that, in practice, cycle detection has at least partially a
// dynamic component to it.
//
//
// ABSTRACT ALGORITHM
//
// The algorithm is described declaratively by defining what it means for a
// field to have a structural cycle. In the below, a _reference_ is uniquely
// identified by the pointer identity of a Go Resolver instance.
//
// Cycles are tracked on a per-conjunct basis and are not aggregated per Vertex:
// administrative information is only passed on from parent to child conjunct.
//
// A conjunct is a _parent_ of another conjunct if is a conjunct of one of the
// non-optional fields of the conjunct. For instance, conjunct `x` with value
// `{b: y & z}`, is a parent of conjunct `y` as well as `z`. Within field `b`,
// the conjuncts `y` and `z` would be tracked individually, though.
//
// A conjunct is _associated with a reference_ if its value was obtained by
// evaluating a reference. Note that a conjunct may be associated with many
// references if its evaluation requires evaluating a chain of references. For
// instance, consider
//
//    a: {x: d}
//    b: a
//    c: b & e
//    d: y: 1
//
// the first conjunct of field `c` (reference `b`) has the value `{x: y: 1}` and
// is associated with references `b` and `a`.
//
// The _tracked references_ of a conjunct are all references that are associated
// with it or any of its ancestors (parents of parents). For instance, the
// tracked references of conjunct `b.x` of field `c.x` are `a`, `b`, and `d`.
//
// A conjunct is a violating cycle if it is a reference that:
//  - occurs in the tracked references of the conjunct, or
//  - directly refers to a parent node of the conjunct.
//
// A conjunct is cyclic if it is a violating cycle or if any of its ancestors
// are a violating cycle.
//
// A field has a structural cycle if it is composed of at least one conjunct
// that is a violating cycle and no conjunct that is not cyclic.
//
// Note that a field can be composed of only cyclic conjuncts while still not be
// structural cycle: as long as there are no conjuncts that are a violating
// cycle, it is not a structural cycle. This is important for the following
//     case:
//
//         a: [string]: b: a
//         x: a
//         x: c: b: c: {}
//
// Here, reference `a` is never a cycle as the recursive references crosses a
// pattern constraint that only instantiates if it is unified with something
// else.
//
//
// DISCUSSION
//
// The goal of conjunct cycle marking algorithm is twofold: - mark conjuncts
// that are proven to propagate indefinitely - mark them as early as possible
// (shortest CUE path).
//
// TODO: Prove all cyclic conjuncts will eventually be marked as cyclic.
//
// TODO:
//   - reference marks whether it crosses a pattern, improving the case
//     a: [string]: b: c: b
//     This requires a compile-time detection mechanism.
//
//
// REFERENCES
// [Tomabechi COLING 1992]: https://aclanthology.org/C92-2068
//     Hideto Tomabechi. 1992. Quasi-Destructive Graph Unification with
//     Structure-Sharing. In COLING 1992 Volume 2: The 14th International
//     Conference on Computational Linguistics.
//
// [Van Lohuizen ACL 2000]: https://aclanthology.org/P00-1045/
//     Marcel P. van Lohuizen. 2000. "Memory-Efficient and Thread-Safe
//     Quasi-Destructive Graph Unification". In Proceedings of the 38th Annual
//     Meeting of the Association for Computational Linguistics, pages 352–359,
//     Hong Kong. Association for Computational Linguistics.
//
// [Carpenter 1992]:
//     Bob Carpenter, "The logic of typed feature structures."
//     Cambridge University Press, ISBN:0-521-41932-8

type CycleInfo struct {
	// IsCyclic indicates whether this conjunct, or any of its ancestors,
	// had a violating cycle.
	IsCyclic bool

	// Inline is used to detect expressions referencing themselves, for instance:
	//     {x: out, out: x}.out
	Inline bool

	// IsPattern indicates whether this conjunct was inserted by a pattern.
	IsPattern bool

	// TODO(perf): pack this in with CloseInfo. Make an uint32 pointing into
	// a buffer maintained in OpContext, using a mark-release mechanism.
	Refs *RefNode

	// Values is used to track the introduction of new values to invalidate
	// potential cycles in case repeated uses of a reference are triggered
	// by something that is not a cycle. For instance, consider
	//
	// 	T: {
	//		x: _
	//		y: x
	//	}
	//	C: { x: T } & T
	//
	// Here the repeated mixing in of T causes reference x to be repeatedly
	// added to the same conjunct. However, as it keeps refering to fresh
	// content, it is not a cycle.
	Values *ValueNode
}

// A RefNode is a linked list of associated references.
type RefNode struct {
	Ref   Resolver
	Arc   *Vertex
	Next  *RefNode
	Depth int32
}

// ValueNode is a node of a linked list of expressions. See the Values field
// in CycleInfo for more info.
type ValueNode struct {
	Expr Expr
	Next *ValueNode
}

// cyclicConjunct is used in nodeContext to postpone the computation of
// cyclic conjuncts until a non-cyclic conjunct permits it to be processed.
type cyclicConjunct struct {
	c   Conjunct
	arc *Vertex // cached Vertex
}

// markCycle checks whether the reference x is cyclic. There are two cases:
//   1) it was previously used in this conjunct, and
//   2) it directly references a parent node.
//
// Other inputs:
//   arc  the reference to which x points
//   v    the Conjunct from which x originates
//
// A cyclic node is added to a queue for later processing if no evidence of
// a non-cyclic node has so far been found. updateCyclicStatus processes
// delayed nodes down the line once such evidence is found.
//
// If a cycle is the result of "inline" processing (an expression referencing
// itself), an error is reported immediately.
//
// It returns the updated Conjunct (marked with cyclic info) and whether or not
// its processing should be delayed, which is the case if the conjunct seems to
// be fully cyclic so far.
func (n *nodeContext) markCycle(arc *Vertex, v Conjunct, x Resolver) (_ Conjunct, delay bool) {
	// Check whether the reference already occurred in the list, signaling
	// a potential cycle.
	found := false
	depth := int32(0)
	for r := v.CloseInfo.Refs; r != nil; r = r.Next {
		if r.Ref == x || r.Arc == arc {
			if v.CloseInfo.Inline {
				n.reportCycleError()
				return v, true
			}
			depth = r.Depth
			found = true

			// We do not exclude references pointing to the same nodes as these
			// are always cycles, and because excluding them would degrade
			// performance too much.
			if r.Arc == arc {
				break
			}

			// This loop checks that the detected cycle is not postponed or
			// invalidated by new values. See the comment in CloseInfo.Values.
		outer:
			for _, c := range arc.Conjuncts {
				x := c.Expr()
				for v := v.CloseInfo.Values; v != nil; v = v.Next {
					if v.Expr == x {
						continue outer
					}
				}
				v.CloseInfo.Values = &ValueNode{
					Next: v.CloseInfo.Values,
					Expr: x,
				}
				found = false
			}

			break
		}
	}

	if !found || depth != n.depth {
		// Adding this in case there is a definite cycle is unnecessary, but
		// gives somewhat better error messages.
		// We also need to add the reference again if the depth differs, as
		// the depth is used for tracking "new structure".
		v.CloseInfo.Refs = &RefNode{
			Arc:   arc,
			Ref:   x,
			Next:  v.CloseInfo.Refs,
			Depth: n.depth,
		}
	}

	if !found && arc.status != EvaluatingArcs {
		// No cycle.
		return v, false
	}

	alreadyCycle := v.CloseInfo.IsCyclic
	v.CloseInfo.IsCyclic = true

	// TODO: depth might legitimately be 0 if it is a root vertex.
	// In the worst case, this may lead to a spurious cycle.
	// Fix this by ensuring the root vertex starts with a depth of 1, for
	// instance.
	foundCycle := n.node.Parent == nil || depth == 0
	foundNonCycle := false

	if !foundCycle {
		// Look for evidence of "new structure" to invalidate the cycle.
		// This is done by checking for non-cyclic conjuncts between the
		// current vertex up to the ancestor to which the reference points.
		// Note that the cyclic conjunct may not be marked as such, so we
		// look for at least one other non-cyclic conjunct if this is the case.
		upCount := n.depth - depth
		for p := n.node.Parent; p != nil; p = p.Parent {
			if upCount--; upCount == 0 {
				break
			}
			a := p.Conjuncts
			count := 0
			for _, c := range a {
				if !c.CloseInfo.IsCyclic {
					count++
				}
			}
			if !alreadyCycle {
				count--
			}
			if count > 0 {
				foundNonCycle = true
				break
			}
		}
	}

	if foundCycle || !foundNonCycle {
		n.hasCycle = true
		if !n.hasNonCycle {
			n.cyclicConjuncts = append(n.cyclicConjuncts, cyclicConjunct{v, arc})
			return v, true
		}
	}

	return v, false
}

// updateCyclicStatus looks for proof of non-cyclic conjuncts to override
// a structural cycle.
func (n *nodeContext) updateCyclicStatus(c CloseInfo) {
	if !c.IsCyclic {
		n.hasNonCycle = true
		for _, c := range n.cyclicConjuncts {
			n.addVertexConjuncts(c.c, c.arc, false)
		}
		n.cyclicConjuncts = n.cyclicConjuncts[:0]
	}
}

func assertStructuralCycle(n *nodeContext) bool {
	if n.hasCycle && !n.hasNonCycle {
		n.reportCycleError()
		return true
	}
	return false
}

func (n *nodeContext) reportCycleError() {
	n.node.BaseValue = CombineErrors(nil,
		n.node.Value(),
		&Bottom{
			Code:  StructuralCycleError,
			Err:   n.ctx.Newf("structural cycle"),
			Value: n.node.Value(),
			// TODO: probably, this should have the referenced arc.
		})
	n.node.Arcs = nil
}

// makeAnonymousConjunct creates a conjunct that tracks self-references when
// evaluating an expression.
//
// Example:
//    TODO:
//
func makeAnonymousConjunct(env *Environment, x Expr, refs *RefNode) Conjunct {
	return Conjunct{
		env, x, CloseInfo{CycleInfo: CycleInfo{
			Inline: true,
			Refs:   refs,
		}},
	}
}
