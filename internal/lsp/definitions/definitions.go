// Copyright 2025 CUE Authors
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

// Definitions resolves paths to sets of [ast.Node]. It is used in the
// LSP for "jump-to-definition" functionality, amongst others. A path
// is either an [ast.Ident], or a CUE expression followed by zero or
// more idents, all chained together by dots.
//
// # Introduction
//
// In the text that follows, subscripts are used in order to make
// identifiers (idents) unique for the purpose of explanation, but
// they should not be considered part of the ident itself, from the
// point of view of CUE.
//
// For example, in the code:
//
//	x₁: 17
//	y: x₂
//
// If the user places their cursor on x₂ and invokes
// "jump-to-definition", the cursor should move to x₁. In CUE, there
// can be several nodes that define a binding. For example:
//
//	x₁: 17
//	y: x₂
//	x₃: int
//
// Now, if the user places their cursor on x₂ and invokes
// "jump-to-definition", they should see both x₁ and x₃ as targets to
// which they can jump.
//
// The implementation is a lazy, memoized, call-by-need evaluator. The
// only purpose of this evaluator is to calculate what each element of
// each path resolves to; there is no calculation of fixed-points, no
// subsumption, no unification. And the little that this evaluator
// does do is imprecise. For example, it does not test field names
// (even when known) against patterns. It does not compute the names
// of dynamic fields, even when it is trivial to do so statically. It
// is a MAY-analysis and not a MUST-analysis. This means that it may
// offer jump-to-definition targets that do not occur during full
// evaluation, but which we are unable to dismiss with only the simple
// evaluation offered here.  A good example of this is with
// disjunctions:
//
//	x₁: {a₁: 3} | {a₂: 4}
//	y₁: x₂
//	y₂: a₃: <n₁
//	n₂: 5
//	z₁: y₃.a₄
//
// Here, a₄ will resolve to both a₁ and a₂, even though the constraint
// via a₃ may (or may not) eliminate one (or both!) branches of the
// disjunction.
//
// # Algorithm 1: simplified CUE
//
// In CUE, a path such as x.y.z, where x is an ident, is only legal if
// x is defined in the same lexical scope as the path x.y.z, or any
// ancestor lexical scope. There is one exception to this which is the
// package scope, which arguably doesn't exist lexically. We return to
// the package scope much later on.
//
// This restriction on paths complicates the algorithm. For example:
//
//	x₁: y₁: x₂.a₁
//	x₃: {
//		x₄: a₂: 17
//		z₁: x₅.a₃
//	}
//	x₆: a₄: 18
//
// Here, x₂ refers to x₁, x₃, and x₆, whilst x₅ refers only to
// x₄. Similarly, a₁ refers to a₄, but a₃ refers to a₂.
//
// To explain this evaluator, we start with a simplified version of
// CUE which does not place this restriction on paths: i.e. the first
// (and possibly only) element of a path may resolve to a definition
// that does *not* exist in the same lexical scope (or ancestor of) as
// that path.
//
// In this evaluator, an "astNode" is a collection of bindings,
// i.e. key-value pairs. The values are themselves astNodes.  An
// astNode is created with one or more unprocessed [ast.Node] values,
// for example, an [ast.File], or an [ast.StructLit].
//
// When an astNode is evaluated, its unprocessed values are
// unpacked. An [ast.StructLit] for example contains a number of
// [ast.Decl] nodes, which are themselves then processed. When a
// astNode encounters an [ast.Field], the astNode ensures a binding
// exists for the field's name, and adds the field's value to the
// binding's astNode's unprocessed values. Thus if the same field
// definition is encountered multiple times, its values are
// accumulated into a single astNode. Note that evaluation of an
// astNode is not recursive: its bindings are not automatically
// evaluated. Thus an astNode is the unit of evaluation; by adding new
// astNodes you can create new points where evaluation can pause (and
// potentially resume later on).
//
// If, during evaluation, an astNode encounters a path, the path will
// correspond either to the value of a field (i.e. the astNode is for
// something like x: y), or an embedding into a struct. The astNode
// keeps track of these embedded paths and once processing of the
// astNode's values is complete, it then resolves the embedded paths
// to further astNodes, and records that this astNode itself resolves
// to these other astNodes (the resolvesTo field).
//
// The consequence is that the evaluation of an astNode creates and
// fully populates (with their unprocessed values) all of its bindings
// before any resolution of paths occurs. Thus evaluation can be
// driven by demand: if a path is encountered that accesses one of the
// astNode's bindings (or any binding of an ancestor astNode), then it
// is guaranteed that the binding (if it exists) contains its complete
// set of values before it is accessed, and so it is safe to evaluate.
//
// Consider this example:
//
//	x: y
//	y: {
//		a: 3
//		b: y.a
//	}
//
// Evaluating the outermost astNode will create two bindings, one for
// x (with the path y as its value), and one for y (containing the
// [ast.StructLit] as its value). If the astNode for y is evaluated,
// it will create its own bindings for a (containing the
// [ast.BasicLit] 3), and for b (containing the path y.a).
//
// Imagine we want to resolve, in the outermost astNode, the path
// x.a. We first evaluate the outermost astNode, then inspect its
// bindings. We find an x in there, so we grab that astNode. This
// completes resolving the x of x.a. We now wish to find an a within
// that astNode, so we evaluate it. This astNode contains only the
// path y and so we have to resolve y and record that result within
// our astNode.
//
// Every astNode knows its own parent astNode. This astNode containing
// the path y will inspect its own bindings for y, and find
// nothing. It asks its ancestors whether they know of a binding for
// y. Its parent does have a binding for y, so we grab that
// astNode. This completes the resolution of y, and thus the
// evaluation of the astNode that contains the path y. We now ask this
// same astNode whether it contains a binding for a. It doesn't, but
// we also inspect all the astNodes that this node resolves to. There
// is one resolved-to astNode, and it does contain a binding for a, so
// we grab that. This completes the resolution of x.a.
//
// In summary: this algorithm traverses the AST breadth first and
// incrementally, to lazily merge together bindings that share the
// same path into astNodes.
//
// Unmentioned is that there are various [ast.Expr] types that can use
// paths but not declare their own bindings, for example an
// interpolated string. When these are encountered during evaluation,
// the astNode accumulates and processes them in the same way as
// embedded paths. The only difference is they don't need to be
// recorded within the node's resolves-to set.
//
// # Querying
//
// In the previous section, we walked through the example of
// attempting to resolve the path x.a in the outermost astNode. But
// this isn't what an LSP client will ask. An LSP client doesn't know
// what path the cursor is on, nor anything about the current scope or
// how these may correspond to astNodes. The LSP client knows only the
// cursor's line and column number.
//
// To facilitate an API that allows querying by file-coordinates,
// astNodes are extended with a rangeset. For each [ast.Node] that an
// astNode processes, it adds to its rangeset the range from the
// node's start file-offset to its end file-offset. Then, when asked
// to resolve whatever exists at some file-coordinate, we only need to
// evaluate the astNodes that contain the file-coordinate in question.
//
// # Algorithm 2: real CUE
//
// If we stuck to algorithm 1, it would mean that in:
//
//	a₁: b: c: a₂
//	a₃: b: a₄: 5
//
// a₂ would resolve to a₄. It also means that you get scary collisions
// with aliases, for example:
//
//	a: l₁=b: c: l₂.x
//	a: x: l₃.c
//
// Here, l₃ resolves to l₁, or b. So the rule that if the the first
// element of any path is an ident, then it can only be resolved
// lexically, must be implemented. This means that this evaluator must
// model "lexical bindings" which are candidates for resolving the
// first element of a path, separately from "navigable bindings" which
// are candidates for resolving the rest of the path (as you navigate
// the path...). The lexical bindings do not have the "merging"
// behaviour of algorithm 1, for example:
//
//	x₁: y₁: 6
//	x₂: y₂: 7
//
// Whereas before (in Algorithm 1) the evaluator would create one
// binding for x, now the evaluator creates two bindings for x, each
// having a distinct astNode value. Both of those astNodes share a
// "navigable bindings" struct and so any children that either of
// these astNodes have, can be grouped together appropriately via
// their shared "navigable bindings". Thus in this example, the
// evaluation of the outermost astNode creates two bindings for x;
// their distinct astNodes share a "navigable bindings", and also have
// one binding each for their respective y fields. These y fields are
// grouped together within the shared "navigable bindings".
//
// This means that when resolving the first element of a path, we can
// walk up the lexical bindings only, and then once that's resolved,
// switch to the navigable bindings for the rest of the path.
//
// For aliases, comprehensions and one or two other things, a binding
// can be created in the current astNode which is not added to the
// astNode's navigable bindings. This means it can only ever be found
// and used as the first element of a path. A navigable binding is
// always also a lexical binding, but a lexical binding need not be a
// navigable binding.
//
// # File and Package scopes
//
// CUE states that fields declared at the top level of a file are not
// in the file's scope, but are in fact in the package's scope. At
// construction, the file astNodes all share a "navigable
// bindings". Thus if two different files in the same package both
// declare the same field, they will be correctly grouped together
// within that navigable bindings.
//
// When a file astNode processes an [ast.File], lexical and navigable
// bindings will be created as normal. When resolving the first
// element of a path in some deeper astNode, it can be the case that
// after walking up the chain of ancestor astNodes, no matching
// lexical binding is found even within the relevant file's astNode's
// bindings. In this case, it is safe to directly inspect the file's
// navigable bindings, which amount to the package's lexical
// bindings. In this way, a path's first element can be an ident that
// is only declared in some separate file within the same package, and
// yet it can still be resolved.
package definitions

