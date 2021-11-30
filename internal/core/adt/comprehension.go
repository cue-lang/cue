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

type envYield struct {
	env   *Environment
	yield Yielder
	id    CloseInfo
	err   *Bottom
}

// injectComprehensions evaluates and inserts comprehensions.
func (n *nodeContext) injectComprehensions(all *[]envYield) (progress bool) {
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

		if len(sa) == 0 {
			continue
		}
		id := d.id.SpawnSpan(d.yield, ComprehensionSpan)

		n.ctx.nonMonotonicInsertNest++
		for _, st := range sa {
			n.addStruct(st.env, st.s, id)
		}
		n.ctx.nonMonotonicInsertNest--
	}

	progress = k < len(*all)

	*all = (*all)[:k]

	return progress
}
