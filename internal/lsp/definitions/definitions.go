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
// is a CUE expression followed by zero or more idents, all chained
// together by dots.
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
// of dynamic fields, even when it is trivial to do so statically.
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
// In this evaluator, a "dynamic scope" is a collection of
// bindings, i.e. key-value pairs. The values are themselves
// dynamic scopes.  A dynamic scope is created with one or
// more unprocessed [ast.Node] values, for example, an [ast.File], or
// an [ast.StructLit].
//
// When a dynamic scope is evaluated, each of these nodes is
// unpacked. An [ast.StructLit] for example contains a number of
// [ast.Decl] nodes, which are themselves then processed. When a
// dynamic scope encounters an [ast.Field], the dynamic scope ensures
// a binding exists for the field's name, and adds the field's value
// to the binding's dynamic-scope's unprocessed values. Note that
// evaluation of a dynamic scope is not recursive: its bindings are
// not automatically evaluated.
//
// If, during evaluation, a dynamic scope encounters a path, the path
// will correspond either to the value of a field (i.e. the scope is
// for something like x: y), or an embedding into a struct. The
// dynamic scope keeps track of these embedded paths and once
// processing of the scope's nodes is complete, it then resolves the
// embedded paths to dynamic scopes, and records that this scope
// itself resolves to these other scopes.
//
// The consequence is that the evaluation of a dynamic scope creates
// and fully populates (with their unprocessed nodes) all of its
// bindings before any resolution of paths occurs. Thus evaluation can
// be driven by demand: if a path is encountered that accesses one of
// the scope's bindings (or any binding of an ancestor scope), then it
// is guaranteed that the binding contains its complete set of nodes
// before it is accessed, and so it is safe to evaluate.
//
// Consider this example:
//
//	x: y
//	y: {
//		a: 3
//		b: y.a
//	}
//
// Evaluating the outermost dynamic scope will create two bindings,
// one for x (containing just the path y), and one for y (containing
// the [ast.StructLit]). If the scope for y is evaluated, it will
// create its own bindings for a (containing the [ast.BasicLit] 3),
// and for b (containing the path y.a).
//
// Imagine we want to evaluate, in the outermost scope, the path
// x.a. We first evaluate the outermost dynamic scope, then inspect
// its bindings. We find an x in there, so we grab that scope. This
// completes resolving the x of x.a. We now wish to find an a within
// this dynamic scope, so we evaluate it. This scope contains only the
// path y and so we have to resolve y and record within the scope what
// it has resolved to.
//
// Every dynamic scope knows its own parent dynamic scope. This
// dynamic scope containing y will inspect its own bindings for y, and
// find nothing. It asks its ancestors whether they know of a binding
// for y. The parent scope does have a binding for y, so we grab that
// scope. This completes the resolution of y, and thus the evaluation
// of the scope that contains y. We now ask this scope whether it
// contains a binding for a. It doesn't, but we also inspect all the
// scopes that this scope resolves to. There is one resolved-to scope,
// and it does contain a binding for a, so we grab that. This
// completes the resolution of x.a.
//
// In summary: this algorithm traverses the AST breadth first and
// incrementally, to lazily merge together bindings that share the
// same path into dynamic scopes.
//
// Unmentioned is that there are various [ast.Expr] types that can use
// paths but not declare their own bindings, for example an
// interpolated string. When these are encountered during evaluation,
// the dynamic scope accumulates and processes them in the same way as
// embedded paths. The only difference is they don't need to be
// recorded within the scope's resolves-to set.
//
// # Querying
//
// In the previous section, we walked through the example of
// attempting to resolve the path x.a in the outermost dynamic
// scope. But this isn't what an LSP client will ask. An LSP client
// doesn't know what path the cursor is on, nor anything about the
// current scope. The LSP client knows only the cursor's line and
// column number.
//
// To facilitate an API that allows querying by file-coordinates,
// dynamic scopes are extended with a rangeset. For each [ast.Node]
// that a scope processes, it adds to its rangeset the range from the
// node's start file-offset to its end file-offset. Then, when asked
// to resolve whatever lies at some file-coordinate, we only need to
// evaluate the scopes that contain the file-coordinate in question.
//
// It's important to keep in mind that a dynamic scope may contain
// nodes from multiple different lexical scopes, across all files in
// the same package. A dynamic scope is evaluated and traversed into,
// if *any* of its node's ranges contain the file-coordinate in
// question. Thus a dynamic scope does not solely evaluate nodes that
// are lexical ancestors of the file-coordinate. This ensures the a
// dynamic scope is fully evaluated before it is queried.
//
// # Algorithm 2: real CUE
//
// If I stuck to algorithm 1, it would mean that in:
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
// Here, l₃ resolves to l₁, or b. So the rule that the first element
// of any path can only be resolved lexically must be implemented (if
// that element is an ident). This means that this evaluator must
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
// having a distinct (dynamic) lexical scope. Both of those scopes
// also share a "navigable scope" struct and so any children that
// either of these bindings have, can be grouped together
// appropriately via their shared "navigable scope". Thus in this
// example, the evaluation of the outermost lexical scope creates two
// bindings for x; their distinct (dynamic) lexical scopes share a
// "navigable scope", and also have one binding each for their
// respective y fields. These y fields are grouped together within the
// shared "navigable scope".
//
// This means that when resolving the first element of a path I can
// walk up the lexical bindings only, and then once that's resolved,
// switch to the navigable bindings for the rest of the path.
//
// The rangeset field, which in Algorithm 1 was part of the dynamic
// scope, is moved to the "navigable scope" type, thus it still
// collects together the ranges of every node that contributes to the
// navigable scope. This is essential to ensure that all lexical
// scopes that share a navigable scope are fully evaluated before the
// navigable scope is queried.
//
// For aliases, comprehensions and one or two other things, a
// lexically-scoped binding without a navigable binding is created. A
// navigable binding is always also a lexical binding, but a lexical
// binding need not be a navigable binding.
//
// # File and Package scopes
//
// CUE states that fields declared at the top level of a file are not
// in the file's scope, but are in fact in the package's scope. At
// construction, the file scopes all share a "navigable scope". Thus
// if two different files in the same package both declare the same
// field, they will be correctly grouped together within that
// navigable scope.
//
// When a file scope processes an [ast.File], lexical and navigable
// bindings will be created as normal within the file scope. When
// resolving the first element of a path in some deeper scope, it can
// be the case that after walking up the chain of ancestor lexical
// scopes, no matching lexical binding is found even within the
// relevant file scope's lexical bindings. In this case, it is safe to
// directly inspect the file scope's navigable bindings, which amount
// to the package's lexical bindings. In this way, a path's first
// element can be an ident that is only declared in some separate file
// within the same package, and yet it can still be resolved.
package definitions

