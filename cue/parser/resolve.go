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

package parser

import (
	"bytes"
	"fmt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// resolve resolves all identifiers in a file. Unresolved identifiers are
// recorded in Unresolved.
func resolve(f *ast.File, errFn func(pos token.Pos, msg string, args ...interface{})) {
	walk(&scope{errFn: errFn}, f)
}

func resolveExpr(e ast.Expr, errFn func(pos token.Pos, msg string, args ...interface{})) {
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
			if name, ok := ast.LabelName(x.Label); ok {
				s.insert(name, x.Value)
			}
		case *ast.Alias:
			name := x.Ident.Name
			s.insert(name, x)
			// Handle imports
		}
	}
	return s
}

func (s *scope) insert(name string, n ast.Node) {
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

func (s *scope) lookup(name string) (obj, node ast.Node) {
	last := s
	for s != nil {
		if n, ok := s.index[name]; ok {
			if last.node == n {
				return nil, n
			}
			return s.node, n
		}
		s, last = s.outer, s
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

	case *ast.ComprehensionDecl:
		s = scopeClauses(s, x.Clauses)

	case *ast.ListComprehension:
		s = scopeClauses(s, x.Clauses)

	case *ast.Field:
		switch label := x.Label.(type) {
		case *ast.Interpolation:
			walk(s, label)
		case *ast.TemplateLabel:
			s := newScope(s.file, s, x, nil)
			name, _ := ast.LabelName(label)
			s.insert(name, x.Label) // Field used for entire lambda.
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
		if obj, node := s.lookup(x.Name); node != nil {
			x.Node = node
			x.Scope = obj
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
				s.insert(f.Key.Name, f.Key)
			}
			s.insert(f.Value.Name, f.Value)
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