import (
	"fmt"
	"strconv"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/rangeset"
)

type DefinitionsForPackageFunc func(importPath string) *Definitions

// Definitions provides methods to resolve file offsets to their
// definitions.
type Definitions struct {
	// pkgNode is the top level (or root) lexical scope
	pkgNode *astNode
	// byFilename maps file names to [FileDefinitions]
	byFilename map[string]*FileDefinitions
	forPackage DefinitionsForPackageFunc
}

// Analyse creates and performs initial configuration of a new
// [Definitions] value. It does not perform any analysis eagerly. All
// files provided will be treated as if they are part of the same
// package. The set of files cannot be modified after construction;
// instead, construction is cheap, so the intention is you replace the
// whole Definitions value.
func Analyse(forPackage DefinitionsForPackageFunc, files ...*ast.File) *Definitions {
	if forPackage == nil {
		forPackage = func(importPath string) *Definitions { return nil }
	}
	dfns := &Definitions{
		byFilename: make(map[string]*FileDefinitions, len(files)),
		forPackage: forPackage,
	}

	pkgNode := dfns.newAstNode(nil, nil, nil, nil)
	dfns.pkgNode = pkgNode
	navigable := &navigableBindings{}

	for _, file := range files {
		pkgNode.newAstNode(nil, file, navigable)
		dfns.byFilename[file.Filename] = &FileDefinitions{
			pkgNode:     pkgNode,
			resolutions: make(map[int][]ast.Node),
			File:        file,
		}
	}

	return dfns
}

