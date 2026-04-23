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

// DocComments returns the doc-comment groups attached to this node,
// keyed by the [ast.Node] (typically a field key) that the comments
// document. A single [Node] may correspond to multiple field
// declarations across files, so the result aggregates all of them.
// The returned map is memoised and must not be mutated by callers.
func (n *Node) DocComments() map[ast.Node][]*ast.CommentGroup {
	if n.docComments != nil {
		return n.docComments
	}

	n.eval()

	commentsMap := make(map[ast.Node][]*ast.CommentGroup)
	for _, nav := range n.navs {
		for _, fr := range nav.frames {
			if fr.key == nil {
				continue
			}
			if comments := fr.docComments(); len(comments) > 0 {
				commentsMap[fr.key] = comments
			}
		}
	}
	n.docComments = commentsMap
	return commentsMap
}

// Children returns this node's named child bindings, keyed by field
// name. Multiple bindings of the same name are merged into a single
// child [Node], so callers see the unified view of the field rather
// than its individual declarations. The returned map is memoised and
// must not be mutated by callers.
func (n *Node) Children() map[string]*Node {
	return n.childNodes()
}

// Values returns an iterator over the distinct [ast.Node] expressions
// that constitute the value of this node.
func (n *Node) Values() iter.Seq[ast.Node] {
	n.eval()

	return func(yield func(ast.Node) bool) {
		seen := make(map[ast.Node]struct{})
		for _, nav := range n.navs {
			for _, fr := range nav.frames {
				node := fr.node
				if node == nil {
					continue
				} else if _, found := seen[node]; found {
					continue
				}
				seen[node] = struct{}{}
				if !yield(node) {
					return
				}
			}
		}
	}
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
