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
	v := &inspector{before: before, after: after}
	walk(node, v.Before, v.After)
}

// WalkVisitor traverses an AST in depth-first order with a [Visitor].
func WalkVisitor(node Node, visitor Visitor) {
	v := &stackVisitor{stack: []Visitor{visitor}}
	walk(node, v.Before, v.After)
}

// stackVisitor helps implement Visitor support on top of Walk.
type stackVisitor struct {
	stack []Visitor
}

func (v *stackVisitor) Before(node Node) bool {
	current := v.stack[len(v.stack)-1]
	next := current.Before(node)
	if next == nil {
		return false
	}
	v.stack = append(v.stack, next)
	return true
}

func (v *stackVisitor) After(node Node) {
	v.stack[len(v.stack)-1] = nil // set visitor to nil so it can be garbage collected
	v.stack = v.stack[:len(v.stack)-1]
}

// A Visitor's before method is invoked for each node encountered by Walk.
// If the result Visitor w is true, Walk visits each of the children
// of node with the Visitor w, followed by a call of w.After.
type Visitor interface {
	Before(node Node) (w Visitor)
	After(node Node)
}

func walkList[N Node](list []N, before func(Node) bool, after func(Node)) {
	for _, node := range list {
		walk(node, before, after)
	}
}

func walk(node Node, before func(Node) bool, after func(Node)) {
	if !before(node) {
		return
	}

	// TODO: record the comment groups and interleave with the values like for
	// parsing and printing?
	walkList(Comments(node), before, after)

	// walk children
	// (the order of the cases matches the order
	// of the corresponding node types in go)
	switch n := node.(type) {
	// Comments and fields
	case *Comment:
		// nothing to do

	case *CommentGroup:
		walkList(n.List, before, after)

	case *Attribute:
		// nothing to do

	case *Field:
		walk(n.Label, before, after)
		if n.Value != nil {
			walk(n.Value, before, after)
		}
		walkList(n.Attrs, before, after)

	case *Func:
		walkList(n.Args, before, after)
		walk(n.Ret, before, after)

	case *StructLit:
		walkList(n.Elts, before, after)

	// Expressions
	case *BottomLit, *BadExpr, *Ident, *BasicLit:
		// nothing to do

	case *Interpolation:
		walkList(n.Elts, before, after)

	case *ListLit:
		walkList(n.Elts, before, after)

	case *Ellipsis:
		if n.Type != nil {
			walk(n.Type, before, after)
		}

	case *ParenExpr:
		walk(n.X, before, after)

	case *SelectorExpr:
		walk(n.X, before, after)
		walk(n.Sel, before, after)

	case *IndexExpr:
		walk(n.X, before, after)
		walk(n.Index, before, after)

	case *SliceExpr:
		walk(n.X, before, after)
		if n.Low != nil {
			walk(n.Low, before, after)
		}
		if n.High != nil {
			walk(n.High, before, after)
		}

	case *CallExpr:
		walk(n.Fun, before, after)
		walkList(n.Args, before, after)

	case *UnaryExpr:
		walk(n.X, before, after)

	case *BinaryExpr:
		walk(n.X, before, after)
		walk(n.Y, before, after)

	// Declarations
	case *ImportSpec:
		if n.Name != nil {
			walk(n.Name, before, after)
		}
		walk(n.Path, before, after)

	case *BadDecl:
		// nothing to do

	case *ImportDecl:
		walkList(n.Specs, before, after)

	case *EmbedDecl:
		walk(n.Expr, before, after)

	case *LetClause:
		walk(n.Ident, before, after)
		walk(n.Expr, before, after)

	case *Alias:
		walk(n.Ident, before, after)
		walk(n.Expr, before, after)

	case *Comprehension:
		walkList(n.Clauses, before, after)
		walk(n.Value, before, after)

	// Files and packages
	case *File:
		walkList(n.Decls, before, after)

	case *Package:
		walk(n.Name, before, after)

	case *ForClause:
		if n.Key != nil {
			walk(n.Key, before, after)
		}
		walk(n.Value, before, after)
		walk(n.Source, before, after)

	case *IfClause:
		walk(n.Condition, before, after)

	default:
		panic(fmt.Sprintf("Walk: unexpected node type %T", n))
	}

	after(node)
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

func (f *inspector) Before(node Node) bool {
	if f.before == nil || f.before(node) {
		f.commentStack = append(f.commentStack, f.current)
		f.current = commentFrame{cg: Comments(node)}
		f.visitComments(f.current.pos)
		return true
	}
	return false
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
