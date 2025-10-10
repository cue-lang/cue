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
//
// # Field declaration keys
//
// We wish for jump-to-definition from a field declaration's key to
// resolve to other declarations of the same field. For example:
//
//	x₁: y₁: int
//	x₂: y₂: 7
//
// x₁ and x₂ should resolve to each other. Similarly y₁ and y₂. To
// achieve this, when a field is encountered and a new binding added,
// the new binding's astNode itself adds a new child astNode that
// contains a [fieldDeclExpr] as its unprocessed value. This value,
// when evaluated, walks up the navigable bindings ancestors,
// gathering their names, and stopping when either the package root is
// reached, or the navigable binding has no name. From this oldest
// ancestor, the calculated path is then resolved using the normal
// mechanics for path resolution. This path will resolve to all the
// declarations of the field in question. Imagine that in the above
// example, both int and 7 are replaced with the path x.y.
package definitions

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/rangeset"
)

// DefinitionsForPackageFunc is a callback function used to resolve
// package definitions by their import path. It returns the
// Definitions for the given import path, or nil if the package cannot
// be resolved.
type DefinitionsForPackageFunc func(importPath string) *Definitions

// ImportersFunc is a callback function to fetch the package
// definitions for packages that import the current package.
type ImportersFunc func() []*Definitions

// Definitions provides methods to resolve file offsets to their
// definitions.
type Definitions struct {
	// ip is the canonical (with major-version suffix) import path for
	// this package.
	ip ast.ImportPath
	// pkgNode is the top level (or root) lexical scope
	pkgNode *astNode
	// pkgDecls contains every package declaration with the files
	// passed to [Analyse]. This is not the same as the
	// [pkgNode.navigable] (which is the entire package scope).
	pkgDecls *navigableBindings
	// byFilename maps file names to [FileDefinitions]
	byFilename map[string]*FileDefinitions
	// forPackage is a callback function to resolve imported packages.
	forPackage DefinitionsForPackageFunc
	// pkgImporters is a callback function to fetch the definitions for
	// all packages that import this package.
	pkgImporters ImportersFunc
	// importCanonicalisation provides canonical (with major-version
	// suffix) import paths for every import spec within the current
	// package. The LSP always uses canonical import paths, but
	// individual cue files can have import statements that (1) elide
	// major version suffices; (2) have explicit but unnecessary
	// qualifiers. Being able to map such paths from import statements
	// to canonical ImportPaths is necessary when (1) grouping together
	// common imports (perhaps different files within this package
	// import the same package, but the path can differ); (2) searching
	// for import specs that have paths which refer to a particular
	// ImportPath.
	importCanonicalisation map[string]ast.ImportPath
	// importSpecNodes contains entries for every import within this
	// package. Within this package, all import specs that are of the
	// same (canonical) import path, share the same
	// navigableBinding. This is made possible by this map.
	importSpecNodes map[ast.ImportPath]*navigableBindings
}

// Analyse creates and performs initial configuration of a new
// [Definitions] value. It does not perform any analysis eagerly. All
// files provided will be treated as if they are part of the same
// package. The set of files cannot be modified after construction;
// instead, construction is cheap, so the intention is you replace the
// whole Definitions value.
//
// For the ip, importCanonicalisation, forPackage, and pkgImporters
// parameters, see the corresponding fields in [Definitions].
func Analyse(ip ast.ImportPath, importCanonicalisation map[string]ast.ImportPath, forPackage DefinitionsForPackageFunc, pkgImporters ImportersFunc, files ...*ast.File) *Definitions {
	if forPackage == nil {
		forPackage = func(importPath string) *Definitions { return nil }
	}
	if pkgImporters == nil {
		pkgImporters = func() []*Definitions { return nil }
	}
	dfns := &Definitions{
		ip:                     ip,
		importCanonicalisation: importCanonicalisation,
		byFilename:             make(map[string]*FileDefinitions, len(files)),
		forPackage:             forPackage,
		pkgImporters:           pkgImporters,
		importSpecNodes:        make(map[ast.ImportPath]*navigableBindings),
	}
	dfns.Reset()

	pkgNode := dfns.pkgNode
	fileNodesNavigable := &navigableBindings{
		dfns:   dfns,
		parent: pkgNode.navigable,
	}

	for _, file := range files {
		fdfns := &FileDefinitions{
			dfns:        dfns,
			definitions: make(map[int][]*navigableBindings),
			completions: make(map[int]*completionResolutions),
			File:        file,
		}
		dfns.byFilename[file.Filename] = fdfns
		fileNode := fdfns.newAstNode(pkgNode, nil, file, fileNodesNavigable)
		fileNode.fieldsAllowed = true
		pkgNode.allChildren = append(pkgNode.allChildren, fileNode)
	}

	return dfns
}

func (dfns *Definitions) Reset() {
	clear(dfns.importSpecNodes)

	// pkgNode, and its navigable, are the roots. They have no parents.
	pkgNode := &astNode{}
	pkgNode.navigable = &navigableBindings{
		dfns:              dfns,
		contributingNodes: []*astNode{pkgNode},
	}
	dfns.pkgNode = pkgNode

	// pkgDecls is a child of pkgNode.navigable. It will have every
	// package declaration contributed to it as they are encountered.
	dfns.pkgDecls = &navigableBindings{
		dfns:   dfns,
		parent: pkgNode.navigable,
	}

	// fileNodesNavigable is also a child of pkgNode.navigable. It has
	// every file node contributed to it.
	fileNodesNavigable := &navigableBindings{
		dfns:   dfns,
		parent: pkgNode.navigable,
	}

	for _, fdfns := range dfns.byFilename {
		clear(fdfns.definitions)
		clear(fdfns.completions)
		clear(fdfns.identUsageOffsets)
		fileNode := fdfns.newAstNode(pkgNode, nil, fdfns.File, fileNodesNavigable)
		fileNode.fieldsAllowed = true
		pkgNode.allChildren = append(pkgNode.allChildren, fileNode)
	}
}

