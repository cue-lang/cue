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
	"cuelang.org/go/internal"
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
			// TODO: switch to ast's implementation
			name, isIdent := internal.LabelName(x.Label)
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

func (s *scope) insert(name string, n ast.Node) {
	if name == "" {
		return
	}
	if _, existing := s.lookup(name); existing != nil {
		_, isAlias1 := n.(*ast.Alias)
		_, isAlias2 := existing.(*ast.Alias)
		if isAlias1 != isAlias2 {
			s.errFn(n.Pos(), "cannot have alias and non-alias with the same name")
			return
		} else if isAlias1 || isAlias2 {
			s.errFn(n.Pos(), "cannot have two aliases with the same name in the same scope")
			return
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

func (s *scope) lookup(name string) (obj, node ast.Node) {
	// TODO(#152): consider returning nil for obj if it is a reference to root.
	// last := s
	for s != nil {
		if n, ok := s.index[name]; ok {
			// if last.node == n {
			// 	return nil, n
			// }
			return s.node, n
		}
		// s, last = s.outer, s
		s = s.outer
	}
	return nil, nil
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
		switch label := x.Label.(type) {
		case *ast.Interpolation:
			walk(s, label)
		case *ast.TemplateLabel:
			s := newScope(s.file, s, x, nil)
			name, err := ast.ParseIdent(label.Ident)
			if err == nil {
				s.insert(name, x.Label) // Field used for entire lambda.
			}
			walk(s, x.Value)
			return nil
		}
		// Disallow referring to the current LHS name (this applies recursively)
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
			break
		}
		if obj, node := s.lookup(name); node != nil {
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
