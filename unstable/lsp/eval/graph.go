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

package eval

import (
	"iter"

	"cuelang.org/go/cue/ast"
)

type Node struct {
	evaluated   bool
	navs        []*navigable
	docComments map[ast.Node][]*ast.CommentGroup
	children    map[string]*Node
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

func (fr *frameDecl) Key() ast.Node {
	return fr.key
}

func (fr *frameDecl) Value() ast.Node {
	return fr.node
}

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