// newAstNode creates a new [astNode]. All arguments may be nil; if
// navigable is nil, then a new navigable will be created and used
// within the new astNode. The key is the node which is the definition
// this node represents, and may be nil if this astNode does represent
// any sort of binding. The unprocessed value is the [ast.Node] which
// is to be processed by the new astNode. In the case of an
// [ast.Field], for example, the key would be the field's label, and
// unprocessed would be the field's value. The navigableBindings are
// the (potentially shared) bindings which are used in the resolution
// of the non-first-elements of a path.
func (dfns *Definitions) newAstNode(parent *astNode, key ast.Node, unprocessed ast.Node, navigable *navigableBindings) *astNode {
	if navigable == nil {
		navigable = &navigableBindings{}
	}
	s := &astNode{
		dfns:        dfns,
		parent:      parent,
		unprocessed: unprocessed,
		navigable:   navigable,
	}
	navigable.contributingNodes = append(navigable.contributingNodes, s)
	if key != nil {
		s.key = key
		s.addRange(key)
	}
	return s
}

// addResolution records that the target navigableBindings are the
// definitions for the file and offset of the start token, and its
// given length.
func (dfns *Definitions) addResolution(start token.Pos, length int, targets []*navigableBindings) {
	if len(targets) == 0 {
		return
	}

	startPosition := start.Position()
	filename := startPosition.Filename
	resolutions := dfns.byFilename[filename].resolutions
	startOffset := startPosition.Offset
	var keys []ast.Node
	for _, nav := range targets {
		for _, lex := range nav.contributingNodes {
			if lex.key != nil {
				keys = append(keys, lex.key)
			}
		}
	}
	for i := range length {
		resolutions[startOffset+i] = keys
	}
}

