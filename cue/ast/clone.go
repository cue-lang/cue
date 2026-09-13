// Copyright 2026 The CUE Authors
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

package ast

import (
	"fmt"
	"reflect"
	"slices"
)

// Clone returns a deep copy of n.
//
// Nodes reachable more than once from n, for example a comment group
// which has different semantic and syntactic AST nodes, are cloned once
// and remain shared in the result.
//
// The [Ident.Scope] and [Ident.Node] fields of the cloned identifiers
// refer to the cloned nodes when the nodes they refer to are part of
// the tree rooted at n, and to the original nodes otherwise. The same
// holds for the identifiers in [File.Unresolved]. An identifier made
// by [NewPredeclared] stays predeclared in the clone.
//
// Clone returns the zero value of N if n is nil.
func Clone[N NilableNode](n N) N {
	// Every node is a pointer, so a nil node is either a nil
	// interface value or a nil pointer.
	if any(n) == nil || reflect.ValueOf(n).IsNil() {
		return *new(N)
	}
	c := &cloner{
		cloned: make(map[Node]Node),
	}
	n = clone(c, n)
	c.resolveReferences()
	return n
}

type cloner struct {
	// cloned maps each node encountered so far to its clone.
	cloned map[Node]Node

	// idents and files hold the clones whose references to
	// other nodes are remapped by resolveReferences.
	idents []*Ident
	files  []*File
}

// resolveReferences points the node references held by the cloned
// nodes at the clones of the nodes they refer to, when those are
// themselves part of the cloned tree.
func (c *cloner) resolveReferences() {
	for _, id := range c.idents {
		if n, ok := c.cloned[id.Scope]; ok {
			id.Scope = n
		}
		if n, ok := c.cloned[id.Node]; ok {
			id.Node = n
		}
	}
	for _, f := range c.files {
		for i, id := range f.Unresolved {
			if n, ok := c.cloned[id].(*Ident); ok {
				f.Unresolved[i] = n
			}
		}
	}
}

