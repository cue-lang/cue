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
				parent = cur
			}
			if parent != s {
				parent.newScope(nil, n.Value, nil)
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
