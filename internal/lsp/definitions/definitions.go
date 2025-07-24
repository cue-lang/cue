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
// LSP for "jump-to-definition" functionality, amongst others.
//
// # Introduction
//
// In the text that follows, I use subscripts in order to make
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
// The implementation is a lazy, call-by-need evaluator. The only
// purpose of this evaluator is to calculate what each element of each
// path resolves to; there is no calculation of fixed-points, no
// subsumption, no unification. And the little that this evaluator
// does do is imprecise. For example, I do not test field names (even
// when known) against patterns. I do not compute the names of dynamic
// fields, even when it is trivial to do so statically.
//
// # Algorithm 1: simplified CUE
//
// In CUE, a path such as x.y.z is only legal if x is defined in the
// same lexical scope as the path x.y.z, or any ancestor lexical
// scope. There is one exception to this which is the package scope,
// which arguably doesn't exist lexically. I return to the package
// scope much later on.
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
// To explain this evaluator, I shall start with a simplified version
// of CUE which does not place this restriction on paths: i.e. the
// first (and possibly only) element of a path may resolve to a
// definition that does *not* exist in the same lexical scope (or
// ancestor of) as that path.
//
// In the evaluator, a "scope" is a collection of bindings,
// i.e. key-value pairs. The values are themselves "scopes".  A scope
// is created with one or more unprocessed [ast.Node] values, for
// example, an [ast.File], or an [ast.StructLit].
//
// When a scope is evaluated, I unpack each of these nodes. An
// [ast.StructLit] for example contains a number of [ast.Decl] nodes,
// which are themselves then processed. When an [ast.Field] is
// encountered, I ensure that within the current scope, a binding
// exists for the field's name, and add the field's value to binding's
// scope's unprocessed values. Note that evaluation of a scope is not
// recursive: I do not, in general, evaluate a scope's bindings.
//
// If the scope contains a path, this will correspond either to the
// value of a field (i.e. the scope is for something like x: y), or an
// embedding into a struct. I keep track of these embedded paths and
// once I have finished processing all the scope's nodes, I then
// resolve the embedded paths to scopes, and record that this scope
// itself resolves to these other scopes.
//
// The consequence is that the evaluation of a scope creates and fully
// populates (with their unprocessed nodes) all of its bindings before
// any resolution of paths occurs. Thus evaluation can be driven by
// demand: if a path is encountered that accesses one of the scope's
// bindings (or any binding of an ancestor scope), then it is
// guaranteed that binding contains its complete set of nodes before
// it is accessed, and so it is safe to evaluate.
//
// Consider this example:
//
//	x: y
//	y: {
//		a: 3
//		b: y.a
//	}
//
// Evaluating the outermost scope will create two bindings, one for x
// (containing just the path y), and one for y (containing the
// [ast.StructLit]). If the scope for y is evaluated, it will create
// its own bindings for a (containing the [ast.BasicLit] 3), and for b
// (containing the path y.a).
//
// Imagine we want to evaluate, in the outermost scope, the path
// x.a. We first evaluate the outermost scope, then inspect its
// bindings. We find an x in there, so we grab that scope. This
// completes resolving the x of x.a. We now wish to find an a within
// this scope, so we evaluate this scope. This scope contains only the
// path y and so we have to resolve y and record within the scope what
// it has resolved to.
//
// Every scope knows its own parent scope. This scope containing y
// will inspect its own bindings for y, and find nothing. It asks its
// ancestors whether they know of a binding for y. The parent scope
// does have a binding for y, so it grabs that scope. This completes
// the resolution of y, and thus the evaluation of the scope that
// contains y. We now ask this scope whether it contains a binding for
// a. It doesn't, but we also inspect all the scopes that this scope
// resolves to. There is one resolved-to scope, and it does contain a
// binding for a, so we grab that. This completes the resolution of
// x.a.
//
// In summary: this algorithm uses the fact that I can traverse the
// AST breadth first and incrementally, to lazily merge together
// bindings that share the same path.
//
// What I haven't mentioned is that there are various [ast.Node] types
// that can use paths but not declare their own bindings, for example
// an interpolated string. When I encounter them, I accumulate and
// process them in the same way as embedded paths, it's just I don't
// record them within the scope's resolves-to set.
//
// # Querying
//
// In the previous section, I walked through the example of attempting
// to resolve the path x.a in the outermost scope. But this isn't what
// an LSP client will ask. An LSP client doesn't know what path the
// cursor is on, nor anything about the current scope. The LSP client
// knows only the cursor's line and column number.
//
// To facilitate an API that allows querying by file-coordinates, I
// extend each scope with a rangeset. For each [ast.Node] that a scope
// processes, it adds to its rangeset the range from the node's start
// file-offset to its end file-offset. Then, when asked to resolve
// whatever lies at some file-coordinate, I evaluate only the scopes
// that contain the file-coordinate in question.
//
// It's important to keep in mind that a scope may contain nodes from
// multiple different lexical scopes, across all files in the same
// package. I evaluate and traverse into a scope if *any* of its
// node's ranges contain the file-coordinate in question. Thus I am
// not solely interpreting nodes that are lexical ancestors of the
// file-coordinate. Instead, I am interpreting all nodes that
// contribute to every scope that contains any node that is a lexical
// ancestor of the file-coordinate. This ensures the scope is fully
// evaluated before it is queried.
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
// Here, l₃ resolves to l₁, or b. So I need to implement the rule that
// the first element of any path can only be resolved lexically. This
// means that I have "lexical bindings" which are candidates for
// resolving the first element of a path, and then "navigable
// bindings" which are candidates for resolving the rest of the path
// (as you navigate the path...). The lexical bindings do not have the
// "merging" behaviour of algorithm 1, for example:
//
//	x₁: y₁: 6
//	x₂: y₂: 7
//
// Here, the outermost scope has two lexical bindings for x, each
// having a distinct scope. But both of those scopes share a
// "navigable scope" struct and so any children that either of these
// bindings have, can be merged together appropriately via their
// shared "navigable scope". Thus in this example, the outermost scope
// has two lexical bindings for x; their distinct scopes have one
// binding each for their respective y fields; the scopes for the two
// x bindings share a "navigable scope", and their individual bindings
// for y both appear grouped together within that shared "navigable
// scope".
//
// This means that when resolving the first element of a path I can
// walk up the lexical bindings only, and then once that's resolved,
// switch to the navigable bindings for the rest of the path.
//
// The rangeset moves to the "navigable scope", thus it still collects
// together the ranges of every node that contributes to the navigable
// scope. This is essential to ensure that all lexical scopes that
// share a navigable scope are fully evaluated before the navigable
// scope is queried.
//
// For aliases, comprehensions and one or two other things, I have the
// ability to create a lexically-scoped binding without also creating
// a navigable binding. A navigable binding is always also a lexical
// binding, but a lexical binding need not be a navigable binding.
//
// # File and Package scopes
//
// CUE states that fields declared at the top level of a file are not
// in the file's scope, but are in fact in the package's scope. I
// ensure that at construction, the file scopes all share a "navigable
// scope". Thus if two different files in the same package both
// declare the same field, they will be correctly grouped within the
// navigable scope.
//
// When a file scope processes an [ast.File], lexical and navigable
// bindings will be created as normal within the file scope. When
// resolving the first element of a path, it's possible I walk up
// through the ancestor lexical scopes, and fail to find any matching
// lexical binding. If I get as far as the file scope and there's
// still no matching lexical binding, then it is safe to directly
// inspect the file scope's navigable bindings, which amount to the
// package's lexical bindings. In this way, a path's first element can
// be an ident that is only declared in some separate file within the
// same package, and yet it can still be resolved.
package definitions

