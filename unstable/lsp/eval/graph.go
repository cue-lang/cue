// Copyright 2026 CUE Authors
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

// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package eval

import (
	"iter"

	"cuelang.org/go/cue/ast"
)

// Node is a node in the graph that the LSP evaluator constructs. A
// Node presents the post-merged view of a single field. For example,
// given:
//
//	x: a
//	x: b
//
// both declarations of x are aggregated into a single Node.
//
// Obtain the root Node with [Evaluator.Root], descend into named
// fields with [Node.Children], and recover the individual
// declarations that contribute to a Node with [Node.Decls].
//
// Evaluation is lazy: a Node evaluates only as much of the package as
// is needed to answer a request, and memoizes the results.
type Node struct {
	// evaluated records whether [Node.eval] has run.
	evaluated bool
	// navs are the navigables that this Node aggregates. A Node has
	// more than one nav when it is reached through several
	// distinct, non-merged navigables. E.g.
	//
	//	x: z: 6
	//	y: x
	//	y: z: 7
	//
	// Here, `y.z`'s Node.navs will contain two navigables, one for the
	// 7, and one for the 6, which will be exactly the same nav as in
	// x.z's Node.navs. So, unlike a navigable, a Node's navs can
	// include navigables contributed by a parent field's resolved
	// embeddings, on top of the field's own declarations.
	navs []*navigable
	// children memoizes this Node's named child Nodes, keyed by field
	// name, as computed by [Node.childNodes]. It is nil until first
	// requested via [Node.Children], and must not be mutated
	// thereafter.
	children map[string]*Node
}

// Root returns the [Node] for the package's top-level scope,
// evaluating only enough to expose its direct children. The returned
// node aggregates the top-level bindings across the package.
func (e *Evaluator) Root() *Node {
	e.bootFiles()

	navs := make([]*navigable, 0, len(e.pkgFrame.childFrames))
	for _, fr := range e.pkgFrame.childFrames {
		navs = append(navs, fr.navigable)
	}
	navs = append(navs, e.pkgDecls)
	navs = deduplicateNavs(navs)

	return &Node{navs: navs}
}

func (n *Node) eval() {
	if n.evaluated {
		return
	}
	n.evaluated = true
	for _, nav := range n.navs {
		nav.eval()
	}
}

// Children returns this node's named child bindings, keyed by field
// name. Multiple bindings of the same name are merged into a single
// child [Node], so callers see the unified view of the field rather
// than its individual declarations. The returned map is memoised and
// must not be mutated by callers.
func (n *Node) Children() map[string]*Node {
	return n.childNodes()
}

// Decls returns an iterator over the distinct declarations that
// contribute to this node. A node may be defined by several
// declarations and each such declaration is yielded as a separate
// [Decl].
func (n *Node) Decls() iter.Seq[Decl] {
	n.eval()

	return func(yield func(Decl) bool) {
		seen := make(map[*frame]struct{})
		for _, nav := range n.navs {
			for _, fr := range nav.frames {
				if _, found := seen[fr]; found {
					continue
				}
				seen[fr] = struct{}{}
				if !yield((*frameDecl)(fr)) {
					return
				}
			}
		}
	}
}

// Decl represents a single declaration that contributes to a
// [Node]. A [Node] aggregates every declaration of a given name; each
// such declaration is exposed as a Decl, retaining its source key,
// the value expression, and any doc comments attached to it. Use
// [Node.Decls] to iterate the Decls for a node.
type Decl interface {
	// Key returns the [ast.Node] that names this declaration:
	// typically the [ast.Ident] or [ast.BasicLit] used as the field
	// label. Returns nil for declarations that have no source-level
	// key, such as the file declarations exposed at the root.
	Key() ast.Node
	// Value returns the [ast.Node] that holds this declaration's
	// value: the field's right-hand-side expression for an ordinary
	// field, or the [ast.File] itself for a file-level
	// declaration. Returns nil for declarations that have no
	// associated value node (e.g. a `package` clause, whose
	// information is exposed via [Decl.Key] and [Decl.DocComments]
	// only).
	Value() ast.Node
	// DocComments returns the doc-comment groups attached to this
	// declaration, or nil if none are present.
	DocComments() []*ast.CommentGroup
}

type frameDecl frame

var _ Decl = (*frameDecl)(nil)

// Key implements [Decl]
func (fr *frameDecl) Key() ast.Node {
	return fr.key
}

// Value implements [Decl]
func (fr *frameDecl) Value() ast.Node {
	return fr.node
}

// DocComments implements [Decl]
func (fr *frameDecl) DocComments() []*ast.CommentGroup {
	return ((*frame)(fr)).docComments()
}

func (n *Node) childNodes() map[string]*Node {
	if n.children != nil {
		return n.children
	}

	n.eval()

	childNavs := make(map[string][]*navigable)
	for nav := range expandNavigables(n.navs) {
		for name, child := range nav.bindings {
			childNavs[name] = append(childNavs[name], child)
		}
	}

	childNodes := make(map[string]*Node, len(childNavs))
	for name, navs := range childNavs {
		navs = deduplicateNavs(navs)
		childNodes[name] = &Node{navs: navs}
	}
	n.children = childNodes
	return childNodes
}

func deduplicateNavs(navs []*navigable) []*navigable {
	result := make([]*navigable, 0, len(navs))
	seen := make(map[*navigable]struct{})
	for _, nav := range navs {
		if _, found := seen[nav]; found {
			continue
		}
		seen[nav] = struct{}{}
		result = append(result, nav)
	}
	return result
}
