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
// Comprehensions are expanded for, if, and let clauses that yield 0 or more
// structs to be embedded in the enclosing list or struct.
//
// CUE allows cascading of insertions, as in:
//
//     a?: int
//     b?: int
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
// for fields with a fixed prefix path in a comprehension value, the
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
// Note that a single comprehension may be distributed across multiple fields.
// The evaluator will ensure, however, that a comprehension is only evaluated
// once.
//
//
// Closedness
//
// The comprehension algorithm uses the usual closedness mechanism for marking
// fields that belong to a struct: it adds the StructLit associated with the
// comprehension value to the respective arc.
//
// One noteworthy point is that the fields of a struct are only legitimate for
// actual results. For instance, if an if clause evaluates to false, the
// value is not embedded.
//
// To account for this, the comprehension algorithm relies on the fact that
// the closedness information is computed as a separate step. So even if
// the StructLit is added early, its fields will only count once it is
// initialized, which is only done when at least one result is added.
//

// envComprehension caches the result of a single comprehension.
type envComprehension struct {
	comp   *Comprehension
	vertex *Vertex // The Vertex from which the comprehension originates.

	// runtime-related fields

	// envs holds all the environments that define a single "yield" result in
	// combination with the comprehension struct.
	envs []*Environment // nil: unprocessed, non-nil: done.
	done bool           // true once the comprehension has been evaluated

	// StructLits to Init (activate for closedness check)
	// when at least one value is yielded.
	structs []*StructLit
}

// addEnv is used as a [YieldFunc] so that we don't need to create a new func
// value for each comprehension.
func (e *envComprehension) addEnv(env *Environment) {
	e.envs = append(e.envs, env)
}

// insertComprehension registers a comprehension with a node, possibly pushing
// down its evaluation to the node's children. It will only evaluate one level
// of fields at a time.
func (n *nodeContext) insertComprehension(
	env *Environment,
	c *Comprehension,
	ci CloseInfo,
) {

	ec := c.comp
	if ec == nil {
		ec = &envComprehension{
			comp:   c,
			vertex: n.node,
		}
	}

	x := c.Value

	node := n.node.DerefDisjunct()

	switch v := ToExpr(x).(type) {
	case *StructLit:
		ci = n.splitStruct(v, ci)

		kind := TopKind
		numFixed := 0
		for _, d := range v.Decls {
			switch f := d.(type) {
			case *Field:
				numFixed++

				if f.Label.IsInt() {
					kind &= ListKind
				} else if f.Label.IsString() {
					kind &= StructKind
				}

				// Create partial comprehension
				partialComp := &Comprehension{
					Syntax:  c.Syntax,
					Clauses: c.Clauses,
					Value:   f,
					arcType: f.ArcType, // TODO: can be derived, remove this field.

					comp:   ec,
					parent: c,
					arc:    node,
				}

				conjunct := MakeConjunct(env, partialComp, ci)
				n.assertInitialized()
				n.insertArc(f.Label, ArcPending, conjunct, conjunct.CloseInfo, false)

			case *LetField:
				// TODO: consider merging this case with the LetField case.

				numFixed++

				// Create partial comprehension
				partialComp := &Comprehension{
					Syntax:  c.Syntax,
					Clauses: c.Clauses,
					Value:   f,

					comp:   ec,
					parent: c,
					arc:    node,
				}

				conjunct := MakeConjunct(env, partialComp, ci)
				n.assertInitialized()
				arc := n.insertFieldUnchecked(f.Label, ArcMember, conjunct)
				arc.MultiLet = true // NOTE: v2 was f.IsMulti
			}
		}

		if numFixed > 0 {
			// Create a stripped struct that only includes fixed fields.
			// TODO(perf): this StructLit may be inserted more than once in
			// the same vertex: once taking the StructLit of the referred node
			// and once for inserting the Conjunct of the original node.
			// Is this necessary (given closedness rules), and is this posing
			// a performance problem?
			st := v
			if numFixed < len(v.Decls) {
				// Mixed case: second pass to split field decls
				// from non-field decls. This only runs when
				// 0 < numFixed < len(v.Decls).
				fields := make([]Decl, 0, numFixed)
				decls := make([]Decl, 0, len(v.Decls)-numFixed)
				for _, d := range v.Decls {
					switch d.(type) {
					case *Field, *LetField:
						fields = append(fields, d)
					default:
						decls = append(decls, d)
					}
				}
				st = &StructLit{
					Src:             v.Src,
					Decls:           fields,
					isComprehension: true,
				}
				x = &StructLit{
					Decls:           decls,
					isComprehension: true,
				}
			}
			node.AddStruct(st)
			switch {
			case !ec.done:
				ec.structs = append(ec.structs, st)
			case len(ec.envs) > 0:
				st.Init(n.ctx)
				if kind == StructKind || kind == ListKind {
					n.updateNodeType(kind, st, ci)
				}
			}
		}

		c.kind = kind

		switch numFixed {
		case 0:
			// Add comprehension as is.

		case len(v.Decls):
			// No comprehension to add at this level.
			// The should be considered a struct if it has only non-regular
			// fields (like definitions), and no embeddings.
			if kind == TopKind {
				c.kind = StructKind
			}
			// If there's an else clause, we still need to schedule a task
			// to handle the fallback case when comprehension yields zero values.
			if c.Fallback == nil {
				return
			}
			// Use an empty struct as the main value since all fields were
			// handled at field level. The else clause will be embedded if
			// the comprehension yields zero values.
			x = &StructLit{
				isComprehension: true,
			}

		default:
			// x was already set in the mixed-case block above.
		}
	}

	t := n.scheduleTask(handleComprehension, env, x, ci)
	t.comp = ec
	t.leaf = c
}