func (dfns *Definitions) bootFiles() {
	// Eval the pkgNode and its immediate children only
	// (which will be filenodes). This is enough to
	// ensure the package declarations have been found
	// and added to the pkgDecls.
	pkgNode := dfns.pkgNode
	pkgNode.eval()
	for _, child := range pkgNode.allChildren {
		child.eval()
	}
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
func (fdfns *FileDefinitions) newAstNode(parent *astNode, key ast.Node, unprocessed ast.Node, navigable *navigableBindings) *astNode {
	if navigable == nil {
		navigable = &navigableBindings{dfns: fdfns.dfns}
		if parent != nil {
			navigable.parent = parent.navigable
		}
	}
	s := &astNode{
		fdfns:       fdfns,
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

// ForFile looks up the [FileDefinitions] for the given filename.
func (dfns *Definitions) ForFile(filename string) *FileDefinitions {
	return dfns.byFilename[filename]
}

// FileDefinitions provides methods to resolve file offsets within a
// certain file to their definitions.
type FileDefinitions struct {
	dfns *Definitions
	// definitions caches the definitions that have been computed
	// during evaluation. This ensures that subsequent calls to
	// [evalForOffset] for a given offset are O(1). The map key is the
	// byte offset within the file.
	definitions map[int][]*navigableBindings
	// completions is the same as definitions, but for completions.
	completions map[int]*completionResolutions
	// File is the original [ast.File] that was passed to [Analyse].
	File *ast.File

	identUsageOffsets map[string][]int
}

// completionResolutions models the various different types of
// completions that are available for a given location, alongside
// various file coordinates which are essential for the LSP to
// instruct the client correctly for editing the source.
type completionResolutions struct {
	embedNavigables []*navigableBindings
	embedNode       *astNode
	fieldNavigables []*navigableBindings
	startOffset     int
	fieldEndOffset  int
	embedEndOffset  int
}

// DefinitionsForOffset reports the definitions that the file offset (number of
// bytes from the start of the file) resolves to.
func (fdfns *FileDefinitions) DefinitionsForOffset(offset int) []ast.Node {
	fdfns.dfns.bootFiles()
	definitions := fdfns.definitions
	navs, found := definitions[offset]
	if !found {
		definitions[offset] = []*navigableBindings{}
		fdfns.evalForOffset(offset)
		navs = definitions[offset]
	}

	var nodes []ast.Node
	for _, nav := range navs {
		for _, n := range nav.contributingNodes {
			if n.key != nil {
				nodes = append(nodes, n.key)
			}
		}
	}

	return nodes
}

// DocCommentsForOffset is very similar to DefinitionsForOffset. It
// reports the doc comments associated with then definitions that the
// file offset (number of bytes from the start of the file) resolves
// to.
func (fdfns *FileDefinitions) DocCommentsForOffset(offset int) map[ast.Node][]*ast.CommentGroup {
	fdfns.dfns.bootFiles()
	definitions := fdfns.definitions
	navs, found := definitions[offset]
	if !found {
		definitions[offset] = []*navigableBindings{}
		fdfns.evalForOffset(offset)
		navs = definitions[offset]
	}

	commentsMap := make(map[ast.Node][]*ast.CommentGroup)
	for _, nav := range navs {
		for _, n := range nav.contributingNodes {
			if n.key != nil && len(n.docComments) > 0 {
				commentsMap[n.key] = n.docComments
			}
		}
	}

	return commentsMap
}

// CompletionsForOffset reports the set of strings that can form a new
// path element following the path element indicated by the offset
// (number of bytes from the start of the file).
func (fdfns *FileDefinitions) CompletionsForOffset(offset int) (fields, embeds []string, startOffset, fieldEndOffset, embedEndOffset int) {
	fdfns.dfns.bootFiles()
	completions := fdfns.completions
	resolutions, found := completions[offset]
	if !found {
		completions[offset] = &completionResolutions{}
		fdfns.evalForOffset(offset)
		resolutions = completions[offset]
	}

	namesSet := make(map[string]struct{})
	if navs := resolutions.fieldNavigables; len(navs) > 0 {
		navigableSet := expandNavigables(navs)
		for nav := range navigableSet {
			for name := range nav.bindings {
				if !strings.HasPrefix(name, "__") {
					namesSet[name] = struct{}{}
				}
			}
		}
	}
	fields = slices.Collect(maps.Keys(namesSet))
	slices.Sort(fields)

	clear(namesSet)
	if navs := resolutions.embedNavigables; len(navs) > 0 {
		navigableSet := expandNavigables(navs)

		for nav := range navigableSet {
			for name := range nav.bindings {
				if !strings.HasPrefix(name, "__") && ast.IsValidIdent(name) {
					namesSet[name] = struct{}{}
				}
			}
		}
	}

	for node := resolutions.embedNode; node != nil; node = node.parent {
		for name := range node.bindings {
			if !strings.HasPrefix(name, "__") && ast.IsValidIdent(name) {
				namesSet[name] = struct{}{}
			}
		}
		if node.isFileNode() {
			// Include the package bindings
			for name := range node.navigable.bindings {
				if !strings.HasPrefix(name, "__") && ast.IsValidIdent(name) {
					namesSet[name] = struct{}{}
				}
			}
		}
	}
	embeds = slices.Collect(maps.Keys(namesSet))
	// For now, we are simply sorting completions lexicographically. It
	// might be worth trying to sort by "distance" - the number of
	// parents/resolvedTo "hops" it takes to get from the cursor
	// position to the suggestion. That might be quite complex to do
	// though, and many editors completely ignore any suggested
	// ordering of completions, so it's not clear if this will be worth
	// it. TODO.
	slices.Sort(embeds)

	return fields, embeds, resolutions.startOffset, resolutions.fieldEndOffset, resolutions.embedEndOffset
}

func (fdfns *FileDefinitions) UsagesForOffset(offset int) []ast.Node {
	fdfns.dfns.bootFiles()
	definitions := fdfns.definitions
	navs, found := definitions[offset]
	if !found {
		definitions[offset] = []*navigableBindings{}
		fdfns.evalForOffset(offset)
		navs = definitions[offset]
	}

	if len(navs) == 1 {
		nav := navs[0]
		if nav == nav.dfns.pkgDecls && nav.dfns != fdfns.dfns {
			// The request is for usages of an import.
			for n, node := range nav.usedBy {
				if _, ok := n.(*ast.ImportSpec); ok && node.fdfns == fdfns {
					navs = []*navigableBindings{node.navigable}
					break
				}
			}
		}
	}

	usages(navs)

	exprs := make(map[ast.Node]*astNode)
	for _, nav := range navs {
		maps.Copy(exprs, nav.usedBy)
	}
	return slices.Collect(maps.Keys(exprs))
}

func usages(navsWorklist []*navigableBindings) {
	// every nav which uses the thing we care about, or any nav in the path to the thing we care about
	targetsSeen := make(map[*navigableBindings]struct{})
	evaluatedNavs := make(map[*navigableBindings]struct{})
	evaluatedNodes := make(map[*astNode]struct{})
	navsWorklist = slices.Clip(navsWorklist)

	for len(navsWorklist) > 0 {
		nav := navsWorklist[0]
		navsWorklist = navsWorklist[1:]

		if _, seen := evaluatedNavs[nav]; seen {
			continue
		}
		evaluatedNavs[nav] = struct{}{}

		isExported := false
		var pathElements []*navigableBindings
		var evalWorklist []*astNode
		{
			var child *navigableBindings
			for parent := nav; parent != nil; child, parent = parent, parent.parent {
				if parent == parent.dfns.pkgNode.navigable {
					isExported = true
				}
				pathElements = append(pathElements, parent)
				if child != nil && parent.name != "" && child.name == "" {
					break
				}
				evaluatedNavs[parent] = struct{}{}
				evalWorklist = parent.contributingNodes
			}
		}

		evalWorklist = slices.Clip(evalWorklist)
		for len(evalWorklist) > 0 {
			node := evalWorklist[0]
			evalWorklist = evalWorklist[1:]

			if _, seen := evaluatedNodes[node]; seen {
				continue
			}
			evaluatedNodes[node] = struct{}{}

			node.eval()
			for _, s := range node.allChildren {
				evalWorklist = append(evalWorklist, s)
			}
		}

		targetsWorklist := pathElements
		clear(targetsSeen)
		for len(targetsWorklist) > 0 {
			target := targetsWorklist[0]
			targetsWorklist = targetsWorklist[1:]

			if _, seen := targetsSeen[target]; seen {
				continue
			}
			targetsSeen[target] = struct{}{}

			for _, use := range target.usedBy {
				// Only if the usage is basically an embedding
				// (i.e. appears in resolvesTo) do we need to go
				// further. So for example, when inspecting the uses of y,
				// if we found x: y + 1 we would not then need to inspect
				// the uses of x.
				if slices.Contains(use.resolvesTo, target) {
					targetsWorklist = append(targetsWorklist, use.navigable)
					navsWorklist = append(navsWorklist, use.navigable)
				}
			}
		}

		if isExported {
			// For all the packages that import us, find where usages of
			// this package appears.
			for _, dfns := range nav.dfns.pkgImporters() {
				navsWorklist = append(navsWorklist, dfns.initialNavsForImport(nav.dfns.ip)...)
			}
		}
	}
}

func (dfns *Definitions) initialNavsForImport(ip ast.ImportPath) []*navigableBindings {
	dfns.bootFiles()
	nav, found := dfns.importSpecNodes[ip]
	if !found {
		return nil
	}
	var result []*navigableBindings
	for _, node := range nav.contributingNodes {
		spec := node.keyAlias.(*ast.ImportSpec)
		name := ""
		if alias := spec.Name; alias != nil && alias.Name != "" {
			name = alias.Name
		} else {
			ip := dfns.parseImportSpec(spec)
			if ip == nil {
				continue
			}
			name = ip.Qualifier
		}
		fdfns := node.fdfns
		definitions := fdfns.definitions
		offsets := fdfns.findIdentUsageOffsets(name)

		for _, offset := range offsets {
			navs, found := definitions[offset]
			if !found {
				definitions[offset] = []*navigableBindings{}
				fdfns.evalForOffset(offset)
				navs = definitions[offset]
			}
			result = append(result, navs...)
		}
	}
	return result
}

func (fdfns *FileDefinitions) findIdentUsageOffsets(name string) []int {
	identUsageOffsets := fdfns.identUsageOffsets
	if identUsageOffsets == nil {
		identUsageOffsets = make(map[string][]int)
		fdfns.identUsageOffsets = identUsageOffsets
	}

	offsets, found := identUsageOffsets[name]
	if !found {
		ast.Walk(fdfns.File, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportDecl:
				return false
			case *ast.Field:
				if label, ok := n.Label.(*ast.Ident); ok && label.Name == name {
					return false
				}
				// NB we could get smarter and try and look inside an alias.
			case *ast.LetClause:
				if n.Ident.Name == name {
					return false
				}
			case *ast.ForClause:
				if n.Key != nil && n.Key.Name == name {
					return false
				}
				if n.Value != nil && n.Value.Name == name {
					return false
				}
			case *ast.Ident:
				if n.Name == name {
					offsets = append(offsets, n.Pos().Position().Offset)
				}
			}
			return true
		}, nil)
		identUsageOffsets[name] = offsets
	}

	return offsets
}

func (dfns *Definitions) parseImportSpec(spec *ast.ImportSpec) *ast.ImportPath {
	str, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return nil
	}
	if ip, found := dfns.importCanonicalisation[str]; found {
		return &ip
	}
	// If the import specs explicitly give a qualifier that isn't
	// needed, we should test again in case we now get a match
	// (modimports calls Canonical on import paths so if the
	// importCanonicalisation is built from the result of modimports
	// etc then this second test becomes necessary).
	ip := ast.ParseImportPath(str).Canonical()
	if ip, found := dfns.importCanonicalisation[ip.String()]; found {
		return &ip
	}
	return &ip
}