import (
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/rangeset"
)

type Definitions struct {
	pkgScope   *scope
	byFilename map[string]*FileDefinitions
}

func Analyse(files ...*ast.File) *Definitions {
	dfns := &Definitions{
		byFilename: make(map[string]*FileDefinitions, len(files)),
	}

	pkgScope := dfns.newScope(nil, nil, nil, nil)
	dfns.pkgScope = pkgScope
	navigable := &navigableScope{}

	for _, file := range files {
		pkgScope.newScope(nil, file, navigable)
		dfns.byFilename[file.Filename] = &FileDefinitions{
			pkgScope:    pkgScope,
			resolutions: make([][]ast.Node, file.End().Offset()),
			File:        file.Pos().File(),
		}
	}

	return dfns
}

func (dfns *Definitions) newScope(parent *scope, key ast.Node, unprocessed ast.Node, navigable *navigableScope) *scope {
	if navigable == nil {
		navigable = &navigableScope{}
	}
	s := &scope{
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

func (dfns *Definitions) addResolution(start token.Pos, length int, targets []*scope) {
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

func (dfns *Definitions) ForFile(filename string) *FileDefinitions {
	return dfns.byFilename[filename]
}

type FileDefinitions struct {
	pkgScope    *scope
	resolutions [][]ast.Node
	File        *token.File
}

func (fdfns *FileDefinitions) ForOffset(offset int) []ast.Node {
	if offset < 0 || offset >= len(fdfns.resolutions) {
		return nil
	}
	nodes := fdfns.resolutions[offset]
	if nodes != nil {
		return nodes
	}
	fdfns.resolutions[offset] = []ast.Node{}

	filename := fdfns.File.Name()
	pkgScope := fdfns.pkgScope
	pkgScope.eval()
	seen := make(map[*scope]struct{})
	worklist := []*scope{pkgScope}
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

type scope struct {
	dfns   *Definitions
	parent *scope
	// unprocessed holds the nodes that make up this scope. Once a call
	// to [scope.eval] has returned, unprocessed must never be altered.
	unprocessed []ast.Node
	// keyPositions holds the positions that are considered to define
	// this scope. For example, if a scope represents `a: {}` then
	// keyPositions will hold the location of the `a`. Due to implicit
	// unification, keyPositions may contain several positions.
	key  ast.Node
	name string
	// resolvesTo points to the scopes reachable from nodes which are
	// embedded within this scope.
	resolvesTo      []*scope
	allScopes       []*scope
	lexicalBindings map[string][]*scope

	// shared
	navigable *navigableScope
}

func (s *scope) newScope(key ast.Node, unprocessed ast.Node, navigable *navigableScope) *scope {
	r := s.dfns.newScope(s, key, unprocessed, navigable)
	s.allScopes = append(s.allScopes, r)
	return r
}

func (s *scope) dump(depth int) {
	fmt.Printf("%*sScope %p (name: %q)\n", depth*3, "", s, s.name)
	navigable := s.navigable
	fmt.Printf("%*s Ranges %v\n", depth*3, "", navigable.ranges)

	if len(navigable.bindings) > 0 {
		fmt.Printf("%*s Navigable: %p\n", depth*3, "", s.navigable)
		for name, bindings := range navigable.bindings {
			fmt.Printf("%*s  %s:\n", depth*3, "", name)
			for _, binding := range bindings {
				binding.dump(depth + 1)
			}
		}
	}

	if len(s.lexicalBindings) > 0 {
		fmt.Printf("%*s Lexical:\n", depth*3, "")
		for name, bindings := range s.lexicalBindings {
			fmt.Printf("%*s  %s:\n", depth*3, "", name)
			for _, binding := range bindings {
				binding.dump(depth + 1)
			}
		}
	}

	if len(s.allScopes) > 0 {
		fmt.Printf("%*s All scopes:\n", depth*3, "")
		for _, r := range s.allScopes {
			r.dump(depth + 1)
		}
	}
}

type navigableScope struct {
	bindings map[string][]*scope
	ranges   *rangeset.FilenameRangeSet
}

func (s *scope) addRange(n ast.Node) {
	start := n.Pos().Position()
	end := n.End().Position()

	rs := s.navigable.ranges
	if rs == nil {
		rs = rangeset.NewFilenameRangeSet()
		s.navigable.ranges = rs
	}

	rs.Add(start.Filename, start.Offset, end.Offset)
}

func (s *scope) contains(filename string, offset int) bool {
	ranges := s.navigable.ranges
	return s == s.dfns.pkgScope || (ranges != nil && ranges.Contains(filename, offset))
}

func (s *scope) eval() {
	if s.unprocessed == nil {
		return
	}

	unprocessed := s.unprocessed
	s.unprocessed = nil

	var embeddedResolvable, resolvable []ast.Expr

	for len(unprocessed) > 0 {
		n := unprocessed[0]
		unprocessed = unprocessed[1:]

		s.addRange(n)

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
					s.ensureLexicalBinding(ip.Qualifier, n, nil)
				}
			} else {
				s.ensureLexicalBinding(n.Name.Name, n, nil)
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
				lhs := s.newScope(nil, n.X, nil)
				rhs := s.newScope(nil, n.Y, nil)
				s.resolvesTo = append(s.resolvesTo, lhs, rhs)
			default:
				resolvable = append(resolvable, n.X, n.Y)
			}

		case *ast.Alias:
			// X=e (the old deprecated alias syntax)
			s.ensureLexicalBinding(n.Ident.Name, n.Ident, n.Expr)

		case *ast.Ellipsis:
			unprocessed = append(unprocessed, n.Type)

		case *ast.CallExpr:
			resolvable = append(resolvable, n.Args...)

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, n.(ast.Expr))

		case *ast.Comprehension:
			parent := s
			for _, clause := range n.Clauses {
				cur := parent.newScope(nil, clause, nil)
				// We need to make sure that the comprehension value
				// (i.e. body) and all subsequent clauses, can be reached
				// by traversing through all clauses. The simplest way to
				// do this is just to include the whole range of n within
				// each descendent.
				cur.addRange(n)
				cur.eval()
				parent = cur
			}
			if parent != s {
				child := parent.newScope(nil, n.Value, nil)
				s.resolvesTo = append(s.resolvesTo, child)
			}

		case *ast.IfClause:
			unprocessed = append(unprocessed, n.Condition)

		case *ast.LetClause:
			s.ensureLexicalBinding(n.Ident.Name, n.Ident, n.Expr)

		case *ast.ForClause:
			if n.Key != nil {
				s.ensureLexicalBinding(n.Key.Name, n.Key, nil)
			}
			if n.Value != nil {
				s.ensureLexicalBinding(n.Value.Name, n.Value, nil)
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

			var binding *scope
			switch label := label.(type) {
			case *ast.Ident:
				binding = s.ensureNavigableBinding(label.Name, label, n.Value)
			case *ast.BasicLit:
				binding = s.ensureNavigableBinding(label.Value, label, n.Value)
			default:
				binding = s.newScope(label, n.Value, nil)
			}

			if isAlias {
				switch alias.Expr.(type) {
				case *ast.ListLit:
					// X=[e]: field
					// X is only visible within field
					wrapper := s.newScope(nil, nil, nil)
					wrapper.appendLexicalBinding(alias.Ident.Name, binding)
					binding.parent = wrapper
				case ast.Label:
					// X=ident: field
					// X="basic": field
					// X="\(e)": field
					// X=(e): field
					// X is visible within s
					s.appendLexicalBinding(alias.Ident.Name, binding)
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
					wrapper := s.newScope(nil, nil, nil)
					wrapper.ensureLexicalBinding(alias.Ident.Name, alias.Ident, alias.Expr)
					binding.parent = wrapper
				} else {
					resolvable = append(resolvable, label.X)
				}
			case *ast.ListLit:
				for _, elt := range label.Elts {
					if alias, ok := elt.(*ast.Alias); ok {
						// [X=e]: field
						// X is only visible within field.
						wrapper := s.newScope(nil, nil, nil)
						wrapper.ensureLexicalBinding(alias.Ident.Name, alias.Ident, alias.Expr)
						binding.parent = wrapper
					} else {
						resolvable = append(resolvable, elt)
					}
				}
			}
		}
	}

	for _, expr := range embeddedResolvable {
		scopes := s.resolve(expr)
		s.resolvesTo = append(s.resolvesTo, scopes...)
	}
	for _, expr := range resolvable {
		s.allScopes = append(s.allScopes, s.resolve(expr)...)
	}
}

func (s *scope) resolve(e ast.Expr) []*scope {
	switch e := e.(type) {
	case *ast.Ident:
		scopes := s.resolvePathRoot(e.Name)
		s.dfns.addResolution(e.NamePos, len(e.Name), scopes)
		return scopes

	case *ast.SelectorExpr:
		resolved := s.resolve(e.X)
		if len(resolved) == 0 {
			return nil
		}
		scopesSet := make(map[*scope]struct{})
		navigableScopesSet := make(map[*navigableScope]struct{})
		for len(resolved) > 0 {
			r := resolved[0]
			resolved = resolved[1:]
			if _, seen := scopesSet[r]; seen {
				continue
			}
			scopesSet[r] = struct{}{}
			navigableScopesSet[r.navigable] = struct{}{}
			r.eval()
			resolved = append(resolved, r.resolvesTo...)
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

		var results []*scope
		for navigable := range navigableScopesSet {
			results = append(results, navigable.bindings[name]...)
		}
		s.dfns.addResolution(e.Sel.Pos(), len(name), results)
		return results

	case *ast.IndexExpr:
		return append(s.resolve(e.X), s.resolve(e.Index)...)

	case *ast.StructLit, *ast.ListLit:
		return []*scope{s.newScope(nil, e, nil)}

	case *ast.ParenExpr:
		return s.resolve(e.X)

	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND, token.OR:
			return append(s.resolve(e.X), s.resolve(e.Y)...)
		}
	}

	return nil
}

func (s *scope) resolvePathRoot(name string) []*scope {
	pkgScope := s.dfns.pkgScope
	for ; s != nil; s = s.parent {
		if bindings, found := s.lexicalBindings[name]; found {
			if len(bindings) == 1 && bindings[0].name != "" {
				// name has been resolved to an alias. Switch to the real
				// name.
				return s.navigable.bindings[bindings[0].name]
			} else {
				return bindings
			}
		}
		if s.parent == pkgScope {
			// pkgScope is the parent of the fileScopes. If we've got
			// this far, we're allowed to inspect the (shared) navigable
			// bindings directly without having to go via our
			// lexicalBindings.
			return s.navigable.bindings[name]
		}
	}
	return nil
}

func (s *scope) ensureNavigableBinding(name string, key ast.Node, unprocessed ast.Node) *scope {
	navigableBindings := s.navigable.bindings
	if navigableBindings == nil {
		navigableBindings = make(map[string][]*scope)
		s.navigable.bindings = navigableBindings
	}

	var navigable *navigableScope
	bindings, found := navigableBindings[name]
	if found {
		navigable = bindings[0].navigable
	}
	binding := s.newScope(key, unprocessed, navigable)
	binding.name = name

	navigableBindings[name] = append(bindings, binding)
	s.appendLexicalBinding(name, binding)

	return binding
}

func (s *scope) ensureLexicalBinding(name string, key ast.Node, unprocessed ast.Node) *scope {
	binding := s.newScope(key, unprocessed, nil)
	s.appendLexicalBinding(name, binding)
	return binding
}

func (s *scope) appendLexicalBinding(name string, binding *scope) {
	if s.lexicalBindings == nil {
		s.lexicalBindings = make(map[string][]*scope)
	}
	s.lexicalBindings[name] = append(s.lexicalBindings[name], binding)
}