// ForFile looks up the [FileDefinitions] for the given filename.
func (dfns *Definitions) ForFile(filename string) *FileDefinitions {
	return dfns.byFilename[filename]
}

// FileDefinitions provides methods to resolve file offsets within a
// certain file to their definitions.
type FileDefinitions struct {
	pkgNode *astNode
	// resolutions caches the results of previous lookups, ensuring
	// that subsequent calls to [ForOffset] for a given offset are
	// O(1). The map key is the byte offset within the file.
	resolutions map[int][]ast.Node
	// File is the original [ast.File] that was passed to [Analyse].
	File *ast.File
}

// ForOffset reports the definitions that the file offset (number of
// bytes from the start of the file) resolves to.
func (fdfns *FileDefinitions) ForOffset(offset int) []ast.Node {
	if offset < 0 {
		return nil
	}
	resolutions := fdfns.resolutions
	nodes, found := resolutions[offset]
	if found {
		return nodes
	}
	resolutions[offset] = []ast.Node{}

	filename := fdfns.File.Filename
	pkgNode := fdfns.pkgNode
	pkgNode.eval()
	seen := make(map[*astNode]struct{})
	worklist := []*astNode{pkgNode}
	for len(worklist) > 0 {
		s := worklist[0]
		worklist = worklist[1:]

		if _, found := seen[s]; found {
			continue
		}
		seen[s] = struct{}{}

		for _, s := range s.allChildren {
			s.eval()
			if s.contains(filename, offset) {
				worklist = append(worklist, s)
			}
		}
	}

	//pkgNode.dump(1)

	return resolutions[offset]
}

// astNode corresponds to a node from the AST. An astNode can be
// created at any time, and creates the opportunity for evaluation to
// be paused (and later resumed). Any binding reachable via
// node.parent*.bindings is a candidate for resolving the first
// (ident) element of a path, and the navigable field's value (which
// can be shared between astNodes) offers candidates for resolving
// subsequent elements of a path. So creating a new astNode creates a
// new namespace for lexical resolution, and may or may not create a
// new namespace for non-lexical resolution.
type astNode struct {
	dfns *Definitions
	// parent is the parent astNode.
	parent *astNode
	// unprocessed is the initial node that this astNode is solely
	// responsible for evaluating. Once a call to [node.eval] has
	// returned, unprocessed must never be modified.
	unprocessed ast.Node
	// key is the position that is considered to define this node. For
	// example, if a node represents `a: {}` then key is set to the `a`
	// ident. This can be nil, such as when a node is an
	// expression. For example in the path {a: 3, b: a}.b, a node with
	// no key will be created, containing the structlit {a: 3, b: a}.
	key ast.Node
	// An astNode may have several names. For example, if a node is the
	// result of a field with an alias, e.g. l=x: e, then in its parent
	// node it'll be stored (in the bindings field) under both l and
	// x. This name field contains only the navigable name, in this
	// case x. Sometimes, a node will have no navigable name, e.g. a
	// let declaration, which exists only as a lexical binding and not
	// a navigable binding.
	name string
	// resolvesTo points to the navigable bindings this node resolves
	// to, due to embedded paths. For example, in x: {y.z}, whatever
	// node y.z resolves to, its navigable bindings will be stored in
	// the resolvesTo field of x.
	resolvesTo []*navigableBindings
	// allChildren contains every astNode that is a child of this
	// node. When searching for a given file-offset, these nodes are
	// tested for whether they contain the desired file-offset.
	allChildren []*astNode
	// bindings contains all bindings for this astNode. Note the map's
	// values are slices because a single node can have multiple
	// bindings for the same key. For example:
	//
	//	x: bool
	//	x: true
	//
	// Bindings are used for the resolution of the first element of a
	// path, if that element is an ident. Thus to some extent they (and
	// an astNode itself) correspond to a lexical scope. Bindings are
	// more general than fields: they include aliases and
	// comprehensions as well as normal fields.
	bindings map[string][]*astNode
	// navigable provides access to the "navigable bindings" that is
	// shared between multiple astNodes that should be considered
	// "merged together".
	navigable *navigableBindings
	ranges    *rangeset.FilenameRangeSet
}