// evalForOffset evaluates from the pkgNode, evaluating only child
// astNodes that contain the given file-byte-offset.
func (fdfns *FileDefinitions) evalForOffset(offset int) {
	if offset < 0 {
		return
	}

	filename := fdfns.File.Filename
	pkgNode := fdfns.dfns.pkgNode
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
	fdfns *FileDefinitions
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
	key      ast.Node
	keyAlias ast.Node
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
	// ranges tracks the file ranges covered by this astNode
	ranges *rangeset.FilenameRangeSet
	// ellipses contains navigableBindings for ellipsis patterns.
	ellipses []*navigableBindings
	// fieldsAllowed indicates whether this astNode models a lexical
	// scope in the AST in which fields could exist. This is used for
	// completions: fields will only be suggested if
	// fieldsAllowed. Within a ListLit, for example, fields are not
	// allowed..
	fieldsAllowed bool
	docComments   []*ast.CommentGroup
}

// newAstNode creates a new [astNodes] which is a child of the current
// astNode. This is a light wrapper around
// [Definitions.newAstNode]. See those docs for more details on the
// arguments to this function.
func (n *astNode) newAstNode(key ast.Node, unprocessed ast.Node, navigable *navigableBindings) *astNode {
	s := n.fdfns.newAstNode(n, key, unprocessed, navigable)
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

	printf("Node %p", n)
	printf(" Ranges %v", n.ranges)

	nav := n.navigable
	if len(nav.bindings) > 0 {
		printf(" Navigable: %p %q", nav, nav.name)
		for name, bindings := range nav.bindings {
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

// navigableBindings groups together astNodes, and itself is a node in
// a graph (directed, acyclic) of navigableBindings. The zero value is
// ready for use.
type navigableBindings struct {
	dfns *Definitions
	// parent is the parent navigableBindings. The graph of
	// navigableBindings can be different from the graph of astNodes,
	// because two astNodes in a parent-child relationship can reuse
	// the same navigableBindings. A good example of this is:
	//
	//	x: y & z
	//
	// Here, the astNode for the x field-value will create two child
	// astNodes, one for each of y and z, but all three will use the
	// same navigableBindings.
	parent *navigableBindings
	// bindings contains all bindings for this navigableBindings
	// node. These bindings are "merged"; for example:
	//
	//	x: a
	//	x: b
	//
	// There would only be one navigableBinding that covers both x
	// field-values. This is in contrast to [astNode], where bindings
	// are not merged: there would be two bindings (astNodes) for x.
	bindings map[string]*navigableBindings
	// contributingNodes are the astNodes that contribute to this
	// navigableBindings. It is an invariant that every member of
	// contributingNodes has its navigable field set to this
	// navigableBindings. It is also an invariant that every astNode
	// that has a particular navigableBindings value in its navigable
	// field will appear in that navigableBinding's contributingNodes.
	contributingNodes []*astNode
	// name is the identifier name for this binding. This may be the
	// empty string if this navigableBinding itself does not appear in
	// its parent's bindings. A good example of this is a let
	// expression:
	//
	//	let x = 3
	//
	// The astNode containing this expression will have its own binding
	// for x to a child astNode. That child astNode will have a fresh
	// navigableBinding, but that navigableBinding will not appear in
	// the parent astNode's own navigableBinding's bindings. This is
	// because navigableBindings are used for resolving
	// non-first-elements of a path, and let expressions (amongst
	// others) introduce bindings that are not visible to
	// non-first-path-elements.
	name   string
	usedBy map[ast.Node]*astNode
}

func (nav *navigableBindings) recordUsage(e ast.Node, node *astNode) {
	if nav.usedBy == nil {
		nav.usedBy = make(map[ast.Node]*astNode)
	}
	nav.usedBy[e] = node
}

// addDefinition records that the target navigableBindings are the
// definitions for the file and all offsets between the start and end
// positions. Existing definitions for those offsets are overwritten
// without warning.
func (n *astNode) addDefinition(start, end token.Pos, targets []*navigableBindings) {
	if len(targets) == 0 || start == token.NoPos || end == token.NoPos {
		return
	}

	startPosition := start.Position()
	definitions := n.fdfns.definitions

	endOffset := end.Position().Offset
	for offset := startPosition.Offset; offset < endOffset; offset++ {
		definitions[offset] = targets
	}
}

// addEmbedCompletions records that both node (and all its parents)
// and targets are considered to contain bindings which should be
// offered as completions for any completion request between the start
// and end positions.
//
// The first element of a path must resolve lexically (if it's an
// ident). For this, the node parameter is provided, and all its
// ancestors are also considered. For subsequent elements of a path,
// the targets parameter is provided, which contains all the
// navigables resulting from resolving the prior elements of the
// selector.
//
// For a non-first path element, the start position will include the
// . so that completions are presented as soon as the user types
// ".". The startOffset parameter contains the position of the start
// of the existing path element so that the LSP server can send
// completions to the client that correctly edit the existing path.
func (n *astNode) addEmbedCompletions(start, end token.Pos, node *astNode, targets []*navigableBindings, startOffset token.Pos) {
	if (len(targets) == 0 && node == nil) || start == token.NoPos || end == token.NoPos {
		return
	}

	startPosition := start.Position()
	completions := n.fdfns.completions

	endOffset := end.Position().Offset
	for offset := startPosition.Offset; offset < endOffset; offset++ {
		r, found := completions[offset]
		if !found {
			r = &completionResolutions{}
			completions[offset] = r
		}
		r.embedNavigables = targets
		r.embedNode = node
		r.embedEndOffset = endOffset
		r.startOffset = startOffset.Position().Offset
	}
}

// addFieldCompletions records that the target navigableBindings are
// considered to contain bindings which should be offered for any
// completion request between the start and end positions. colonPos
// contains the position of the colon which follows the existing field
// name (probably slightly beyond the end position), and is used to
// ensure that when the LSP server suggests completions to the client,
// those completions correctly edit the existing field.
func (n *astNode) addFieldCompletions(start, end token.Pos, targets []*navigableBindings, colonPos token.Pos) {
	if len(targets) == 0 || start == token.NoPos || end == token.NoPos {
		return
	}

	startPosition := start.Position()
	completions := n.fdfns.completions

	endOffset := end.Position().Offset
	for offset := startPosition.Offset; offset < endOffset; offset++ {
		r, found := completions[offset]
		if !found {
			r = &completionResolutions{}
			completions[offset] = r
		}
		r.fieldNavigables = targets
		r.fieldEndOffset = colonPos.Position().Offset
		r.startOffset = startPosition.Offset
	}
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
	return n.isFileNode() || (ranges != nil && ranges.Contains(filename, offset))
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
	// This maps from clauses we process in this astNode, to the
	// remains of the corresponding comprehension that should be passed
	// to some child astNode. See the ast.Comprehension case below.
	//
	// Say we have Comprehension{Clauses: [A,B,C], Value: D} in our
	// list of unprocessed nodes. When we encounter it, clause A will
	// go into our unprocessed list, and comprehensionsStash[A] =
	// Comprehension{Clauses: [B,C], Value: D}. Then, when we then
	// process A, we can find this tail of the comprehension and pass
	// that to some child astNode.
	//
	// The base-case is when we have Comprehension{Clauses: [C], Value:
	// D} in our list of unprocessed nodes. When we process it, C will
	// go into our list of unprocessed nodes as normal, and
	// comprehensionsStash[C] = D. So then when we process C, again
	// we'll be able to find the tail - D - and pass that to the
	// appropriate astNode.
	var comprehensionsStash map[ast.Node]ast.Node

	for len(unprocessed) > 0 {
		node := unprocessed[0]
		unprocessed = unprocessed[1:]

		n.addRange(node)

		switch node := node.(type) {
		case *ast.File:
			for _, decl := range node.Decls {
				unprocessed = append(unprocessed, decl)
			}
			n.fieldsAllowed = true

		case *ast.Package:
			// Package declarations must be added to the pkgDecls
			// navigable, so that they can all be found when resolving
			// imports of this package, in some other package.
			child := n.newAstNode(node.Name, nil, n.fdfns.dfns.pkgDecls)
			child.addDocComments(node)
			n.addDefinition(node.Name.Pos(), node.Name.End(), []*navigableBindings{child.navigable})

		case *ast.ImportDecl:
			for _, spec := range node.Specs {
				unprocessed = append(unprocessed, spec)
			}

		case *ast.ImportSpec:
			// We process import specs twice, for laziness reasons: we
			// avoid the possibility that evaluating a filenode would
			// lookup every imported package and evaluate its filenodes
			// (which themselves might do the same...).
			dfns := n.fdfns.dfns
			if n.isFileNode() {
				// 1) At the filenode level, the first time we see the
				// ImportSpec, we create appropriate file-scope bindings,
				// but also pass the spec as the unprocessed value to a
				// fresh child node;
				ip := dfns.parseImportSpec(node)
				if ip == nil {
					break
				}
				importSpecNodes := dfns.importSpecNodes
				nav, found := importSpecNodes[*ip]

				var child *astNode
				if alias := node.Name; alias != nil && alias.Name != "" {
					child = n.newAstNode(alias, node, nav)
					n.appendBinding(alias.Name, child)
				} else if ip.Qualifier != "" {
					child = n.newAstNode(node.Path, node, nav)
					n.appendBinding(ip.Qualifier, child)
				}
				if child != nil {
					child.keyAlias = node
					if !found {
						importSpecNodes[*ip] = child.navigable
					}
				}

			} else {
				// 2) In that child node, the second time we see the
				// ImportSpec, we lookup the package imported and add a
				// resolution to them.
				str, err := strconv.Unquote(node.Path.Value)
				var remotePkgDfns *Definitions
				if err == nil {
					remotePkgDfns = dfns.forPackage(str)
					if remotePkgDfns != nil {
						remotePkgDfns.bootFiles()
						// The pkg exists. We've booted it which means that
						// its pkgDecls now have contributingNodes which are
						// the package declarations from every file in that
						// package.
					}
				}
				if remotePkgDfns == nil {
					// Something went wrong. We create a fake dfns to
					// handle this, so that elsewhere we can treat all
					// imports the same, regardless of whether they were
					// successful or not. Essentially, unsuccessful imports
					// just appear as empty packages.
					//
					// If we really need to, we can tell that the import
					// was successful or not from its dfns:
					//
					// 1. a bad import will have an empty ip (importPath) field
					// 2. a bad import will have pkgDecls with no contributingNodes
					remotePkgDfns = Analyse(ast.ImportPath{}, nil, nil, nil)
				}

				// We add a definition from this import to the package
				// declarations in the remote package.
				n.addDefinition(n.key.Pos(), n.key.End(), []*navigableBindings{remotePkgDfns.pkgDecls})
				// We also record that we are using those package
				// decls. This means that from the result of resolving the
				// import spec, we can always get back to this astNode n,
				// and thus find our ImportSpec in n.keyAlias.
				remotePkgDfns.pkgDecls.recordUsage(node, n)
			}

		case *ast.StructLit:
			for _, elt := range node.Elts {
				unprocessed = append(unprocessed, elt)
			}
			n.fieldsAllowed = true

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
					Label:    &ast.Ident{NamePos: elt.Pos(), Name: "__" + fmt.Sprint(i)},
					TokenPos: elt.Pos(),
					Token:    token.COLON,
					Value:    elt,
				})
			}
			n.fieldsAllowed = false

		case *ast.Interpolation:
			resolvable = append(resolvable, node.Elts...)

		case *ast.EmbedDecl:
			unprocessed = append(unprocessed, node.Expr)

		case *ast.PostfixExpr:
			if node.Op == token.ELLIPSIS {
				unprocessed = append(unprocessed, node.X)
			} else {
				// Currently should never happen as Postfix is only used
				// with ellipses. Just in case that changes, behave the
				// same as Unary.
				n.newAstNode(nil, node.X, nil)
			}

		case *ast.ParenExpr:
			unprocessed = append(unprocessed, node.X)

		case *ast.UnaryExpr:
			n.newAstNode(nil, node.X, nil)

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
				n.newAstNode(nil, node.X, nil)
				n.newAstNode(nil, node.Y, nil)
			}

		case *ast.Alias:
			// X=e (the old deprecated alias syntax)
			n.newBinding(node.Ident.Name, node.Ident, node.Expr)

		case *ast.Ellipsis:
			child := n.newAstNode(node, node.Type, nil)
			n.ellipses = append(n.ellipses, child.navigable)

		case *ast.CallExpr:
			resolvable = append(resolvable, node.Fun)
			for _, arg := range node.Args {
				n.newAstNode(nil, arg, nil)
			}

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, node.(ast.Expr))

		case *fieldDeclExpr:
			parent := n.parent
			key := parent.key
			if node.colonPos != token.NoPos {
				navs := expandNavigableViaAncestralPath(parent.navigable.parent)
				n.addFieldCompletions(key.Pos(), key.End(), navs, node.colonPos.Add(1))
			}

			navs := expandNavigableViaAncestralPath(parent.navigable)
			n.addDefinition(key.Pos(), key.End(), navs)

			if keyAlias := parent.keyAlias; keyAlias != nil {
				n.addDefinition(keyAlias.Pos(), keyAlias.End(), navs)
				for _, nav := range navs {
					nav.recordUsage(keyAlias, n)
				}
			}

		case *ast.Comprehension:
			clause := node.Clauses[0]
			unprocessed = append(unprocessed, clause)
			// We don't know how many child astNodes we'll need to
			// process clause. So we stash whatever remains of this
			// comprehension and can later find it once we've finished
			// processing our clause.
			if comprehensionsStash == nil {
				comprehensionsStash = make(map[ast.Node]ast.Node)
			}
			if len(node.Clauses) == 1 {
				// Base-case: we're dealing with the last clause. So that
				// clause gets processed in this node, and we make sure we
				// can later use that last clause to find the body (value)
				// of this comprehension.
				comprehensionsStash[clause] = node.Value
			} else {
				// Non-base-case: we're processing the first clause in
				// this node, and all that remain go into a copy of the
				// comprehension, which we find later and pass to an
				// appropriate child/descendant.
				nodeCopy := *node
				nodeCopy.Clauses = node.Clauses[1:]
				comprehensionsStash[clause] = &nodeCopy
			}

		case *ast.IfClause:
			n.newAstNode(nil, node.Condition, nil)

			comprehensionTail := comprehensionsStash[node]
			n.newAstNode(nil, comprehensionTail, n.navigable)

		case *ast.ForClause:
			n.newAstNode(nil, node.Source, nil)

			stack := astNodeStack{n.newAstNode(nil, nil, nil)}

			if key := node.Key; key != nil {
				stack.push(key, stack.peek().newBinding(key.Name, key, nil))
			}
			if val := node.Value; val != nil {
				stack.push(val, stack.peek().newBinding(val.Name, val, nil))
			}

			comprehensionTail := comprehensionsStash[node]
			stack.push(comprehensionTail, stack.peek().newAstNode(nil, comprehensionTail, n.navigable))

		case *ast.LetClause:
			ident := node.Ident
			// A let clause might or might not be within a comprehension.
			if comprehensionTail, found := comprehensionsStash[node]; found {
				// We're within a wider comprehension.
				n.newAstNode(nil, node.Expr, nil)

				stack := astNodeStack{n.newAstNode(nil, nil, nil)}
				stack.push(ident, stack.peek().newBinding(ident.Name, ident, nil))
				stack.push(comprehensionTail, stack.peek().newAstNode(nil, comprehensionTail, n.navigable))

			} else {
				// We're not within a wider comprehension: the binding
				// must be added to the current node n because we need to
				// be able to find it from the first element of a path.
				n.newBinding(ident.Name, ident, node.Expr)
			}

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
				binding = n.ensureNavigableBinding(label.Name, label, node.Value, node.TokenPos)
			case *ast.BasicLit:
				name, _, err := ast.LabelName(label)
				if err == nil {
					binding = n.ensureNavigableBinding(name, label, node.Value, node.TokenPos)
				}
			}

			if isAlias {
				switch alias.Expr.(type) {
				case *ast.ListLit:
					// X=[e]: field
					// X is only visible within field
					wrapper := n.newAstNode(nil, nil, nil)
					binding = wrapper.newBinding(alias.Ident.Name, alias.Ident, node.Value)
					wrapper.addRange(node)
				case ast.Label:
					// X=ident: field
					// X="basic": field
					// X="\(e)": field
					// X=(e): field
					// X is visible within s
					if binding == nil {
						binding = n.newBinding(alias.Ident.Name, alias.Ident, node.Value)
					} else {
						n.appendBinding(alias.Ident.Name, binding)
						binding.addRange(alias.Ident)
						binding.keyAlias = alias.Ident
					}
				}
			}
			if binding == nil {
				binding = n.newAstNode(label, node.Value, nil)
			}

			binding.addDocComments(node)

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
					wrapper.addRange(node)
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
						wrapper.addRange(node)
						binding.parent = wrapper
					} else {
						resolvable = append(resolvable, elt)
					}
				}
			}
		}
	}

	for _, expr := range embeddedResolvable {
		nodes := n.resolve(expr, true)
		n.resolvesTo = append(n.resolvesTo, nodes...)
	}
	for _, expr := range resolvable {
		n.resolve(expr, false)
	}
}

