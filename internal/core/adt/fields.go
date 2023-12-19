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

	Arcs []*Vertex // TODO:  also link to parent.src Vertex?

	// Patterns contains all patterns of the current closeContext.
	// It is used in the construction of Expr.
	Patterns []Value

	// Expr contains the Expr that is used for checking whether a Feature
	// is allowed in this context. It is only complete after the full
	// context has been completed, but it can be used for initial checking
	// once isClosed is true.
	Expr Value
}

// spawnCloseContext wraps the closeContext in c with a new one and returns
// this new context along with an updated CloseInfo. The new values reflect
// that the set of fields represented by c are now, for instance, enclosed in
// an embedding or a definition.
//
// This call is used when preparing ADT values for evaluation.
func (c CloseInfo) spawnCloseContext(t closeNodeType) (CloseInfo, *closeContext) {
	c.cc.incDependent()

	c.cc = &closeContext{
		parent: c.cc,
	}

	switch t {
	case closeDef:
		c.cc.isDef = true
	case closeEmbed:
		c.cc.isEmbed = true
	}

	return c, c.cc
}

// incDependent needs to be called for any conjunct or child closeContext
// scheduled for c that is queued for later processing and not scheduled
// immediately.
func (c *closeContext) incDependent() {
	c.conjunctCount++
}

// decDependent needs to be called for any conjunct or child closeContext for
// which a corresponding incDependent was called after it has been successfully
// processed.
func (c *closeContext) decDependent(n *nodeContext) {
	c.conjunctCount--
	if c.conjunctCount > 0 {
		return
	}

	c.finalizePattern(n)

	if c.isDef {
		c.isClosed = true
	}

	p := c.parent
	if p == nil {
		// Root pattern, set allowed patterns.
		if pcs := n.node.PatternConstraints; pcs != nil {
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
		injectClosed(n, c, p)
		p.Expr = mergeConjunctions(p.Expr, c.Expr)
	} else {
		// Do not check closedness of fields for embeddings.
		// The pattern constraints of the embedding still need to be added
		// to the current context.
		p.linkPatterns(c)
	}

	p.decDependent(n)
}

// linkPatterns merges the patterns of child into c, if needed.
func (c *closeContext) linkPatterns(child *closeContext) {
	if len(child.Patterns) > 0 {
		child.next = c.child
		c.child = child
	}
}

// insertArc inserts conjunct c into n. If check is true it will not add c if it
// was already added.
func (n *nodeContext) insertArc(f Feature, mode ArcType, c Conjunct, check bool) {
	if n == nil {
		panic("nil nodeContext")
	}
	if n.node == nil {
		panic("nil node")
	}
	cc := c.CloseInfo.cc
	if cc == nil {
		panic("nil closeContext")
	}

	if _, isNew := n.insertArc1(f, mode, c, check); !isNew {
		return // Patterns were already added.
	}

	// Match and insert patterns.
	if pcs := n.node.PatternConstraints; pcs != nil {
		for _, pc := range pcs.Pairs {
			if matchPattern(n, pc.Pattern, f) {
				for _, c := range pc.Constraint.Conjuncts {
					n.insertArc1(f, mode, c, check)
				}
			}
		}
	}
}

// insertArc1 inserts conjunct c into its associated closeContext. If the
// closeContext did not yet have a Vertex for f, it is created and it is ensured
// that the grouping is associated with all the parent closeContexts. If it is
// newly added to the root closeContext, the outer grouping is also added to
// n.node, the top-level Vertex itself.
//
// insertArc1 is exclusively used by insertArc to insert conjuncts for regular
// fields as well as pattern constraints.
func (n *nodeContext) insertArc1(f Feature, mode ArcType, c Conjunct, check bool) (v *Vertex, isNew bool) {
	cc := c.CloseInfo.cc

	// Locate or create the arc in the current context.
	v, isNew = cc.insertArc(n, f, mode, c, check)
	if !isNew {
		return v, false
	}

	i := cc.parent
	for prev := cc; i != nil && isNew; prev, i = i, i.parent {
		vc := MakeRootConjunct(nil, v)
		vc.CloseInfo.FromDef = prev.isDef
		vc.CloseInfo.FromEmbed = prev.isEmbed
		v, isNew = i.insertArc(n, f, mode, vc, check)
	}
	if isNew && i == nil {
		n.node.Arcs = append(n.node.Arcs, v)
	}

	if cc.isClosed && !v.disallowedField && !matchPattern(n, cc.Expr, f) {
		n.notAllowedError(n.node)
	}

	return v, isNew
}

// insertArc is exclusively called from nodeContext.insertArc1 and is used to
// insert a conjunct in closeContext, along with other conjuncts from the same
// origin. It does not recursively insert conjuncts into parent closeContexts,
// which is done by insertArc1.
//
// If check is true it will not add c if it was already added.
func (cc *closeContext) insertArc(
	n *nodeContext, f Feature, mode ArcType, c Conjunct, check bool) (v *Vertex, isNew bool) {
	c.CloseInfo.cc = nil

	for _, a := range cc.Arcs {
		if a.Label != f {
			continue
		}

		if f.IsLet() {
			a.MultiLet = true
			return a, false
		}
		if check {
			a.AddConjunct(c)
		} else {
			a.addConjunctUnchecked(c)
		}
		// TODO: possibly add positions to error.
		return a, false
	}

	v = &Vertex{Parent: cc.src, Label: f, ArcType: mode}
	if mode == ArcPending {
		cc.src.hasPendingArc = true
	}
	v.Conjuncts = append(v.Conjuncts, c)
	cc.Arcs = append(cc.Arcs, v)

	return v, true
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
		if matchPattern(n, pattern, a.Label) {
			// TODO: is it necessary to check for uniqueness here?
			n.insertArc(a.Label, a.ArcType, c, true)
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
func injectClosed(n *nodeContext, closed, dst *closeContext) {
	// TODO: check that fields are not void arcs.
outer:
	for _, a := range dst.Arcs {
		for _, b := range closed.Arcs {
			if a.Label == b.Label {
				if b.disallowedField {
					// Error was already reported.
					break
				}
				continue outer
			}
		}
		if !matchPattern(n, closed.Expr, a.Label) {
			n.notAllowedError(a)
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
func (n *nodeContext) notAllowedError(arc *Vertex) {
	// Set the error on the same arc as the old implementation
	// and using the same path.
	// arc := n.node.Lookup(f)
	v := n.ctx.PushArc(arc)

	defer n.ctx.ReleasePositions(n.ctx.MarkPositions())

	x := arc // n.node
	for _, c := range x.Conjuncts {
		n.ctx.addPositions(c)
	}
	// XXX(0.7): Find another way to get this provenance information. Not
	// currently stored in new evaluator.
	// for _, s := range x.Structs {
	//  s.AddPositions(n.ctx)
	// }

	if arc.ArcType == ArcPending {
		arc.ArcType = ArcNotPresent
		return
	}
	// TODO: setting arc instead of n.node eliminates subfields. This may be
	// desirable or not, but it differs, at least from <=v0.6 behavior.
	n.Logf("======----  FIELD NOT ALLOWED -----====")
	arc.SetValue(n.ctx, n.ctx.NewErrf("field not allowed"))

	// TODO: remove?
	n.node.SetValue(n.ctx, n.ctx.NewErrf("field not allowed"))
	arc.disallowedField = true // Is this necessary?
	n.ctx.PopArc(v)

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
func (c *closeContext) finalizePattern(n *nodeContext) {
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
