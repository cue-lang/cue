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
	"slices"
	"sync/atomic"
)

// This file contains functionality for pattern constraints.

// Constraints keeps track of pattern constraints and the set of allowed
// fields.
type Constraints struct {
	// Pairs lists Pattern-Constraint pairs.
	// Use a pair by index or by pointer; it must not be copied.
	Pairs []PatternConstraint // TODO(perf): move to Arcs?

	// Allowed is a Value that defines the set of all allowed fields.
	// To check if a field is allowed, its correpsonding CUE value can be
	// unified with this value.
	Allowed Value
}

// A PatternConstraint represents a single
//
//	[pattern]: T.
//
// The Vertex holds a list of conjuncts to represent the constraints. We use
// a Vertex so that these can be evaluated and compared for equality.
// Unlike for regular Vertex values, CloseInfo.closeContext is set for
// constraints: it is needed when matching subfields to ensure that conjuncts
// get inserted into the proper groups.
type PatternConstraint struct {
	Pattern    Value
	Constraint *Vertex

	// finalized caches the copy made by [Vertex.PatternConstraint].
	finalized atomic.Pointer[Vertex]
}

// PatternConstraint returns the finalized constraint of the i-th pair of
// v.PatternConstraints, which must not be nil.
//
// A constraint is not necessarily finalized along with v, which may be
// shared between goroutines once finalized. Such a constraint is not
// mutated: a copy of it is evaluated on first use and cached.
func (v *Vertex) PatternConstraint(c *OpContext, i int) *Vertex {
	pc := &v.PatternConstraints.Pairs[i]
	orig := pc.Constraint
	// Each vertex's own status matters here, not [Vertex.Status]: a sharing
	// vertex left evaluating is still mutated by Finalize.
	if v.status != finalized {
		// v is still being evaluated by c.
		orig.Finalize(c)
		return orig
	}
	if orig.status == finalized {
		return orig
	}
	if w := pc.finalized.Load(); w != nil {
		return w
	}
	if w := c.pendingConstraints[orig]; w != nil {
		// Reached again while evaluating the copy, as in a recursive schema.
		// Like any vertex being evaluated, it is returned as it stands.
		return w
	}
	w := &Vertex{
		IsPatternConstraint: true,
		Conjuncts:           slices.Clip(orig.Conjuncts),
	}
	if c.pendingConstraints == nil {
		c.pendingConstraints = make(map[*Vertex]*Vertex)
	}
	c.pendingConstraints[orig] = w
	w.Finalize(c)
	delete(c.pendingConstraints, orig)
	if w.Status() != finalized {
		// Left unfinished, so a later call would mutate it; do not share it.
		return w
	}
	if w.status != finalized {
		// A sharing vertex keeps the status its target had before it was
		// unified. Mark it done and release its state, as unify would have.
		w.ForceDone()
		c.reclaimTempBuffers(w)
	}
	if !pc.finalized.CompareAndSwap(nil, w) {
		w = pc.finalized.Load()
	}
	return w
}