// resolve resolves the given expression into a slice of navigable
// bindings. It is a slice because a single expression may resolve to
// several unrelated navigable bindings. For example the expression `x
// & y`.
func (n *astNode) resolve(e ast.Expr, maybeField bool) []*navigableBindings {
	switch e := e.(type) {
	case *ast.Ident:
		if maybeField && n.fieldsAllowed {
			n.addFieldCompletions(e.Pos(), e.End(), expandNavigableViaAncestralPath(n.navigable), e.End())
		}
		n.addEmbedCompletions(e.Pos(), e.End(), n, nil, e.Pos())
		root := n.resolvePathRoot(e.Name)
		if root == nil {
			return nil
		}
		navs := []*navigableBindings{root}
		n.addDefinition(e.Pos(), e.End(), navs)
		root.recordUsage(e, n)
		return navs

	case *ast.SelectorExpr:
		resolved := n.resolve(e.X, false)
		sel := e.Sel
		// 1. Add a completion from the end of e.X to the start of the
		// selector, which is "0 width". This basically covers just the
		// . in the SelectorExpr. So when the user presses . and we
		// present completions, they will be purely inserting text and
		// not replacing any existing text.
		n.addEmbedCompletions(e.X.End(), sel.Pos(), nil, resolved, sel.Pos())
		// 2. Add a completion for the selector, which is the width of
		// the selector. This means when the user is within the selector
		// part, the completion will replace any existing part of the
		// selector.
		n.addEmbedCompletions(sel.Pos(), sel.End(), nil, resolved, sel.Pos())
		name, _, err := ast.LabelName(sel)
		if err != nil {
			return nil
		}

		results := navigateBindingsByName(resolved, name)
		n.addDefinition(sel.Pos(), sel.End(), results)
		for _, nav := range results {
			nav.recordUsage(sel, n)
		}
		return results

	case *ast.IndexExpr:
		resolved := n.resolve(e.X, false)
		lit, ok := e.Index.(*ast.BasicLit)
		if !ok {
			// If it's a path/ident etc, we don't attempt to calculate
			// the dynamic index.
			n.resolve(e.Index, false)
			return nil
		}
		name := "__" + lit.Value
		if lit.Kind != token.INT {
			var err error
			name, _, err = ast.LabelName(lit)
			if err != nil {
				return nil
			}
		}

		results := navigateBindingsByName(resolved, name)
		n.addDefinition(e.Lbrack, e.Rbrack.Add(1), results)
		for _, nav := range results {
			nav.recordUsage(e.Index, n)
		}
		return results

	default:
		return []*navigableBindings{n.newAstNode(nil, e, nil).navigable}
	}
}