import (
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/rangeset"
)

// Definitions provides methods to resolve file offsets to their
// definitions.
type Definitions struct {
	// pkgScope is the top level (or root) lexical scope
	pkgScope *lexicalScope
	// byFilename maps file names to [FileDefinitions]
	byFilename map[string]*FileDefinitions
}

// Analyse creates and performs initial configuration of a new
// [Definitions] value. It does not perform any analysis eagerly. All
// files provided will be treated as if they are part of the same
// package. The set of files cannot be modified after construction;
// instead, construction is cheap, so the intention is you replace the
// whole Definitions value.
func Analyse(files ...*ast.File) *Definitions {
	dfns := &Definitions{
		byFilename: make(map[string]*FileDefinitions, len(files)),
	}

	pkgScope := dfns.newLexicalScope(nil, nil, nil, nil)
	dfns.pkgScope = pkgScope
	navigable := &navigableScope{}

	for _, file := range files {
		pkgScope.newLexicalScope(nil, file, navigable)
		dfns.byFilename[file.Filename] = &FileDefinitions{
			pkgScope:    pkgScope,
			resolutions: make([][]ast.Node, file.End().Offset()),
			File:        file,
		}
	}

	return dfns
}

// newLexicalScope creates a new [lexicalScope]. All arguments may be
// nil; if navigable is nil, then a new navigable will be created and
// used within the new scope.
func (dfns *Definitions) newLexicalScope(parent *lexicalScope, key ast.Node, unprocessed ast.Node, navigable *navigableScope) *lexicalScope {
	if navigable == nil {
		navigable = &navigableScope{}
	}
	s := &lexicalScope{
		dfns:      dfns,
		parent:    parent,
		navigable: navigable,
	}
	if unprocessed != nil {
		s.unprocessed = []ast.Node{unprocessed}
	}
	if key != nil {
		s.key = key
		s.addRange(key)
	}
	return s
}