// newAstNode creates a new [astNodes] which is a child of the current
// astNode. This is a light wrapper around
// [Definitions.newAstNode]. See those docs for more details on the
// arguments to this function.
func (n *astNode) newAstNode(key ast.Node, unprocessed ast.Node, navigable *navigableBindings) *astNode {
	s := n.dfns.newAstNode(n, key, unprocessed, navigable)
	n.allChildren = append(n.allChildren, s)
	return s
}

// dump sends to stdout the current astNode, its bindings, and
// allChildren, in a "pretty" indented fashion. This is for aiding
// debugging.
func (n *astNode) dump(depth int) {
	printf := func(f string, a ...any) {
		fmt.Printf("%*s%s\n", depth*3, "", fmt.Sprintf(f, a...))
	}

	printf("Node %p (name: %q)", n, n.name)
	printf(" Ranges %v", n.ranges)

	navigable := n.navigable
	if len(navigable.bindings) > 0 {
		printf(" Navigable: %p", n.navigable)
		for name, bindings := range navigable.bindings {
			printf("  %s: %p", name, bindings)
		}
	}

	if len(n.bindings) > 0 {
		printf(" Lexical:")
		for name, bindings := range n.bindings {
			printf("  %s:", name)
			for _, binding := range bindings {
				binding.dump(depth + 1)
			}
		}
	}

	if len(n.allChildren) > 0 {
		printf(" All children:")
		for _, s := range n.allChildren {
			s.dump(depth + 1)
		}
	}
}

// A navigableBindings groups together nodes. The zero value is ready
// for use.
type navigableBindings struct {
	bindings          map[string]*navigableBindings
	ellipses          []*navigableBindings
	contributingNodes []*astNode
}

// addRange records that the astNode covers the range from the node's
// start file-offset to its end file-offset. Because the AST is
// non-recursive in a few areas (e.g. comprehensions), it's sometimes
// necessary to explicitly extend the range of an astNode so that
// navigation-by-offset evaluates the correct astNodes.
func (n *astNode) addRange(node ast.Node) {
	start := node.Pos().Position()
	end := node.End().Position()

	rs := n.ranges
	if rs == nil {
		rs = rangeset.NewFilenameRangeSet()
		n.ranges = rs
	}

	rs.Add(start.Filename, start.Offset, end.Offset)
}

// contains reports whether the astNode contains the given
// file-offset.
//
// As a special case, file nodes (i.e. astNodes for which the parent
// is the pkgNode) always contain every file-offset.
func (n *astNode) contains(filename string, offset int) bool {
	ranges := n.ranges
	return n.parent == n.dfns.pkgNode || (ranges != nil && ranges.Contains(filename, offset))
}

