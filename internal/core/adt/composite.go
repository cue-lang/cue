// Copyright 2020 CUE Authors
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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// An Environment links the parent scopes for identifier lookup to a composite
// node. Each conjunct that make up node in the tree can be associated with
// a different environment (although some conjuncts may share an Environment).
type Environment struct {
	Up     *Environment
	Vertex *Vertex
}

// A Vertex is a node in the value tree. It may be a leaf or internal node.
// It may have arcs to represent elements of a fully evaluated struct or list.
//
// For structs, it only contains definitions and concrete fields.
// optional fields are dropped.
//
// It maintains source information such as a list of conjuncts that contributed
// to the value.
type Vertex struct {
	Parent *Vertex // Do we need this?

	// Label is the feature leading to this vertex.
	Label Feature

	// Value is the value associated with this vertex. For lists and structs
	// this is a sentinel value indicating its kind.
	Value Value

	// The parent of nodes can be followed to determine the path within the
	// configuration of this node.
	// Value  Value
	Arcs []*Vertex // arcs are sorted in display order.

	// Conjuncts lists the structs that ultimately formed this Composite value.
	// This includes all selected disjuncts. This information is used to compute
	// the topological sort of arcs.
	Conjuncts []Conjunct

	// Structs is a slice of struct literals that contributed to this value.
	Structs []*StructLit
}

func (v *Vertex) Kind() Kind {
	// This is possible when evaluating comprehensions. It is potentially
	// not known at this time what the type is.
	if v.Value == nil {
		return TopKind
	}
	return v.Value.Kind()
}

func (v *Vertex) IsList() bool {
	_, ok := v.Value.(*ListMarker)
	return ok
}

// lookup returns the Arc with label f if it exists or nil otherwise.
func (v *Vertex) lookup(f Feature) *Vertex {
	for _, a := range v.Arcs {
		if a.Label == f {
			return a
		}
	}
	return nil
}

// GetArc returns a Vertex for the outgoing arc with label f. It creates and
// ads one if it doesn't yet exist.
func (v *Vertex) GetArc(f Feature) (arc *Vertex, isNew bool) {
	arc = v.lookup(f)
	if arc == nil {
		arc = &Vertex{Parent: v, Label: f}
		v.Arcs = append(v.Arcs, arc)
		isNew = true
	}
	return arc, isNew
}

func (v *Vertex) Source() ast.Node { return nil }

// AddConjunct adds the given Conjuncts to v if it doesn't already exist.
func (v *Vertex) AddConjunct(c Conjunct) *Bottom {
	for _, x := range v.Conjuncts {
		if x == c {
			return nil
		}
	}
	if v.Value != nil {
		// This is likely a bug in the evaluator and should not happen.
		return &Bottom{Err: errors.Newf(token.NoPos, "cannot add conjunct")}
	}
	v.Conjuncts = append(v.Conjuncts, c)
	return nil
}

// Path computes the sequence of Features leading from the root to of the
// instance to this Vertex.
func (v *Vertex) Path() []Feature {
	return appendPath(nil, v)
}

func appendPath(a []Feature, v *Vertex) []Feature {
	if v.Parent == nil {
		return a
	}
	a = appendPath(a, v.Parent)
	return append(a, v.Label)
}

// An Conjunct is an Environment-Expr pair. The Environment is the starting
// point for reference lookup for any reference contained in X.
type Conjunct struct {
	Env *Environment
	x   Node
}

// TODO(perf): replace with composite literal if this helps performance.

// MakeConjunct creates a conjunct from the given environment and node.
// It panics if x cannot be used as an expression.
func MakeConjunct(env *Environment, x Node) Conjunct {
	switch x.(type) {
	case Expr, interface{ expr() Expr }:
	default:
		panic("invalid Node type")
	}
	return Conjunct{env, x}
}

func (c *Conjunct) Source() ast.Node {
	return c.x.Source()
}

func (c *Conjunct) Expr() Expr {
	switch x := c.x.(type) {
	case Expr:
		return x
	case interface{ expr() Expr }:
		return x.expr()
	default:
		panic("unreachable")
	}
}
