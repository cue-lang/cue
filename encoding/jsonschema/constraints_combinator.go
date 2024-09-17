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

// Constraint combinators.

func constraintAllOf(key string, n cue.Value, s *state) {
	var knownTypes cue.Kind
	items := s.listItems("allOf", n, false)
	if len(items) == 0 {
		s.errf(n, "allOf requires at least one subschema")
		return
	}
	a := make([]ast.Expr, 0, len(items))
	for _, v := range items {
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
		s.all.add(n, ast.NewCall(
			ast.NewIdent("matchN"),
			// TODO it would be nice to be able to use a special sentinel "all" value
			// here rather than redundantly encoding the length of the list.
			&ast.BasicLit{
				Kind:  token.INT,
				Value: strconv.Itoa(len(items)),
			},
			ast.NewList(a...),
		))
	}
}

func constraintAnyOf(key string, n cue.Value, s *state) {
	var types cue.Kind
	var knownTypes cue.Kind
	items := s.listItems("anyOf", n, false)
	if len(items) == 0 {
		s.errf(n, "anyOf requires at least one subschema")
		return
	}
	a := make([]ast.Expr, 0, len(items))
	for _, v := range items {
		x, sub := s.schemaState(v, s.allowedTypes, nil, true)
		if sub.allowedTypes == 0 {
			// Nothing is allowed; omit.
			continue
		}
		types |= sub.allowedTypes
		knownTypes |= sub.knownTypes
		a = append(a, x)
	}
	if len(a) == 0 {
		// Nothing at all is allowed.
		s.allowedTypes = 0
		return
	}
	if len(a) == 1 {
		s.all.add(n, a[0])
		return
	}
	s.allowedTypes &= types
	s.knownTypes &= knownTypes
	s.all.add(n, ast.NewCall(
		ast.NewIdent("matchN"),
		&ast.UnaryExpr{
			Op: token.GEQ,
			X: &ast.BasicLit{
				Kind:  token.INT,
				Value: "1",
			},
		},
		ast.NewList(a...),
	))
}

func constraintOneOf(key string, n cue.Value, s *state) {
	var types cue.Kind
	var knownTypes cue.Kind
	hasSome := false
	items := s.listItems("oneOf", n, false)
	if len(items) == 0 {
		s.errf(n, "oneOf requires at least one subschema")
		return
	}
	a := make([]ast.Expr, 0, len(items))
	for _, v := range items {
		x, sub := s.schemaState(v, s.allowedTypes, nil, true)
		if sub.allowedTypes == 0 {
			// Nothing is allowed; omit
			continue
		}
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
		s.all.add(n, ast.NewCall(
			ast.NewIdent("matchN"),
			&ast.BasicLit{
				Kind:  token.INT,
				Value: "1",
			},
			ast.NewList(a...),
		))
	}

	// TODO: oneOf({a:x}, {b:y}, ..., not(anyOf({a:x}, {b:y}, ...))),
	// can be translated to {} | {a:x}, {b:y}, ...
}

func constraintNot(key string, n cue.Value, s *state) {
	subSchema := s.schema(n)
	s.all.add(n, ast.NewCall(
		ast.NewIdent("matchN"),
		&ast.BasicLit{
			Kind:  token.INT,
			Value: "0",
		},
		ast.NewList(subSchema),
	))
}

func constraintIf(key string, n cue.Value, s *state) {
	s.ifConstraint = s.schema(n)
}

func constraintThen(key string, n cue.Value, s *state) {
	s.thenConstraint = s.schema(n)
}

func constraintElse(key string, n cue.Value, s *state) {
	s.elseConstraint = s.schema(n)
}
