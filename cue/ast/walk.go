// Copyright 2018 The CUE Authors
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

	"cuelang.org/go/cue/token"
)

// Walk traverses an AST in depth-first order: It starts by calling f(node);
// node must not be nil. If before returns true, Walk invokes f recursively for
// each of the non-nil children of node, followed by a call of after. Both
// functions may be nil. If before is nil, it is assumed to always return true.
func Walk(node Node, before func(Node) bool, after func(Node)) {
	walk(&inspector{before: before, after: after}, node)
}

// WalkVisitor traverses an AST in depth-first order with a [Visitor].
func WalkVisitor(node Node, visitor Visitor) {
	walk(visitor, node)
}

// A Visitor's before method is invoked for each node encountered by Walk.
// If the result Visitor w is true, Walk visits each of the children
// of node with the Visitor w, followed by a call of w.After.
type Visitor interface {
	Before(node Node) (w Visitor)
	After(node Node)
}

func walkList[N Node](v Visitor, list []N) {
	for _, node := range list {
		walk(v, node)
	}
}

// walk traverses an AST in depth-first order: It starts by calling
// v.Visit(node); node must not be nil. If the visitor w returned by
// v.Visit(node) is not nil, walk is invoked recursively with visitor
// w for each of the non-nil children of node, followed by a call of
// w.Visit(nil).
func walk(v Visitor, node Node) {
	if v = v.Before(node); v == nil {
		return
	}

	// TODO: record the comment groups and interleave with the values like for
	// parsing and printing?
	walkList(v, Comments(node))

	// walk children
	// (the order of the cases matches the order
	// of the corresponding node types in go)
	switch n := node.(type) {
	// Comments and fields
	case *Comment:
		// nothing to do

	case *CommentGroup:
		walkList(v, n.List)

	case *Attribute:
		// nothing to do

	case *Field:
		walk(v, n.Label)
		if n.Value != nil {
			walk(v, n.Value)
		}
		walkList(v, n.Attrs)

	case *Func:
		walkList(v, n.Args)
		walk(v, n.Ret)

	case *StructLit:
		walkList(v, n.Elts)

	// Expressions
	case *BottomLit, *BadExpr, *Ident, *BasicLit:
		// nothing to do

	case *Interpolation:
		walkList(v, n.Elts)

	case *ListLit:
		walkList(v, n.Elts)

	case *Ellipsis:
		if n.Type != nil {
			walk(v, n.Type)
		}

	case *ParenExpr:
		walk(v, n.X)

	case *SelectorExpr:
		walk(v, n.X)
		walk(v, n.Sel)

	case *IndexExpr:
		walk(v, n.X)
		walk(v, n.Index)

	case *SliceExpr:
		walk(v, n.X)
		if n.Low != nil {
			walk(v, n.Low)
		}
		if n.High != nil {
			walk(v, n.High)
		}

	case *CallExpr:
		walk(v, n.Fun)
		walkList(v, n.Args)

	case *UnaryExpr:
		walk(v, n.X)

	case *BinaryExpr:
		walk(v, n.X)
		walk(v, n.Y)

	// Declarations
	case *ImportSpec:
		if n.Name != nil {
			walk(v, n.Name)
		}
		walk(v, n.Path)

	case *BadDecl:
		// nothing to do

	case *ImportDecl:
		walkList(v, n.Specs)

	case *EmbedDecl:
		walk(v, n.Expr)

	case *LetClause:
		walk(v, n.Ident)
		walk(v, n.Expr)

	case *Alias:
		walk(v, n.Ident)
		walk(v, n.Expr)

	case *Comprehension:
		walkList(v, n.Clauses)
		walk(v, n.Value)

	// Files and packages
	case *File:
		walkList(v, n.Decls)

	case *Package:
		walk(v, n.Name)

	case *ForClause:
		if n.Key != nil {
			walk(v, n.Key)
		}
		walk(v, n.Value)
		walk(v, n.Source)

	case *IfClause:
		walk(v, n.Condition)

	default:
		panic(fmt.Sprintf("Walk: unexpected node type %T", n))
	}

	v.After(node)
}

type inspector struct {
	before func(Node) bool
	after  func(Node)

	commentStack []commentFrame
	current      commentFrame
}

type commentFrame struct {
	cg  []*CommentGroup
	pos int8
}

func (f *inspector) Before(node Node) Visitor {
	if f.before == nil || f.before(node) {
		f.commentStack = append(f.commentStack, f.current)
		f.current = commentFrame{cg: Comments(node)}
		f.visitComments(f.current.pos)
		return f
	}
	return nil
}

func (f *inspector) After(node Node) {
	f.visitComments(127)
	p := len(f.commentStack) - 1
	f.current = f.commentStack[p]
	f.commentStack = f.commentStack[:p]
	f.current.pos++
	if f.after != nil {
		f.after(node)
	}
}

func (f *inspector) Token(t token.Token) {
	f.current.pos++
}

func (f *inspector) visitComments(pos int8) {
	c := &f.current
	for ; len(c.cg) > 0; c.cg = c.cg[1:] {
		cg := c.cg[0]
		if cg.Position == pos {
			continue
		}
		if f.before == nil || f.before(cg) {
			for _, c := range cg.List {
				if f.before == nil || f.before(c) {
					if f.after != nil {
						f.after(c)
					}
				}
			}
			if f.after != nil {
				f.after(cg)
			}
		}
	}
}