// eval evaluates the astNode lazily. Evaluation is not recursive: it
// does not evaluate child bindings. eval must be called before a
// node's bindings, allChildren, or resolvesTo fields are inspected,
// or before [astNode.contains] is invoked. See also the package level
// documentation.
func (n *astNode) eval() {
	if n.unprocessed == nil {
		return
	}

	unprocessed := []ast.Node{n.unprocessed}
	n.unprocessed = nil

	var embeddedResolvable, resolvable []ast.Expr

	for len(unprocessed) > 0 {
		node := unprocessed[0]
		unprocessed = unprocessed[1:]

		n.addRange(node)

		switch node := node.(type) {
		case *ast.File:
			for _, decl := range node.Decls {
				unprocessed = append(unprocessed, decl)
			}

		case *ast.ImportDecl:
			for _, spec := range node.Specs {
				unprocessed = append(unprocessed, spec)
			}

		case *ast.ImportSpec:
			if node.Name == nil {
				str, err := strconv.Unquote(node.Path.Value)
				if err != nil {
					continue
				}
				ip := ast.ParseImportPath(str)
				if ip.Qualifier != "" {
					n.newBinding(ip.Qualifier, node, nil)
				}
			} else {
				n.newBinding(node.Name.Name, node, nil)
			}

		case *ast.StructLit:
			for _, elt := range node.Elts {
				unprocessed = append(unprocessed, elt)
			}

		case *ast.ListLit:
			for i, elt := range node.Elts {
				if _, ok := elt.(*ast.Ellipsis); ok {
					unprocessed = append(unprocessed, elt)
					continue
				}
				// Fake list elements as numbered fields. These will
				// immediately be converted into bindings via the
				// *ast.Field case below.
				unprocessed = append(unprocessed, &ast.Field{
					Label:    &ast.Ident{NamePos: elt.Pos(), Name: fmt.Sprint(i)},
					TokenPos: elt.Pos(),
					Token:    token.COLON,
					Value:    elt,
				})
			}

		case *ast.Interpolation:
			resolvable = append(resolvable, node.Elts...)

		case *ast.EmbedDecl:
			unprocessed = append(unprocessed, node.Expr)

		case *ast.ParenExpr:
			unprocessed = append(unprocessed, node.X)

		case *ast.UnaryExpr:
			resolvable = append(resolvable, node.X)

		case *ast.BinaryExpr:
			switch node.Op {
			case token.AND:
				n.newAstNode(nil, node.X, n.navigable)
				n.newAstNode(nil, node.Y, n.navigable)
			case token.OR:
				lhs := n.newAstNode(nil, node.X, nil)
				rhs := n.newAstNode(nil, node.Y, nil)
				n.resolvesTo = append(n.resolvesTo, lhs.navigable, rhs.navigable)
			default:
				resolvable = append(resolvable, node.X, node.Y)
			}

		case *ast.Alias:
			// X=e (the old deprecated alias syntax)
			n.newBinding(node.Ident.Name, node.Ident, node.Expr)

		case *ast.Ellipsis:
			child := n.newAstNode(node, node.Type, nil)
			n.navigable.ellipses = append(n.navigable.ellipses, child.navigable)

		case *ast.CallExpr:
			resolvable = append(resolvable, node.Fun)
			resolvable = append(resolvable, node.Args...)

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, node.(ast.Expr))

		case *ast.Comprehension:
			parent := n
			for _, clause := range node.Clauses {
				cur := parent.newAstNode(nil, clause, nil)
				// We need to make sure that the comprehension value
				// (i.e. body) and all subsequent clauses, can be reached
				// by traversing through all clauses. The simplest way to
				// do this is just to include the whole range of n within
				// each descendent.
				cur.addRange(node)
				cur.eval()
				parent = cur
			}
			if parent != n {
				child := parent.newAstNode(nil, node.Value, nil)
				n.resolvesTo = append(n.resolvesTo, child.navigable)
			}

		case *ast.IfClause:
			unprocessed = append(unprocessed, node.Condition)

		case *ast.LetClause:
			n.newBinding(node.Ident.Name, node.Ident, node.Expr)

		case *ast.ForClause:
			if node.Key != nil {
				n.newBinding(node.Key.Name, node.Key, nil)
			}
			if node.Value != nil {
				n.newBinding(node.Value.Name, node.Value, nil)
			}
			resolvable = append(resolvable, node.Source)

		case *ast.Field:
			label := node.Label

			alias, isAlias := label.(*ast.Alias)
			if isAlias {
				if expr, ok := alias.Expr.(ast.Label); ok {
					label = expr
				}
			}

			var binding *astNode
			switch label := label.(type) {
			case *ast.Ident:
				binding = n.ensureNavigableBinding(label.Name, label, node.Value)
			case *ast.BasicLit:
				name, _, err := ast.LabelName(label)
				if err == nil {
					binding = n.ensureNavigableBinding(name, label, node.Value)
				} else {
					binding = n.newAstNode(label, node.Value, nil)
				}
			default:
				binding = n.newAstNode(label, node.Value, nil)
			}

			if isAlias {
				switch alias.Expr.(type) {
				case *ast.ListLit:
					// X=[e]: field
					// X is only visible within field
					wrapper := n.newAstNode(nil, nil, nil)
					wrapper.appendBinding(alias.Ident.Name, binding)
					binding.parent = wrapper
				case ast.Label:
					// X=ident: field
					// X="basic": field
					// X="\(e)": field
					// X=(e): field
					// X is visible within s
					n.appendBinding(alias.Ident.Name, binding)
				}
			}

			switch label := label.(type) {
			case *ast.Interpolation:
				resolvable = append(resolvable, label.Elts...)
			case *ast.ParenExpr:
				if alias, ok := label.X.(*ast.Alias); ok {
					// (X=e): field
					// X is only visible within field.
					// Although the spec supports this, the parser doesn't seem to.
					wrapper := n.newAstNode(nil, nil, nil)
					wrapper.newBinding(alias.Ident.Name, alias.Ident, alias.Expr)
					binding.parent = wrapper
				} else {
					resolvable = append(resolvable, label.X)
				}
			case *ast.ListLit:
				for _, elt := range label.Elts {
					if alias, ok := elt.(*ast.Alias); ok {
						// [X=e]: field
						// X is only visible within field.
						wrapper := n.newAstNode(nil, nil, nil)
						wrapper.newBinding(alias.Ident.Name, alias.Ident, alias.Expr)
						binding.parent = wrapper
					} else {
						resolvable = append(resolvable, elt)
					}
				}
			}
		}
	}

	for _, expr := range embeddedResolvable {
		nodes := n.resolve(expr)
		n.resolvesTo = append(n.resolvesTo, nodes...)
	}
	for _, expr := range resolvable {
		n.resolve(expr)
	}
}