// insertConstraint ensures a given pattern constraint is present in the
// constraints of n and reports whether the pair was added newly.
//
// The given conjunct must have a closeContext associated with it. This ensures
// that different pattern constraints pairs originating from the same
// closeContext will be collated properly in fields to which these constraints
// are applied.
func (n *nodeContext) insertConstraint(pattern Value, c Conjunct) bool {
	ctx := n.ctx
	v := n.node

	pcs := v.PatternConstraints
	if pcs == nil {
		pcs = &Constraints{}
		v.PatternConstraints = pcs
	}

	var constraint *Vertex
	for i := range pcs.Pairs {
		pc := &pcs.Pairs[i]
		if Equal(ctx, pc.Pattern, pattern, 0) {
			constraint = pc.Constraint
			break
		}
	}

	if constraint == nil {
		constraint = &Vertex{
			// See "Self-referencing patterns" in cycle.go
			IsPatternConstraint: true,
		}
		pcs.Pairs = append(pcs.Pairs, PatternConstraint{
			Pattern:    pattern,
			Constraint: constraint,
		})
	} else {
		for x := range constraint.LeafConjuncts() {
			if x.x == c.x && x.Env.Equal(ctx, c.Env) {
				// Both defIDs must stem from the current OpContext: x may
				// predate it, in which case its defID indexes another
				// context's containments table.
				if c.CloseInfo.opID == n.ctx.opID && x.CloseInfo.opID == n.ctx.opID {
					// TODO: do we need this replacement?
					src := x.CloseInfo.defID
					dst := c.CloseInfo.defID
					n.addReplacement(replaceID{from: dst, to: src})
				} else {
					n.ctx.stats.MisalignedConstraint++
				}
				// The constraint already existed and the conjunct was already added.
				return false
			}
		}
	}

	constraint.addConjunctUnchecked(c)
	return true
}

// matchPattern reports whether f matches pattern. The result reflects
// whether unification of pattern with f converted to a CUE value succeeds.
// The caller should check separately whether f matches any other arcs
// that are not covered by pattern.
func matchPattern(ctx *OpContext, pattern Value, f Feature) bool {
	if pattern == nil || !f.IsRegular() {
		return false
	}

	return matchPatternValue(ctx, pattern, f)
}

// matchPatternValue matches a concrete value against f.
//
// This is an optimization an intended to be faster than regular CUE evaluation
// for the majority of cases where pattern constraints are used.
func matchPatternValue(ctx *OpContext, pattern Value, f Feature) (result bool) {
	// Normally completePatternValue has already evaluated the pattern at
	// registration time; this finalize remains as a backstop.
	if v, ok := pattern.(*Vertex); ok {
		v.unify(ctx, Flags{condition: scalarKnown, mode: finalize, checkTypos: false})
	}
	pattern = Unwrap(pattern)

	k := IntKind
	if f.IsString() {
		k = StringKind
	}
	// This also rejects a *Bottom pattern, as its kind is BottomKind.
	// Its error was already added to the node by the task evaluating it.
	if !k.IsAnyOf(pattern.Kind()) {
		return false
	}

	// Fast track for the majority of cases.
	switch x := pattern.(type) {
	case *Top:
		return true

	case *BasicType:
		return x.K&k == k

	case *BoundValue:
		switch x.Kind() {
		case StringKind:
			if !f.IsString() || int64(f.Index()) == MaxIndex {
				return false
			}
			str := ctx.IndexToString(f.safeIndex())
			return x.validateStr(ctx, str)

		case NumberKind:
			return x.validateInt(ctx, int64(f.Index()))
		}

	case *Num:
		if !f.IsInt() {
			return false
		}
		yi := int64(f.Index())
		xi, err := x.X.Int64()
		return err == nil && xi == yi

	case *String:
		if !f.IsString() || int64(f.Index()) == MaxIndex {
			return false
		}
		str := ctx.IndexToString(f.safeIndex())
		return x.Str == str

	case *Conjunction:
		for _, a := range x.Values {
			if !matchPatternValue(ctx, a, f) {
				return false
			}
		}
		return true

	case *Disjunction:
		for _, a := range x.Values {
			if matchPatternValue(ctx, a, f) {
				return true
			}
		}
		return false
	}

	// Slow track.
	//
	// TODO(perf): if a pattern tree has many values that are not handled in the
	// fast track, it is probably more efficient to handle everything in the
	// slow track. One way to signal this would be to have a "value thunk" at
	// the root that causes the fast track to be bypassed altogether.

	label := f.ToValue(ctx)
	n := ctx.newInlineVertex(nil, nil,
		MakeConjunct(ctx.e, pattern, ctx.ci),
		MakeConjunct(ctx.e, label, ctx.ci))
	n.Finalize(ctx)
	return n.Err(ctx) == nil
}
