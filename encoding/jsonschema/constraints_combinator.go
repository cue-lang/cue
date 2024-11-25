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
		x, sub := s.schemaState(v, s.allowedTypes)
		s.allowedTypes &= sub.allowedTypes
		if sub.hasConstraints {
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
		if len(a) == 1 {
			// Only one possibility. Use that.
			s.all.add(n, a[0])
			return
		}
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
		x, sub := s.schemaState(v, s.allowedTypes)
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
	needsConstraint := false
	items := s.listItems("oneOf", n, false)
	if len(items) == 0 {
		s.errf(n, "oneOf requires at least one subschema")
		return
	}
	a := make([]ast.Expr, 0, len(items))
	for _, v := range items {
		x, sub := s.schemaState(v, s.allowedTypes)
		if sub.allowedTypes == 0 {
			// Nothing is allowed; omit
			continue
		}

		// TODO: make more finegrained by making it two pass.
		if sub.hasConstraints {
			needsConstraint = true
		} else if (types & sub.allowedTypes) != 0 {
			// If there's overlap between the unconstrained elements,
			// we'll definitely need to add a constraint.
			needsConstraint = true
		}
		types |= sub.allowedTypes
		knownTypes |= sub.knownTypes
		a = append(a, x)
	}
	// TODO if there are no elements in the oneOf, validation
	// should fail.
	s.allowedTypes &= types
	if len(a) > 0 && needsConstraint {
		s.knownTypes &= knownTypes
		if len(a) == 1 {
			// Only one possibility. Use that.
			s.all.add(n, a[0])
			return
		}
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
	s.ifConstraint = n
}

func constraintThen(key string, n cue.Value, s *state) {
	s.thenConstraint = n
}

func constraintElse(key string, n cue.Value, s *state) {
	s.elseConstraint = n
}

// constraintIfThenElse is not implemented as a standard constraint
// function because it needs to operate knowing about the presence
// of all of "if", "then" and "else".
func constraintIfThenElse(s *state) {
	hasIf, hasThen, hasElse := s.ifConstraint.Exists(), s.thenConstraint.Exists(), s.elseConstraint.Exists()
	if !hasIf || (!hasThen && !hasElse) {
		return
	}
	var ifExpr, thenExpr, elseExpr ast.Expr
	ifExpr, ifSub := s.schemaState(s.ifConstraint, s.allowedTypes)
	if hasThen {
		// The allowed types of the "then" constraint are constrained both
		// by the current constraints and the "if" constraint.
		thenExpr, _ = s.schemaState(s.thenConstraint, s.allowedTypes&ifSub.allowedTypes)
	}
	if hasElse {
		elseExpr, _ = s.schemaState(s.elseConstraint, s.allowedTypes)
	}
	if thenExpr == nil {
		thenExpr = top()
	}
	if elseExpr == nil {
		elseExpr = top()
	}
	s.all.add(s.pos, ast.NewCall(
		ast.NewIdent("matchIf"),
		ifExpr,
		thenExpr,
		elseExpr,
	))
}