// resolve resolves the given expression into a slice of navigable
// bindings. It is a slice because a single expression may resolve to
// several unrelated navigable bindings. For example the expression `x
// & y`.
func (n *astNode) resolve(e ast.Expr) []*navigableBindings {
	switch e := e.(type) {
	case *ast.Ident:
		root := n.resolvePathRoot(e.Name)
		if root == nil {
			return nil
		}
		nav := []*navigableBindings{root}
		n.dfns.addResolution(e.NamePos, len(e.Name), nav)
		return nav

	case *ast.SelectorExpr:
		resolved := n.resolve(e.X)
		name, isIdent, err := ast.LabelName(e.Sel)
		if err != nil {
			return nil
		}

		results := navigateBindingsByName(resolved, name)
		nameLen := len(name)
		if !isIdent {
			// If it's not an ident, then it is quoted.
			nameLen += 2
		}
		n.dfns.addResolution(e.Sel.Pos(), nameLen, results)
		return results

	case *ast.IndexExpr:
		resolved := n.resolve(e.X)
		lit, ok := e.Index.(*ast.BasicLit)
		if !ok {
			// If it's a path/ident etc, we don't attempt to calculate
			// the dynamic index.
			n.resolve(e.Index)
			return nil
		}
		name := lit.Value
		if lit.Kind != token.INT {
			var err error
			name, _, err = ast.LabelName(lit)
			if err != nil {
				return nil
			}
		}

		results := navigateBindingsByName(resolved, name)
		spanLen := e.Rbrack.Offset() - e.Lbrack.Offset() + 1
		n.dfns.addResolution(e.Lbrack, spanLen, results)
		return results

	case *ast.StructLit, *ast.ListLit:
		return []*navigableBindings{n.newAstNode(nil, e, nil).navigable}

	case *ast.ParenExpr:
		return n.resolve(e.X)

	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND, token.OR:
			return append(n.resolve(e.X), n.resolve(e.Y)...)
		}
	}

	return nil
}

// navigateBindingsByName maximally expands the set of bindings by
// transitively traversing resolvesTo fields of their contributing
// nodes. Every navigable binding within this expanded set is then
// indexed by the name, and the accumulated results returned.
func navigateBindingsByName(navigables []*navigableBindings, name string) []*navigableBindings {
	if len(navigables) == 0 {
		return nil
	}
	navigableSet := make(map[*navigableBindings]struct{})
	for len(navigables) > 0 {
		nav := navigables[0]
		navigables = navigables[1:]
		if _, seen := navigableSet[nav]; seen {
			continue
		}
		navigableSet[nav] = struct{}{}

		for _, node := range nav.contributingNodes {
			node.eval()
			navigables = append(navigables, node.resolvesTo...)

			if spec, ok := node.key.(*ast.ImportSpec); ok {
				str, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				dfns := node.dfns.forPackage(str)
				if dfns == nil {
					continue
				}
				pkgNode := dfns.pkgNode
				pkgNode.eval()
				for _, node := range pkgNode.allChildren {
					navigables = append(navigables, node.navigable)
				}
			}
		}
	}

	var results []*navigableBindings
	for navigable := range navigableSet {
		binding, found := navigable.bindings[name]
		if found {
			results = append(results, binding)
		} else {
			results = append(results, navigable.ellipses...)
		}
	}
	return results
}

