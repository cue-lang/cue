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
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// Constraint combinators.

func constraintAllOf(key string, n cue.Value, s *state) {
	var a []ast.Expr
	var knownTypes cue.Kind
	for _, v := range s.listItems("allOf", n, false) {
		x, sub := s.schemaState(v, s.allowedTypes, nil, true)
		s.allowedTypes &= sub.allowedTypes
		if sub.hasConstraints() {
			// This might seem a little odd, since the actual
			// types are the intersection of the known types
			// of the allOf members. However, knownTypes
			// is really there to avoid adding redundant disjunctions.
			// So if we have (int & string) & (disjunction)
			// we definitely don't have to add int or string to
			// disjunction.
			knownTypes |= sub.knownTypes
			a = append(a, x)
		}
	}
	// TODO maybe give an error/warning if s.allowedTypes == 0
	// as that's a known-impossible assertion?
	if len(a) > 0 {
		s.knownTypes &= knownTypes
		s.all.add(n, ast.NewBinExpr(token.AND, a...))
	}
}

func constraintAnyOf(key string, n cue.Value, s *state) {
	var types cue.Kind
	var a []ast.Expr
	var knownTypes cue.Kind
	for _, v := range s.listItems("anyOf", n, false) {
		x, sub := s.schemaState(v, s.allowedTypes, nil, true)
		types |= sub.allowedTypes
		knownTypes |= sub.knownTypes
		a = append(a, x)
	}
	s.allowedTypes &= types
	if len(a) > 0 {
		s.knownTypes &= knownTypes
		s.all.add(n, ast.NewBinExpr(token.OR, a...))
	}
}

func constraintOneOf(key string, n cue.Value, s *state) {
	var types cue.Kind
	var knownTypes cue.Kind
	var a []ast.Expr
	hasSome := false
	for _, v := range s.listItems("oneOf", n, false) {
		x, sub := s.schemaState(v, s.allowedTypes, nil, true)
		types |= sub.allowedTypes

		// TODO: make more finegrained by making it two pass.
		if sub.hasConstraints() {
			hasSome = true
		}

		if !isAny(x) {
			knownTypes |= sub.knownTypes
			a = append(a, x)
		}
	}
	// TODO if there are no elements in the oneOf, validation
	// should fail.
	s.allowedTypes &= types
	if len(a) > 0 && hasSome {
		s.knownTypes &= knownTypes
		s.all.add(n, ast.NewBinExpr(token.OR, a...))
	}

	// TODO: oneOf({a:x}, {b:y}, ..., not(anyOf({a:x}, {b:y}, ...))),
	// can be translated to {} | {a:x}, {b:y}, ...
}