func (c *cloner) node(node Node) Node {
	if x, ok := c.cloned[node]; ok {
		return x
	}

	// The cases are ordered lexically by type name.
	switch n := node.(type) {
	case *Alias:
		x := shallow(c, n)
		x.Ident = clone(c, n.Ident)
		x.Expr = clone(c, n.Expr)
		return x

	case *Attribute:
		return shallow(c, n)

	case *BadDecl:
		return shallow(c, n)

	case *BadExpr:
		return shallow(c, n)

	case *BasicLit:
		return shallow(c, n)

	case *BinaryExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		x.Y = clone(c, n.Y)
		return x

	case *BottomLit:
		return shallow(c, n)

	case *CallExpr:
		x := shallow(c, n)
		x.Fun = clone(c, n.Fun)
		x.Args = cloneList(c, n.Args)
		x.ArgLabels = cloneList(c, n.ArgLabels)
		return x

	case *Comment:
		return shallow(c, n)

	case *CommentGroup:
		x := shallow(c, n)
		x.List = cloneList(c, n.List)
		return x

	case *Comprehension:
		x := shallow(c, n)
		x.Clauses = cloneList(c, n.Clauses)
		x.Value = clone(c, n.Value)
		x.Fallback = clone(c, n.Fallback)
		return x

	case *Ellipsis:
		x := shallow(c, n)
		x.Type = clone(c, n.Type)
		return x

	case *EmbedDecl:
		x := shallow(c, n)
		x.Expr = clone(c, n.Expr)
		return x

	case *FallbackClause:
		x := shallow(c, n)
		x.Body = clone(c, n.Body)
		return x

	case *Field:
		x := shallow(c, n)
		x.Label = clone(c, n.Label)
		x.Alias = clone(c, n.Alias)
		x.Value = clone(c, n.Value)
		x.Attrs = cloneList(c, n.Attrs)
		return x

	case *File:
		x := shallow(c, n)
		c.files = append(c.files, x)
		x.Decls = cloneList(c, n.Decls)
		x.Unresolved = slices.Clone(n.Unresolved)
		return x

	case *ForClause:
		x := shallow(c, n)
		x.Key = clone(c, n.Key)
		x.Value = clone(c, n.Value)
		x.Source = clone(c, n.Source)
		return x

	case *Func:
		x := shallow(c, n)
		x.Params = cloneList(c, n.Params)
		x.Args = cloneList(c, n.Args)
		x.Ret = clone(c, n.Ret)
		x.Body = clone(c, n.Body)
		return x

	case *FuncParam:
		x := shallow(c, n)
		x.Label = clone(c, n.Label)
		x.Alias = clone(c, n.Alias)
		x.Value = clone(c, n.Value)
		x.Default = clone(c, n.Default)
		x.Attrs = cloneList(c, n.Attrs)
		return x

	case *Ident:
		x := shallow(c, n)
		c.idents = append(c.idents, x)
		return x

	case *IfClause:
		x := shallow(c, n)
		x.Condition = clone(c, n.Condition)
		return x

	case *ImportDecl:
		x := shallow(c, n)
		x.Specs = cloneList(c, n.Specs)
		return x

	case *ImportSpec:
		x := shallow(c, n)
		x.Name = clone(c, n.Name)
		x.Path = clone(c, n.Path)
		return x

	case *IndexExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		x.Index = clone(c, n.Index)
		return x

	case *Interpolation:
		x := shallow(c, n)
		x.Elts = cloneList(c, n.Elts)
		return x

	case *LetClause:
		x := shallow(c, n)
		x.Ident = clone(c, n.Ident)
		x.Expr = clone(c, n.Expr)
		return x

	case *ListLit:
		x := shallow(c, n)
		x.Elts = cloneList(c, n.Elts)
		return x

	case *Package:
		x := shallow(c, n)
		x.Name = clone(c, n.Name)
		return x

	case *ParenExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		return x

	case *PostfixAlias:
		x := shallow(c, n)
		x.Label = clone(c, n.Label)
		x.Field = clone(c, n.Field)
		return x

	case *PostfixExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		return x

	case *SelectorExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		x.Sel = clone(c, n.Sel)
		return x

	case *SliceExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		x.Low = clone(c, n.Low)
		x.High = clone(c, n.High)
		return x

	case *StructLit:
		x := shallow(c, n)
		x.Elts = cloneList(c, n.Elts)
		return x

	case *TryClause:
		x := shallow(c, n)
		x.Ident = clone(c, n.Ident)
		x.Expr = clone(c, n.Expr)
		return x

	case *UnaryExpr:
		x := shallow(c, n)
		x.X = clone(c, n.X)
		return x

	default:
		panic(fmt.Sprintf("Clone: unexpected node type %T", n))
	}
}

// clone returns a clone of n, which may be a nil interface value.
func clone[N NilableNode](c *cloner, n N) N {
	if n == *new(N) {
		return n
	}
	return c.node(n).(N)
}

// cloneList returns a clone of the list.
func cloneList[N NilableNode](c *cloner, list []N) []N {
	if list == nil {
		return nil
	}
	a := make([]N, len(list))
	for i, n := range list {
		a[i] = clone(c, n)
	}
	return a
}

// shallow returns a shallow copy of n with its comments cloned,
// recording it as the clone of n before its children are cloned.
func shallow[T any, PT interface {
	*T
	Node
}](c *cloner, n PT) PT {
	x := PT(new(T))
	*x = *n
	c.cloned[n] = x
	cloneComments(c, x.commentInfo())
	return x
}

// cloneComments replaces the comments held by info, which may be nil,
// with clones of themselves.
func cloneComments(c *cloner, info *comments) {
	if info == nil {
		return
	}
	info.syntacticGroups = cloneGroups(c, info.syntacticGroups)
	info.inheritedDocComments = cloneGroups(c, info.inheritedDocComments)
}

func cloneGroups(c *cloner, groups *[]*CommentGroup) *[]*CommentGroup {
	if groups == nil {
		return nil
	}
	return new(cloneList(c, *groups))
}