// resolvePathRoot resolves only the [ast.Ident] first element of a
// path. CUE restricts the first element of any path (if it's an
// ident) to be lexically defined. So here, we search for a match via
// the astNode's own bindings (and its ancestry), whereas for
// subsequent path elements, we search the navigable bindings (see the
// [astNode.resolve] method).
func (n *astNode) resolvePathRoot(name string) *navigableBindings {
	pkgNode := n.dfns.pkgNode
	for ; n != nil; n = n.parent {
		if bindings, found := n.bindings[name]; found {
			if len(bindings) == 1 {
				binding := bindings[0]
				if binding.name == "" {
					// name has been resolved to an alias (or comprehension
					// binding, dynamic field, pattern etc). Crucially, it
					// doesn't have a "navigable" name.
					return binding.navigable
				} else if binding.name != name {
					// name has been resolved to an alias which had a
					// normal ident or basiclit field name. Switch to that
					// name.
					return n.navigable.bindings[binding.name]
				}
			}

			// If name lexically matches a non-alias, it must be matching
			// an ident and not a basiclit. But that ident can come from
			// any of the (potentially many) matching bindings!
			identFound := false
			var nav *navigableBindings
			for _, binding := range bindings {
				if nav == nil {
					nav = binding.navigable
				} else if nav != binding.navigable {
					// Invariant: all bindings associated with the same
					// name within an astNode must share the same navigable
					// bindings. This should be guaranteed by construction,
					// but we test it here just in case.
					panic(fmt.Sprintf("Different navigable bindings for name: %q", name))
				}
				if _, ok := binding.key.(*ast.Ident); ok {
					identFound = true
				}
			}
			if !identFound {
				continue
			}
			return nav
		}
		if n.parent == pkgNode {
			// pkgNode is the parent of the fileNodes. If we've got this
			// far, we're allowed to inspect the (shared) navigable
			// bindings directly without having to go via our bindings.
			return n.navigable.bindings[name]
		}
	}
	return nil
}

// ensureNavigableBinding creates and returns a new [astNode],
// locating and using the appropriate shared [navigableBindings] for
// the given name. The new node is stored in the node's bindings.
func (n *astNode) ensureNavigableBinding(name string, key ast.Node, unprocessed ast.Node) *astNode {
	// Search via our own shared navigable bindings. This is a
	// criticial step that ensures that we continue to correctly share
	// navigableBindings even as astNodes diverge. For example:
	//
	//	a: x.y.z
	//	x: y: z: 3
	//	x: y: z: 4
	//
	// By searching the *shared* bindings, we ensure not only that the
	// two x: astNodes share a navigableBinding, but so too do the two
	// y: nodes, and the two z: nodes. This ensures that the z in the
	// x.y.z path resolves to both the z: 3 and z: 4 definitions.

	// Lazily create our own navigable's bindings if needed:
	bindings := n.navigable.bindings
	if bindings == nil {
		bindings = make(map[string]*navigableBindings)
		n.navigable.bindings = bindings
	}

	// Search for the navigable for the new binding.
	navigable, found := bindings[name]
	binding := n.newAstNode(key, unprocessed, navigable)
	binding.name = name

	if !found {
		// If the new binding has a new navigable, store it in our
		// bindings, under name.
		bindings[name] = binding.navigable
	}
	n.appendBinding(name, binding)

	return binding
}

// newBinding creates and returns a new [astNode], and stores it under
// the given name in the current astNode only.
func (n *astNode) newBinding(name string, key ast.Node, unprocessed ast.Node) *astNode {
	binding := n.newAstNode(key, unprocessed, nil)
	n.appendBinding(name, binding)
	return binding
}

// appendBinding stores the binding under the given name in the
// current astNode only.
func (n *astNode) appendBinding(name string, binding *astNode) {
	if n.bindings == nil {
		n.bindings = make(map[string][]*astNode)
	}
	n.bindings[name] = append(n.bindings[name], binding)
}
