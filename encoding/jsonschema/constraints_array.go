// Copyright 2019 CUE Authors
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

package jsonschema

import (
	"strconv"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// Array constraints

func constraintAdditionalItems(key string, n cue.Value, s *state) {
	switch n.Kind() {
	case cue.BoolKind:
		// TODO: support

	case cue.StructKind:
		if s.list != nil {
			elem := s.schema(n)
			s.list.Elts = append(s.list.Elts, &ast.Ellipsis{Type: elem})
		}

	default:
		s.errf(n, `value of "additionalItems" must be an object or boolean`)
	}
}

func constraintMinContains(key string, n cue.Value, s *state) {
	p, err := uint64Value(n)
	if err != nil {
		s.errf(n, `value of "minContains" must be a non-negative integer value`)
		return
	}
	s.minContains = &p
}

func constraintMaxContains(key string, n cue.Value, s *state) {
	p, err := uint64Value(n)
	if err != nil {
		s.errf(n, `value of "maxContains" must be a non-negative integer value`)
		return
	}
	s.maxContains = &p
}

func constraintContains(key string, n cue.Value, s *state) {
	list := s.addImport(n, "list")
	x := s.schema(n)

	var min uint64 = 1
	if s.minContains != nil {
		min = *s.minContains
	}
	var c ast.Expr = &ast.UnaryExpr{
		Op: token.GEQ,
		X:  ast.NewLit(token.INT, strconv.FormatUint(min, 10)),
	}

	if s.maxContains != nil {
		c = ast.NewBinExpr(token.AND, c, &ast.UnaryExpr{
			Op: token.LEQ,
			X:  ast.NewLit(token.INT, strconv.FormatUint(*s.maxContains, 10)),
		})
	}

	x = ast.NewCall(ast.NewSel(list, "MatchN"), c, clearPos(x))
	s.add(n, arrayType, x)
}

func constraintItems(key string, n cue.Value, s *state) {
	switch n.Kind() {
	case cue.StructKind:
		elem := s.schema(n)
		ast.SetRelPos(elem, token.NoRelPos)
		s.add(n, arrayType, ast.NewList(&ast.Ellipsis{Type: elem}))

	case cue.ListKind:
		var a []ast.Expr
		for _, n := range s.listItems("items", n, true) {
			v := s.schema(n) // TODO: label with number literal.
			ast.SetRelPos(v, token.NoRelPos)
			a = append(a, v)
		}
		s.list = ast.NewList(a...)
		s.add(n, arrayType, s.list)

	default:
		s.errf(n, `value of "items" must be an object or array`)
	}
}

func constraintMaxItems(key string, n cue.Value, s *state) {
	list := s.addImport(n, "list")
	x := ast.NewCall(ast.NewSel(list, "MaxItems"), clearPos(s.uint(n)))
	s.add(n, arrayType, x)
}

func constraintMinItems(key string, n cue.Value, s *state) {
	a := []ast.Expr{}
	p, err := uint64Value(n)
	if err != nil {
		s.errf(n, "invalid uint")
	}
	for ; p > 0; p-- {
		a = append(a, top())
	}
	s.add(n, arrayType, ast.NewList(append(a, &ast.Ellipsis{})...))

	// TODO: use this once constraint resolution is properly implemented.
	// list := s.addImport(n, "list")
	// s.addConjunct(n, ast.NewCall(ast.NewSel(list, "MinItems"), clearPos(s.uint(n))))
}

func constraintUniqueItems(key string, n cue.Value, s *state) {
	if s.boolValue(n) {
		list := s.addImport(n, "list")
		s.add(n, arrayType, ast.NewCall(ast.NewSel(list, "UniqueItems")))
	}
}

func clearPos(e ast.Expr) ast.Expr {
	ast.SetRelPos(e, token.NoRelPos)
	return e
}
