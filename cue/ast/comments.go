// Copyright 2019 CUE Authors
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

import "slices"

// Comments returns all comments associated with a given node.
func Comments(n Node) []*CommentGroup {
	c := n.commentInfo()
	if c == nil {
		return nil
	}
	return c.Comments()
}

// AddComment adds the given comment to the node if it supports it.
// If a node does not support comments, such as for CommentGroup or Comment,
// this call has no effect.
func AddComment(n Node, cg *CommentGroup) {
	c := n.commentInfo()
	if c == nil {
		return
	}
	c.AddComment(cg)
}

// SetComments replaces all comments of n with the given set of comments.
// If a node does not support comments, such as for CommentGroup or Comment,
// this call has no effect.
func SetComments(n Node, cgs []*CommentGroup) {
	c := n.commentInfo()
	if c == nil {
		return
	}
	c.SetComments(cgs)
}

// DocComments returns the doc comments that semantically document n,
// resolving the field-chain convention: a doc comment textually
// attached to the head of a field-chain documents the innermost
// (leaf) field. E.g.
//
//	// c
//	a: b: {x: 1}
//
// DocComments reports "// c" for b, and nothing for a.
//
// The result is only accurate after [ResolveComments] has run for the
// enclosing (sub)tree; [parser.ParseFile] and [parser.ParseExpr] do
// so automatically when [parser.ParseComments] is set.
func DocComments(n Node) []*CommentGroup {
	c := n.commentInfo()
	if c == nil {
		return nil
	}

	var docs []*CommentGroup
	if c.inheritedDocComments != nil {
		docs = append(docs, *c.inheritedDocComments...)
	}

	// Syntactic doc comments belong semantically to the field-chain
	// leaf only.
	if f, ok := n.(*Field); !ok || isFieldChainLeaf(f) {
		for _, cg := range c.Comments() {
			if isDocComment(cg) {
				docs = append(docs, cg)
			}
		}
	}
	return docs
}

// isDocComment reports whether cg documents the node it is attached to: a
// doc-position group sitting before the node's first token (Position 0). A
// field can also carry a trailing or dangling group with Doc set, e.g.:
//
//	x: {
//		// c1
//		y: _
//
//		// c2
//	}
//
// Here, c2 is a doc comment and it is attached to y, but its position
// != 0.
func isDocComment(cg *CommentGroup) bool {
	return cg.Doc && cg.Position == 0
}

// ResolveComments populates the inherited doc-comments for every
// field-chain under n, so that [DocComments] can report which node a
// doc comment documents without walking parent pointers. It modifies
// the AST in place and is idempotent: each run clears and recomputes
// ownership for the (sub)tree, so it is safe to re-run after a
// structural rewrite.
func ResolveComments(n Node) {
	// A single pre-order walk. A field-chain is linear - all but the
	// leaf have exactly one child - so the doc comments owed to the
	// leaf are exactly those accumulated on the way down. We carry
	// them in inherited, flush them to the leaf, then reset.
	var inherited []*CommentGroup
	Walk(n, func(n Node) bool {
		f, ok := n.(*Field)
		if !ok {
			return true
		}
		ci := f.commentInfo()
		ci.inheritedDocComments = nil

		if isFieldChainLeaf(f) {
			// Chain leaf owns the doc comments accumulated from its chain
			// ancestors. Reset afterwards so the leaf's own value - which
			// could begin a fresh chain - does not inherit them.
			if len(inherited) > 0 {
				inherited := slices.Clone(inherited)
				ci.inheritedDocComments = &inherited
			}
			inherited = inherited[:0]

		} else {
			for _, cg := range ci.Comments() {
				if isDocComment(cg) {
					inherited = append(inherited, cg)
				}
			}
		}

		return true
	}, nil)
}

// isFieldChainLeaf reports if f should be considered the leaf of a
// field-chain (i.e. f terminates the chain).
func isFieldChainLeaf(f *Field) bool {
	s, ok := f.Value.(*StructLit)
	if !ok || len(s.Elts) != 1 || s.Lbrace.IsValid() || s.Rbrace.IsValid() {
		return true
	}
	child, _ := s.Elts[0].(*Field)
	if child == nil {
		return true
	}
	if f.Pos().File() != child.Pos().File() {
		return true
	}
	// If it's a pattern or dynamic field, we terminate the chain. E.g.
	// `a: [string]: int`.
	//
	// TODO: Should we terminate the chain here? This mimics the
	// original exporter behaviour; but it is not obviously correct
	// that we should treat dynamic fields and patterns differently.
	if _, _, err := LabelName(child.Label); err != nil {
		return true
	}
	return false
}
