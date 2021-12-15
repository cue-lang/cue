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

// Comprehension algorithm
//
// Comprehensions are expand for, if, and let clauses that yield 0 or more
// structs to be embedded in the enclosing list or struct.
//
// CUE allows cascading of insertions, as in:
//
//     a?: int
//     if a != _|_ {
//         b: 2
//     }
//     if b != _|_ {
//         c: 3
//         d: 4
//     }
//
// even though CUE does not allow the result of a comprehension to depend
// on another comprehension within a single struct. The way this works is that
// for fields with a fixed prefix path in a comprehension value are, the
// comprehension is assigned to these respective fields.
//
// More concretely, the above example is rewritten to:
//
//    a?: int
//    b: if a != _|_ { 2 }
//    c: if b != _|_ { 3 }
//    d: if b != _|_ { 4 }
//
// where the fields with if clause are only inserted if their condition
// resolves to true. (Note that this is not valid CUE; it may be in the future.)
//
// With this rewrite, any dependencies in comprehension expressions will follow
// the same rules, more or less, as with normal evaluation.
//
// Note that a singe comprehension may be distributed across multiple fields.
// The evaluator will ensure, however, that a comprehension is only evaluated
// once.
//
//
// Closedness
//
// The comprehension algorithm uses the usual closedness mechanism for marking
// fields to belong to a struct: it adds the StructLit associated with the
// comprehension value to the respective arc.
//
// One noteworthy point is that the fields of a struct are only legitimate for
// actually results. For instance, if an if clause evaluates to false, the
// value is not embedded.
//
// To account for this, the comprehension algorithm relies on the fact that
// the closedness information is computed as a separate step. So even if
// the StructLit is added early, its fields will only count once it is
// initialized, which is only done when at least one result is added.
//

// envComprehension caches the result of a single comprehension.
type envComprehension struct {
	comp *Comprehension
	node *Vertex

	// runtime
	err  *Bottom
	envs []*Environment // nil: unprocessed, non-nil: done.
	done bool

	// StructLits to Init (activate for closedness check)
	// when at least one value is yielded.
	structs []*StructLit
}

// envYield defines a comprehension for a specific field within a comprehension
// value. Multiple envYields can be associated with a single envComprehension.
type envYield struct {
	*envComprehension

	env  *Environment
	id   CloseInfo
	expr Expr
	nest int
}

// insertComprehension registers a comprehension with a node, possibly pushing
// down its evaluation to its children. It will only evaluate one level of
// fields at a time.
func (n *nodeContext) insertComprehension(
	env *Environment,
	c *Comprehension,
	ci CloseInfo) {

	ec := c.comp
	if ec == nil {
		ec = &envComprehension{
			comp: c,
			node: n.node,

			err:  nil,   // shut up linter
			envs: nil,   // shut up linter
			done: false, // shut up linter
		}
	}

	if ec.done && len(ec.envs) == 0 {
		return
	}

	x := c.Value

	ci = ci.SpawnSpan(c, ComprehensionSpan)

	switch v := x.(type) {
	case *StructLit:
		numFixed := 0
		var fields []Decl
		for _, d := range v.Decls {
			switch f := d.(type) {
			case *Field:
				numFixed++

				arc, _ := n.node.GetArc(n.ctx, f.Label, arcVoid)

				// Create partial comprehension
				c := &Comprehension{
					Clauses: c.Clauses,
					Value:   f.expr(),

					comp: ec,
					Nest: c.Nest + 1,
				}

				arc.addConjunctUnchecked(MakeConjunct(env, c, ci))
				fields = append(fields, f)
				// TODO: adjust ci to embed?

				// TODO: this also needs to be done for optional fields.
			}
		}

		if len(fields) > 0 {
			// Create a stripped struct that only includes
			// TODO(perf): this StructLit may be inserted more than once in
			// the same vertex: once taking the StructLit of the referred node
			// and once for inserting the Conjunct of the original node.
			// Is this necessary (given closedness rules), and is this posing
			// a performance problem?
			st := v
			if len(fields) < len(v.Decls) {
				st = &StructLit{
					Src:   v.Src,
					Decls: fields,
				}
			}
			n.node.AddStruct(st, env, ci)
			switch {
			case !ec.done:
				ec.structs = append(ec.structs, st)
			case len(ec.envs) > 0:
				st.Init()
			}
		}

		switch numFixed {
		case 0:
			// Add comprehension as is.

		case len(v.Decls):
			// No comprehension to add at this level.
			return

		default:
			// Create a new StructLit with only the fields that need to be
			// added at this level.
			s := &StructLit{Decls: make([]Decl, 0, len(v.Decls)-numFixed)}
			for _, d := range v.Decls {
				switch d.(type) {
				case *Field:
				default:
					s.Decls = append(s.Decls, d)
				}
			}
			x = s
		}
	}

	n.comprehensions = append(n.comprehensions, envYield{
		envComprehension: ec,
		env:              env,
		id:               ci,
		expr:             x,
		nest:             c.Nest,
	})
}

// injectComprehension evaluates and inserts embeddings. It first evaluates all
// embeddings before inserting the results to ensure that the order of
// evaluation does not matter.
func (n *nodeContext) injectComprehensions(all *[]envYield) (progress bool) {
	ctx := n.ctx

	k := 0
	for i := 0; i < len(*all); i++ {
		d := (*all)[i]

		// Compute environments, if needed.
		if !d.done {
			f := func(env *Environment) {
				d.envs = append(d.envs, env)
			}

			if err := ctx.Yield(d.env, d.comp, f); err != nil {
				if err.IsIncomplete() {
					d.err = err
					(*all)[k] = d
					k++

					// // Add comprehension to ensure incomplete error is inserted.
					// // This ensures that the error is reported in the Vertex
					// // where the comprehension was defined, and not just in the
					// // node below. This, in turn, is necessary to support
					// // certain logic, like export, that expects to be able to
					// // detect an "incomplete" error at the first level where it
					// // is necessary.
					// n := d.node.getNodeContext(ctx)
					// n.addBottom(err)
				} else {
					// continue to collect other errors.
					n.addBottom(err)
					d.done = true
				}
				continue
			}

			if len(d.envs) > 0 {
				for _, s := range d.structs {
					s.Init()
				}
			}
			d.structs = nil
		}

		d.done = true

		if len(d.envs) == 0 {
			continue
		}

		id := d.id

		for _, env := range d.envs {
			if d.nest > 0 {
				env = linkChildren(env, n.node.Parent, d.nest-1)
			}
			n.addExprConjunct(Conjunct{env, d.expr, id})
		}
	}

	progress = k < len(*all)

	*all = (*all)[:k]

	return progress
}

func linkChildren(env *Environment, v *Vertex, nest int) *Environment {
	if nest > 0 {
		env = linkChildren(env, v.Parent, nest-1)
	}
	env = spawn(env, v)
	return env
}