// addResolution records that the target scopes are the definitions
// for the file and offset of the start token, and its given length.
func (dfns *Definitions) addResolution(start token.Pos, length int, targets []*lexicalScope) {
	if len(targets) == 0 {
		return
	}

	startPosition := start.Position()
	filename := startPosition.Filename
	offsets := dfns.byFilename[filename].resolutions
	startOffset := startPosition.Offset
	var keys []ast.Node
	for _, scope := range targets {
		keys = append(keys, scope.key)
	}
	for i := range length {
		offsets[startOffset+i] = keys
	}
}

// ForFile looks up the [FileDefinitions] for the given filename.
func (dfns *Definitions) ForFile(filename string) *FileDefinitions {
	return dfns.byFilename[filename]
}

// FileDefinitions provides methods to resolve file offsets, within a
// certain file, to their definitions.
type FileDefinitions struct {
	pkgScope *lexicalScope
	// resolutions caches the results of previous lookups, ensuring
	// that subsequent calls to [ForOffset] for a given offset are
	// O(1).
	resolutions [][]ast.Node
	// File is the original [ast.File] that was passed to [Analyse].
	File *ast.File
}

// ForOffset reports the definitions that the file offset resolves to.
func (fdfns *FileDefinitions) ForOffset(offset int) []ast.Node {
	if offset < 0 || offset >= len(fdfns.resolutions) {
		return nil
	}
	nodes := fdfns.resolutions[offset]
	if nodes != nil {
		return nodes
	}
	fdfns.resolutions[offset] = []ast.Node{}

	filename := fdfns.File.Filename
	pkgScope := fdfns.pkgScope
	pkgScope.eval()
	seen := make(map[*lexicalScope]struct{})
	worklist := []*lexicalScope{pkgScope}
	for len(worklist) > 0 {
		s := worklist[0]
		worklist = worklist[1:]

		if _, found := seen[s]; found {
			continue
		}
		seen[s] = struct{}{}

		for _, s := range s.allScopes {
			s.eval()
			if s.contains(filename, offset) {
				worklist = append(worklist, s)
			}
		}
	}

	//pkgScope.dump(1)

	return fdfns.resolutions[offset]
}

// lexicalScope models a lexical scope.
type lexicalScope struct {
	dfns *Definitions
	// parent is the lexical parent scope.
	parent *lexicalScope
	// unprocessed holds the nodes that make up this scope. Once a call
	// to [scope.eval] has returned, unprocessed must never be
	// modified.
	unprocessed []ast.Node
	// key is the position that is considered to define this scope. For
	// example, if a scope represents `a: {}` then key is set to the
	// `a` ident. This can be nil, such as when an expression is being
	// evaluated. For example in the path {a: 3, b: a}.b, a
	// lexicalScope with no key will be created, containing the
	// structlit {a: 3, b: a}.
	key ast.Node
	// A lexicalScope may have several names. For example, if a scope
	// is the result of a field with an alias, e.g. l=x: e, then in its
	// parent scope it'll be stored under both l and x. This name field
	// contains only the navigable name, in this case x. Sometimes, a
	// lexicalScope will have no navigable name, e.g. a let
	// declaration, which exists only as a lexical binding and not a
	// navigable binding.
	name string
	// resolvesTo points to the scopes reachable from nodes which are
	// embedded within this scope.
	resolvesTo []*lexicalScope
	// allScopes contains every scope that is a child of this scope.
	// When searching for a given file-offset, these scopes are tested
	// for whether they contain the desired file-offset.
	allScopes []*lexicalScope
	// bindings contains all bindings for this lexical scope. Note the
	// map's values are slices because a single lexical scope can have
	// multiple bindings for the same key. For example:
	//
	//	x: bool
	//	x: true
	bindings map[string][]*lexicalScope
	// navigable provides access to the "navigable scope" that is
	// shared between multiple lexicalScopes that should be considered
	// "merged together".
	navigable *navigableScope
}

// newLexicalScope creates a new [lexicalScope] which is a child of
// the current scope.
func (ls *lexicalScope) newLexicalScope(key ast.Node, unprocessed ast.Node, navigable *navigableScope) *lexicalScope {
	s := ls.dfns.newLexicalScope(ls, key, unprocessed, navigable)
	ls.allScopes = append(ls.allScopes, s)
	return s
}