type compState struct {
	ctx   *OpContext
	comp  *Comprehension
	i     int
	f     YieldFunc
	state vertexStatus
}

// yield evaluates a Comprehension within the given Environment and calls
// f for each result.
func (c *OpContext) yield(
	node *Vertex, // errors are associated with this node
	env *Environment, // env for field for which this yield is called
	comp *Comprehension,
	state Flags,
	f YieldFunc, // called for every result
) *Bottom {
	s := &compState{
		ctx:   c,
		comp:  comp,
		f:     f,
		state: state.status,
	}
	y := comp.Clauses[0]

	saved := c.PushState(env, y.Source())
	if node != nil {
		defer c.PopArc(c.PushArc(node))
	}

	s.i++
	y.yield(s)
	s.i--

	return c.PopState(saved)
}

func (s *compState) yield(env *Environment) (ok bool) {
	c := s.ctx
	if s.i >= len(s.comp.Clauses) {
		s.f(env)
		return true
	}
	dst := s.comp.Clauses[s.i]
	saved := c.PushState(env, dst.Source())

	s.i++
	dst.yield(s)
	s.i--

	if b := c.PopState(saved); b != nil {
		c.AddBottom(b)
		return false
	}
	return !c.HasErr()
}

// processComprehension processes a single Comprehension conjunct.
// It returns an incomplete error if there was one. Fatal errors are
// processed as a "successfully" completed computation.
func (n *nodeContext) processComprehension(t *task, state vertexStatus) *Bottom {
	ctx := n.ctx

	ec := t.comp

	// Compute environments, if needed.
	if !ec.done {
		if err := ctx.yield(ec.vertex, t.env, ec.comp, Flags{
			status:    state,
			condition: allKnown,
			mode:      ignore,
		}, ec.addEnv); err != nil {
			if err.IsIncomplete() {
				return err
			}

			// continue to collect other errors.
			ec.done = true
			if ec.vertex != nil {
				ec.vertex.state.addBottom(err)
				ctx.PopArc(ec.vertex)
			}
			return nil
		}

		if len(ec.envs) > 0 {
			for _, s := range ec.structs {
				s.Init(n.ctx)
			}
		}
		ec.structs = nil
		ec.done = true
	}

	if len(ec.envs) == 0 {
		// If there's an else clause, use it instead of marking arc as not present.
		if t.leaf.Fallback != nil {
			// Evaluate the else clause in the outer environment.
			// We use linkChildren to properly chain the environment, similar to
			// normal comprehension yield processing.
			env := linkChildren(t.env, t.leaf)
			n.scheduleConjunct(Conjunct{env, t.leaf.Fallback, t.id}, t.id)
			return nil
		}
		n.node.updateArcType(ArcNotPresent)
		return nil
	}

	v := n.node
	for c := t.leaf; c.parent != nil; c = c.parent {
		v = n.ctx.deref(v)
		v.updateArcType(c.arcType)
		if v.ArcType == ArcNotPresent {
			parent := v.Parent
			b := parent.reportFieldCycleError(ctx, ec.comp.Syntax.Pos(), v.Label)
			ec.vertex.state.addBottom(b)
			ctx.current().err = b
			ctx.current().state = taskFAILED
			return nil
		}
		if k := c.kind; k == StructKind || k == ListKind {
			v := v.DerefDisjunct()
			if s := v.getBareState(n.ctx); s != nil {
				s.updateNodeType(k, ToExpr(c.Value), t.id)
			}
		}
		v = c.arc
	}

	id := t.id
	// TODO: should we treat comprehension values as optional?
	// It seems so, but it causes some hangs.
	// id.setOptional(nil)

	for _, env := range ec.envs {
		if n.node.ArcType == ArcNotPresent {
			b := n.node.reportFieldCycleError(ctx, ec.comp.Syntax.Pos(), n.node.Label)
			ctx.current().err = b
			n.yield()
			return nil
		}

		env = linkChildren(env, t.leaf)

		n.scheduleConjunct(Conjunct{env, t.x, id}, id)
	}

	return nil
}

// linkChildren adds environments for the chain of vertices to a result
// environment.
func linkChildren(env *Environment, c *Comprehension) *Environment {
	if c.parent != nil {
		env = linkChildren(env, c.parent)
		env = spawn(env, c.arc)
	}
	return env
}
