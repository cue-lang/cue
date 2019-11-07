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

// This file implements scopes and the objects they contain.

package astutil

import (
	"bytes"
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// An ErrFunc processes errors.
type ErrFunc func(pos token.Pos, msg string, args ...interface{})

// Resolve resolves all identifiers in a file. Unresolved identifiers are
// recorded in Unresolved. It will not overwrite already resolved values.
func Resolve(f *ast.File, errFn ErrFunc) {
	walk(&scope{errFn: errFn}, f)
}

// Resolve resolves all identifiers in an expression.
// It will not overwrite already resolved values.
func ResolveExpr(e ast.Expr, errFn ErrFunc) {
	f := &ast.File{}
	walk(&scope{file: f, errFn: errFn}, e)
}

// A Scope maintains the set of named language entities declared
// in the scope and a link to the immediately surrounding (outer)
// scope.
//
type scope struct {
	file  *ast.File
	outer *scope
	node  ast.Node
	index map[string]ast.Node

	errFn func(p token.Pos, msg string, args ...interface{})
}

func newScope(f *ast.File, outer *scope, node ast.Node, decls []ast.Decl) *scope {
	const n = 4 // initial scope capacity
	s := &scope{
		file:  f,
		outer: outer,
		node:  node,
		index: make(map[string]ast.Node, n),
		errFn: outer.errFn,
	}
	for _, d := range decls {
		switch x := d.(type) {
		case *ast.Field:
			label := x.Label

			if a, ok := x.Label.(*ast.Alias); ok {
				if name, _, _ := ast.LabelName(a.Ident); name != "" {
					s.insert(name, x)
				}
				label, _ = a.Expr.(ast.Label)
			}

			switch y := label.(type) {
			// TODO: support *ast.ParenExpr?
			case *ast.ListLit:
				// In this case, it really should be scoped like a template.
				if len(y.Elts) != 1 {
					break
				}
				if a, ok := y.Elts[0].(*ast.Alias); ok {
					s.insert(a.Ident.Name, x)
				}
			}

			// default:
			name, isIdent, _ := ast.LabelName(label)
			if isIdent {
				s.insert(name, x.Value)
			}
		case *ast.Alias:
			name, isIdent, _ := ast.LabelName(x.Ident)
			if isIdent {
				s.insert(name, x)
			}
			// Handle imports
		}
	}
	return s
}

func (s *scope) isAlias(n ast.Node) bool {
	if _, ok := s.node.(*ast.Field); ok {
		return true
	}
	switch n.(type) {
	case *ast.Alias:
		return true

	case *ast.Field:
		return true
	}
	return false
}

func (s *scope) insert(name string, n ast.Node) {
	if name == "" {
		return
	}
	// TODO: record both positions.
	if outer, _, existing := s.lookup(name); existing != nil {
		isAlias1 := s.isAlias(n)
		isAlias2 := outer.isAlias(existing)
		if isAlias1 != isAlias2 {
			s.errFn(n.Pos(), "cannot have both alias and field with name %q in same scope", name)
			return
		} else if isAlias1 || isAlias2 {
			if outer == s {
				s.errFn(n.Pos(), "alias %q redeclared in same scope", name)
				return
			}
			// TODO: Should we disallow shadowing of aliases?
			// This was the case, but it complicates the transition to
			// square brackets. The spec says allow it.
			// s.errFn(n.Pos(), "alias %q already declared in enclosing scope", name)
		}
	}
	s.index[name] = n
}

func (s *scope) resolveScope(name string, node ast.Node) (scope ast.Node, ok bool) {
	last := s
	for s != nil {
		if n, ok := s.index[name]; ok && node == n {
			if last.node == n {
				return nil, true
			}
			return s.node, true
		}
		s, last = s.outer, s
	}
	return nil, false
}

func (s *scope) lookup(name string) (p *scope, obj, node ast.Node) {
	// TODO(#152): consider returning nil for obj if it is a reference to root.
	// last := s
	for s != nil {
		if n, ok := s.index[name]; ok {
			// if last.node == n {
			// 	return nil, n
			// }
			return s, s.node, n
		}
		// s, last = s.outer, s
		s = s.outer
	}
	return nil, nil, nil
}

func (s *scope) After(n ast.Node) {}
func (s *scope) Before(n ast.Node) (w visitor) {
	switch x := n.(type) {
	case *ast.File:
		s := newScope(x, s, x, x.Decls)
		// Support imports.
		for _, d := range x.Decls {
			walk(s, d)
		}
		return nil

	case *ast.StructLit:
		return newScope(s.file, s, x, x.Elts)

	case *ast.Comprehension:
		s = scopeClauses(s, x.Clauses)

	case *ast.ListComprehension:
		s = scopeClauses(s, x.Clauses)

	case *ast.Field:
		var n ast.Node = x.Label
		alias, ok := x.Label.(*ast.Alias)
		if ok {
			n = alias.Expr
		}

		switch label := n.(type) {
		case *ast.Interpolation:
			walk(s, label)

		case *ast.ListLit:
			if len(label.Elts) != 1 {
				break
			}
			s := newScope(s.file, s, x, nil)
			if alias != nil {
				if name, _, _ := ast.LabelName(alias.Ident); name != "" {
					s.insert(name, x)
				}
			}

			expr := label.Elts[0]

			if a, ok := expr.(*ast.Alias); ok {
				expr = a.Expr

				// Add to current scope, instead of the value's, and allow
				// references to bind to these illegally.
				// We need this kind of administration anyway to detect
				// illegal name clashes, and it allows giving better error
				// messages. This puts the burdon on clients of this library
				// to detect illegal usage, though.
				name, err := ast.ParseIdent(a.Ident)
				if err == nil {
					s.insert(name, a.Expr)
				}
			}
			walk(s, expr)
			walk(s, x.Value)
			return nil

		case *ast.TemplateLabel:
			s := newScope(s.file, s, x, nil)
			name, err := ast.ParseIdent(label.Ident)
			if err == nil {
				s.insert(name, x.Label) // Field used for entire lambda.
			}
			walk(s, x.Value)
			return nil
		}
		if x.Value != nil {
			walk(s, x.Value)
		}
		return nil

	case *ast.Alias:
		// Disallow referring to the current LHS name.
		name := x.Ident.Name
		saved := s.index[name]
		delete(s.index, name) // The same name may still appear in another scope

		if x.Expr != nil {
			walk(s, x.Expr)
		}
		s.index[name] = saved
		return nil

	case *ast.ImportSpec:
		return nil

	case *ast.SelectorExpr:
		walk(s, x.X)
		return nil

	case *ast.Ident:
		name, ok, _ := ast.LabelName(x)
		if !ok {
			// TODO: generate error
			break
		}
		if _, obj, node := s.lookup(name); node != nil {
			switch {
			case x.Node == nil:
				x.Node = node
				x.Scope = obj

			case x.Node == node:
				x.Scope = obj

			default: // x.Node != node
				scope, ok := s.resolveScope(name, x.Node)
				if !ok {
					s.file.Unresolved = append(s.file.Unresolved, x)
				}
				x.Scope = scope
			}
		} else {
			s.file.Unresolved = append(s.file.Unresolved, x)
		}
		return nil
	}
	return s
}

func scopeClauses(s *scope, clauses []ast.Clause) *scope {
	for _, c := range clauses {
		if f, ok := c.(*ast.ForClause); ok { // TODO(let): support let clause
			walk(s, f.Source)
			s = newScope(s.file, s, f, nil)
			if f.Key != nil {
				name, err := ast.ParseIdent(f.Key)
				if err == nil {
					s.insert(name, f.Key)
				}
			}
			name, err := ast.ParseIdent(f.Value)
			if err == nil {
				s.insert(name, f.Value)
			}
		} else {
			walk(s, c)
		}
	}
	return s
}

// Debugging support
func (s *scope) String() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "scope %p {", s)
	if s != nil && len(s.index) > 0 {
		fmt.Fprintln(&buf)
		for name := range s.index {
			fmt.Fprintf(&buf, "\t%v\n", name)
		}
	}
	fmt.Fprintf(&buf, "}\n")
	return buf.String()
}