// dump sends to stdout the current lexicalScope, its bindings, and
// allScopes, in a "pretty" indented fashion. This is for aiding
// debugging.
func (ls *lexicalScope) dump(depth int) {
	printf := func(f string, a ...any) {
		fmt.Printf("%*s%s\n", depth*3, "", fmt.Sprintf(f, a...))
	}

	printf("Scope %p (name: %q)", ls, ls.name)
	navigable := ls.navigable
	printf(" Ranges %v", navigable.ranges)

	if len(navigable.bindings) > 0 {
		printf(" Navigable: %p", ls.navigable)
		for name, bindings := range navigable.bindings {
			printf("  %s:", name)
			for _, binding := range bindings {
				binding.dump(depth + 1)
			}
		}
	}

	if len(ls.bindings) > 0 {
		printf(" Lexical:")
		for name, bindings := range ls.bindings {
			printf("  %s:", name)
			for _, binding := range bindings {
				binding.dump(depth + 1)
			}
		}
	}

	if len(ls.allScopes) > 0 {
		printf(" All scopes:")
		for _, s := range ls.allScopes {
			s.dump(depth + 1)
		}
	}
}

// A navigableScope groups together scopes and the ranges of their
// nodes. The zero value is ready for use.
type navigableScope struct {
	bindings map[string][]*lexicalScope
	ranges   *rangeset.FilenameRangeSet
}

// addRange records that the scope (particularly its navigableScope)
// covers the range from the node's start file-offset to its end
// file-offset.
func (ls *lexicalScope) addRange(n ast.Node) {
	start := n.Pos().Position()
	end := n.End().Position()

	rs := ls.navigable.ranges
	if rs == nil {
		rs = rangeset.NewFilenameRangeSet()
		ls.navigable.ranges = rs
	}

	rs.Add(start.Filename, start.Offset, end.Offset)
}

// contains reports whether the lscope (particularly its
// navigableScope) contains the given file-offset. It is intentional
// that this will report true if any [ast.Node] from any
// [lexicalScope] that share a [navigableScope] contains the given
// file-offset. This is because this method is used to test whether or
// not a given lexicalScope should be evaluated. We need to evaluate
// every lexicalScope that contributes to a navigableScope, regardless
// of whether any given lexicalScope directly contains nodes covering
// the file-offset.
//
// As a special case, the root lexicalScope (the pkgScope) always
// contains every file-offset.
func (ls *lexicalScope) contains(filename string, offset int) bool {
	ranges := ls.navigable.ranges
	return ls == ls.dfns.pkgScope || (ranges != nil && ranges.Contains(filename, offset))
}