// expandNavigables maximally expands the provided set of navigables:
// transitively inspecting all the astNodes that contribute to each
// navigable, evaluating them and their resolvesTo navigables. This
// expands a set of navigables to every navigable that can be reached
// (transitively) via embedding.
func expandNavigables(navigables []*navigableBindings) map[*navigableBindings]struct{} {
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

		// evaluating a node X can append new nodes onto X's
		// navigable.contributingNodes. So we need to make sure we
		// evaluate and expand into those too. I.e. calling node.eval()
		// can modify nav.contributingNodes, thus we don't use range.
		for i := 0; i < len(nav.contributingNodes); i++ {
			node := nav.contributingNodes[i]

			node.eval()
			navigables = append(navigables, node.resolvesTo...)

			if spec, ok := node.keyAlias.(*ast.ImportSpec); ok {
				str, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				dfns := node.fdfns.dfns.forPackage(str)
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
	return navigableSet
}

// expandNavigableViaAncestralPath calculates the set of
// navigableBindings with which the argument is unified. Note this is
// imperfect: for example it will not attempt to explore patterns.
func expandNavigableViaAncestralPath(nav *navigableBindings) []*navigableBindings {
	var names []string
	for ; nav != nil && nav.name != ""; nav = nav.parent {
		names = append(names, nav.name)
	}
	navs := []*navigableBindings{nav}
	for _, name := range slices.Backward(names) {
		navs = navigateBindingsByName(navs, name)
	}
	return navs
}

// navigateBindingsByName maximally expands the set of bindings, and
// indexes every member of the expanded set by the name, and the
// accumulated results returned.
func navigateBindingsByName(navigables []*navigableBindings, name string) []*navigableBindings {
	navigableSet := expandNavigables(navigables)

	var results []*navigableBindings
	for navigable := range navigableSet {
		childNav, found := navigable.bindings[name]
		if found {
			results = append(results, childNav)
		}
		for _, node := range navigable.contributingNodes {
			childNodes, found := node.bindings[name]
			// if there is a binding, we still add the ellipses if the
			// binding is for a pattern, alias, comprehension etc.
			addEllipses := !found || (len(childNodes) == 1 && childNodes[0].navigable != childNav)
			if addEllipses {
				results = append(results, node.ellipses...)
			}
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
	nOrig := n
	for ; n != nil; n = n.parent {
		if bindings, found := n.bindings[name]; found {
			nav := bindings[0].navigable
			if len(bindings) == 1 {
				if nav.name == "" {
					// name has been resolved to an alias (or comprehension
					// binding, dynamic field, pattern etc). Crucially, it
					// doesn't have a "navigable" name.
					return nav
				} else if nav.name != name {
					// name has been resolved to an alias which had a
					// normal ident or basiclit field name. Switch to that
					// name.
					return n.navigable.bindings[nav.name]
				}
			}

			// If name lexically matches a non-alias, it must be matching
			// an ident and not a basiclit. But that ident can come from
			// any of the (potentially many) matching bindings!
			identFound := false
			for _, binding := range bindings {
				if _, ok := binding.key.(*ast.Ident); ok {
					identFound = true
					break
				}
			}
			if !identFound {
				continue
			}
			return nav
		}
		if n.isFileNode() {
			// If we've got this far, we're allowed to inspect the
			// (shared) navigable bindings directly without having to go
			// via our bindings.
			if nav, found := n.navigable.bindings[name]; found {
				return nav
			}
			// Support for the Self experiment:
			if name == "self" && nOrig.key != nil && nOrig.key.Pos().Experiment().AliasAndSelf {
				return nOrig.navigable.parent
			}
			return nil
		}
	}
	return nil
}

// isFileNode reports whether n is a direct child of the package
// astNode.
func (n *astNode) isFileNode() bool {
	return n.parent == nil || n.parent == n.fdfns.dfns.pkgNode
}

// ensureNavigableBinding creates and returns a new [astNode],
// locating and using the appropriate shared [navigableBindings] for
// the given name. The new node is stored in the node's bindings.
func (n *astNode) ensureNavigableBinding(name string, key ast.Label, unprocessed ast.Node, colonPos token.Pos) *astNode {
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

	// Search for the nav for the new binding.
	nav, found := bindings[name]
	binding := n.newAstNode(key, unprocessed, nav)

	if !strings.HasPrefix(name, "__") {
		// If binding name starts with __ then we assume we artificially
		// created it when converting a list's elements to struct
		// fields. A list element doesn't have a key in the source, so
		// there's no need to add a fieldDeclExpr for resolving that
		// key.
		expr := &fieldDeclExpr{position: key, colonPos: colonPos}
		binding.newAstNode(key, expr, nil)
	}

	if !found {
		// If the new binding has a new navigable, store it in our
		// bindings, under name.
		binding.navigable.name = name
		bindings[name] = binding.navigable
	} else if name != binding.navigable.name {
		panic(fmt.Sprintf("Navigable name is %q but it should be %q", binding.navigable.name, name))
	}
	n.appendBinding(name, binding)

	return binding
}

// newBinding creates and returns a new [astNode], and stores it under
// the given name in the current astNode only.
func (n *astNode) newBinding(name string, key ast.Node, unprocessed ast.Node) *astNode {
	binding := n.newAstNode(key, unprocessed, nil)
	n.appendBinding(name, binding)
	if !strings.HasPrefix(name, "__") {
		// Same logic as in [astNode.ensureNavigableBinding] above;
		expr := &fieldDeclExpr{position: key, colonPos: token.NoPos}
		binding.newAstNode(key, expr, nil)
	}
	return binding
}

// appendBinding stores the binding under the given name in the
// current astNode only.
func (n *astNode) appendBinding(name string, binding *astNode) {
	binding.fieldsAllowed = true
	if n.bindings == nil {
		n.bindings = make(map[string][]*astNode)
	}
	n.bindings[name] = append(n.bindings[name], binding)
}

func (n *astNode) addDocComments(node ast.Node) {
	var comments []*ast.CommentGroup
	for _, group := range ast.Comments(node) {
		if group.Doc && len(group.List) > 0 && group.List[0].Pos().Before(node.Pos()) {
			comments = append(comments, group)
		}
	}
	n.docComments = comments
}

// fieldDeclExpr is a temporary representation of a field
// declaration's key, used inside [astNode.ensureNavigableBinding] and
// [astNode.resolve]. The position is holds the position of the key,
// and the expression is always nil.
type fieldDeclExpr struct {
	// Always nil: make the struct implement [ast.Expr]
	ast.Expr
	position ast.Node
	// colonPos is the position of the colon that follows the field's
	// label.
	colonPos token.Pos
}

var _ ast.Node = (*fieldDeclExpr)(nil)

func (w *fieldDeclExpr) Pos() token.Pos {
	return w.position.Pos()
}

func (w *fieldDeclExpr) End() token.Pos {
	return w.position.End()
}

type astNodeStack []*astNode

func (stack *astNodeStack) push(n ast.Node, node *astNode) {
	nodes := *stack
	for _, node := range nodes {
		node.addRange(n)
	}
	*stack = append(nodes, node)
}

func (stack *astNodeStack) peek() *astNode {
	nodes := *stack
	if len(nodes) == 0 {
		return nil
	}
	return nodes[len(nodes)-1]
}
