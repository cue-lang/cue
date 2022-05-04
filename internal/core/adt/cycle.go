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

// updateCyclicStatus looks for proof of non-cyclic conjuncts to override
// a structural cycle.
func (n *nodeContext) updateCyclicStatus(env *Environment) {
	if env == nil || !env.Cyclic {
		n.hasNonCycle = true
	}
}

func assertStructuralCycle(n *nodeContext) bool {
	if cyclic := n.hasCycle && !n.hasNonCycle; cyclic {
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
	// Don't process Arcs. This is mostly to ensure that no Arcs with
	// an Unprocessed status remain in the output.
	n.node.Arcs = nil
}

func makeAnonymousConjunct(env *Environment, x Expr) Conjunct {
	return Conjunct{
		env, x, CloseInfo{},
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
	return MakeConjunct(env, c.Elem(), c.CloseInfo)
}