// eval evaluates lazily the lexicalScope. Evaluation is not
// recursive: it does not evaluate child bindings. eval must be called
// a lexicalScope's bindings, allScopes, or resolvesTo fields are
// inspected, or before [lexicalScope.contains] is invoked. See also
// the package level documentation.
func (ls *lexicalScope) eval() {
	if ls.unprocessed == nil {
		return
	}

	unprocessed := ls.unprocessed
	ls.unprocessed = nil

	var embeddedResolvable, resolvable []ast.Expr

	for len(unprocessed) > 0 {
		n := unprocessed[0]
		unprocessed = unprocessed[1:]

		ls.addRange(n)

		switch n := n.(type) {
		case *ast.File:
			for _, decl := range n.Decls {
				unprocessed = append(unprocessed, decl)
			}

		case *ast.ImportDecl:
			for _, spec := range n.Specs {
				unprocessed = append(unprocessed, spec)
			}

		case *ast.ImportSpec:
			if n.Name == nil {
				str, err := literal.Unquote(n.Path.Value)
				if err != nil {
					continue
				}
				ip := ast.ParseImportPath(str).Canonical()
				if ip.Qualifier != "" {
					ls.newLexicalBinding(ip.Qualifier, n, nil)
				}
			} else {
				ls.newLexicalBinding(n.Name.Name, n, nil)
			}

		case *ast.StructLit:
			for _, elt := range n.Elts {
				unprocessed = append(unprocessed, elt)
			}

		case *ast.ListLit:
			for i, elt := range n.Elts {
				unprocessed = append(unprocessed, &ast.Field{
					Label:      ast.NewIdent(fmt.Sprint(i)),
					Constraint: token.ILLEGAL,
					TokenPos:   elt.Pos(),
					Token:      token.COLON,
					Value:      elt,
				})
			}

		case *ast.Interpolation:
			resolvable = append(resolvable, n.Elts...)

		case *ast.EmbedDecl:
			unprocessed = append(unprocessed, n.Expr)

		case *ast.ParenExpr:
			unprocessed = append(unprocessed, n.X)

		case *ast.UnaryExpr:
			resolvable = append(resolvable, n.X)

		case *ast.BinaryExpr:
			switch n.Op {
			case token.AND:
				unprocessed = append(unprocessed, n.X, n.Y)
			case token.OR:
				lhs := ls.newLexicalScope(nil, n.X, nil)
				rhs := ls.newLexicalScope(nil, n.Y, nil)
				ls.resolvesTo = append(ls.resolvesTo, lhs, rhs)
			default:
				resolvable = append(resolvable, n.X, n.Y)
			}

		case *ast.Alias:
			// X=e (the old deprecated alias syntax)
			ls.newLexicalBinding(n.Ident.Name, n.Ident, n.Expr)

		case *ast.Ellipsis:
			unprocessed = append(unprocessed, n.Type)

		case *ast.CallExpr:
			resolvable = append(resolvable, n.Args...)

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, n.(ast.Expr))

		case *ast.Comprehension:
			parent := ls
			for _, clause := range n.Clauses {
				cur := parent.newLexicalScope(nil, clause, nil)
				// We need to make sure that the comprehension value
				// (i.e. body) and all subsequent clauses, can be reached
				// by traversing through all clauses. The simplest way to
				// do this is just to include the whole range of n within
				// each descendent.
				cur.addRange(n)
				cur.eval()
				parent = cur
			}
			if parent != ls {
				child := parent.newLexicalScope(nil, n.Value, nil)
				ls.resolvesTo = append(ls.resolvesTo, child)
			}

		case *ast.IfClause:
			unprocessed = append(unprocessed, n.Condition)

		case *ast.LetClause:
			ls.newLexicalBinding(n.Ident.Name, n.Ident, n.Expr)

		case *ast.ForClause:
			if n.Key != nil {
				ls.newLexicalBinding(n.Key.Name, n.Key, nil)
			}
			if n.Value != nil {
				ls.newLexicalBinding(n.Value.Name, n.Value, nil)
			}
			resolvable = append(resolvable, n.Source)

		case *ast.Field:
			label := n.Label

			alias, isAlias := label.(*ast.Alias)
			if isAlias {
				if expr, ok := alias.Expr.(ast.Label); ok {
					label = expr
				}
			}

			var binding *lexicalScope
			switch label := label.(type) {
			case *ast.Ident:
				binding = ls.ensureNavigableBinding(label.Name, label, n.Value)
			case *ast.BasicLit:
				binding = ls.ensureNavigableBinding(label.Value, label, n.Value)
			default:
				binding = ls.newLexicalScope(label, n.Value, nil)
			}

			if isAlias {
				switch alias.Expr.(type) {
				case *ast.ListLit:
					// X=[e]: field
					// X is only visible within field
					wrapper := ls.newLexicalScope(nil, nil, nil)
					wrapper.appendLexicalBinding(alias.Ident.Name, binding)
					binding.parent = wrapper
				case ast.Label:
					// X=ident: field
					// X="basic": field
					// X="\(e)": field
					// X=(e): field
					// X is visible within s
					ls.appendLexicalBinding(alias.Ident.Name, binding)
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
					wrapper := ls.newLexicalScope(nil, nil, nil)
					wrapper.newLexicalBinding(alias.Ident.Name, alias.Ident, alias.Expr)
					binding.parent = wrapper
				} else {
					resolvable = append(resolvable, label.X)
				}
			case *ast.ListLit:
				for _, elt := range label.Elts {
					if alias, ok := elt.(*ast.Alias); ok {
						// [X=e]: field
						// X is only visible within field.
						wrapper := ls.newLexicalScope(nil, nil, nil)
						wrapper.newLexicalBinding(alias.Ident.Name, alias.Ident, alias.Expr)
						binding.parent = wrapper
					} else {
						resolvable = append(resolvable, elt)
					}
				}
			}
		}
	}

	for _, expr := range embeddedResolvable {
		scopes := ls.resolve(expr)
		ls.resolvesTo = append(ls.resolvesTo, scopes...)
	}
	for _, expr := range resolvable {
		ls.resolve(expr)
	}
}

// resolve resolves the given expression into a lexicalScope slice.
func (ls *lexicalScope) resolve(e ast.Expr) []*lexicalScope {
	switch e := e.(type) {
	case *ast.Ident:
		scopes := ls.resolvePathRoot(e.Name)
		ls.dfns.addResolution(e.NamePos, len(e.Name), scopes)
		return scopes

	case *ast.SelectorExpr:
		resolved := ls.resolve(e.X)
		if len(resolved) == 0 {
			return nil
		}
		scopesSet := make(map[*lexicalScope]struct{})
		navigableScopesSet := make(map[*navigableScope]struct{})
		for len(resolved) > 0 {
			s := resolved[0]
			resolved = resolved[1:]
			if _, seen := scopesSet[s]; seen {
				continue
			}
			scopesSet[s] = struct{}{}
			navigableScopesSet[s.navigable] = struct{}{}
			s.eval()
			resolved = append(resolved, s.resolvesTo...)
		}
		name := ""
		switch l := e.Sel.(type) {
		case *ast.Ident:
			name = l.Name
		case *ast.BasicLit:
			name = l.Value
		default:
			return nil
		}

		var results []*lexicalScope
		for navigable := range navigableScopesSet {
			results = append(results, navigable.bindings[name]...)
		}
		ls.dfns.addResolution(e.Sel.Pos(), len(name), results)
		return results

	case *ast.IndexExpr:
		return append(ls.resolve(e.X), ls.resolve(e.Index)...)

	case *ast.StructLit, *ast.ListLit:
		return []*lexicalScope{ls.newLexicalScope(nil, e, nil)}

	case *ast.ParenExpr:
		return ls.resolve(e.X)

	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND, token.OR:
			return append(ls.resolve(e.X), ls.resolve(e.Y)...)
		}
	}

	return nil
}

// resolvePathRoot resolves only the first element of a path - an
// [ast.Ident]'s name. CUE restricts the first element of any path to
// be lexically defined. So here, we search for a match via the
// lexicalScope's own bindings, whereas for subsequent path elements,
// we search the navigable bindings (in the [lexicalScope.resolve]
// method).
func (ls *lexicalScope) resolvePathRoot(name string) []*lexicalScope {
	pkgScope := ls.dfns.pkgScope
	for ; ls != nil; ls = ls.parent {
		if bindings, found := ls.bindings[name]; found {
			if len(bindings) == 1 && bindings[0].name != "" {
				// name has been resolved to an alias. Switch to the real
				// name.
				return ls.navigable.bindings[bindings[0].name]
			} else {
				return bindings
			}
		}
		if ls.parent == pkgScope {
			// pkgScope is the parent of the fileScopes. If we've got
			// this far, we're allowed to inspect the (shared) navigable
			// bindings directly without having to go via our
			// bindings.
			return ls.navigable.bindings[name]
		}
	}
	return nil
}

// ensureNavigableBinding creates and returns a new [lexicalScope],
// locating and using the appropriate shared [navigableScope]. The new
// scope is also stored as a lexical binding.
func (ls *lexicalScope) ensureNavigableBinding(name string, key ast.Node, unprocessed ast.Node) *lexicalScope {
	// Search via our own shared navigable bindings. This is a
	// criticial step that ensures that we continue to correctly share
	// navigableScopes even as lexicalScopes diverge. For example:
	//
	//	a: x.y.z
	//	x: y: z: 3
	//	x: y: z: 4
	//
	// By searching the *shared* bindings, we ensure not only that the
	// two x lexicalScopes share a navigable scope, but so too do the
	// two y lexicalScopes, and the two z lexicalScopes. This ensures
	// that the z in the x.y.z path resolves to both the z: 3 and z: 4
	// definitions.

	bindings := ls.navigable.bindings
	if bindings == nil {
		bindings = make(map[string][]*lexicalScope)
		ls.navigable.bindings = bindings
	}

	var navigable *navigableScope
	scopes, found := bindings[name]
	if found {
		navigable = scopes[0].navigable
	}
	binding := ls.newLexicalScope(key, unprocessed, navigable)
	binding.name = name

	bindings[name] = append(scopes, binding)
	ls.appendLexicalBinding(name, binding)

	return binding
}

// newLexicalBinding creates and returns a new [lexicalScope], and
// stores it in the current scope only, under the given name.
func (ls *lexicalScope) newLexicalBinding(name string, key ast.Node, unprocessed ast.Node) *lexicalScope {
	binding := ls.newLexicalScope(key, unprocessed, nil)
	ls.appendLexicalBinding(name, binding)
	return binding
}

// appendLexicalBinding stores the binding under the given name within
// the current scope only.
func (ls *lexicalScope) appendLexicalBinding(name string, binding *lexicalScope) {
	if ls.bindings == nil {
		ls.bindings = make(map[string][]*lexicalScope)
	}
	ls.bindings[name] = append(ls.bindings[name], binding)
}
