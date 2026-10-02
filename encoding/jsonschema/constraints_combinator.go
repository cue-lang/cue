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
		x, sub := s.schemaState(v, s.allowedTypes, nil)
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
		// Unify the subschemas so that their fields and docs stay reachable,
		// unless the object is closed: unifying would then declare fields only
		// to disallow them, which the generator cannot tell from allowed ones.
		if len(a) == 1 || !s.schemaRoot().closesRoot(s.pos, make(map[string]bool), s.preserveUnknownFields) {
			for _, x := range a {
				s.all.add(n, x)
			}
			return
		}
		s.all.add(n, matchN(
			// TODO it would be nice to be able to use a special sentinel "all" value
			// here rather than redundantly encoding the length of the list.
			&ast.BasicLit{
				Kind:  token.INT,
				Value: strconv.Itoa(len(a)),
			},
			ast.NewList(a...),
		))
	}
}

// closesRoot reports whether the schema n disallows or constrains the
// properties it does not declare, following "$ref" and the subschemas
// unified with it, which inherit preserve like [state.preserveUnknownFields].
func (s *state) closesRoot(n cue.Value, seen map[string]bool, preserve bool) bool {
	path := n.Path().String()
	if n.Kind() != cue.StructKind || seen[path] {
		return false
	}
	// The first schema holds the "allOf", and its "unevaluatedProperties"
	// accounts for the properties of the subschemas.
	holder := len(seen) == 0
	seen[path] = true
	lookup := func(key string) cue.Value {
		return n.LookupPath(cue.MakePath(cue.Str(key)))
	}
	isTrueBool := func(v cue.Value) bool {
		ok, _ := v.Bool()
		return ok
	}
	additional := lookup("additionalProperties")
	if additional.Exists() && !isTrueBool(additional) {
		return true
	}
	if v := lookup("unevaluatedProperties"); !holder && v.Exists() && !isTrueBool(v) {
		return true
	}
	preserve = preserve || isTrueBool(lookup("x-kubernetes-preserve-unknown-fields"))
	if s.cfg.OpenOnlyWhenExplicit && !preserve && !additional.Exists() {
		// The schema does not open the object which it gives, if any.
		if typ := lookup("type"); typ.Exists() {
			if typeKinds(typ)&cue.StructKind != 0 {
				return true
			}
		} else if lookup("properties").Exists() || lookup("patternProperties").Exists() || lookup("required").Exists() {
			return true
		}
	}
	if target, ok := s.localRefTarget(lookup("$ref")); ok && s.closesRoot(target, seen, false) {
		return true
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		subs := lookup(key)
		if count, _ := subs.Len().Int64(); key != "allOf" && count > 1 {
			continue // Multiple alternatives only validate the instance.
		}
		for i, _ := subs.List(); i.Next(); {
			if s.closesRoot(i.Value(), seen, preserve) {
				return true
			}
		}
	}
	return false
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
		x, sub := s.schemaState(v, s.allowedTypes, nil)
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
	s.all.add(n, matchN(
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
		x, sub := s.schemaState(v, s.allowedTypes, nil)
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
		s.all.add(n, matchN(
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
	s.all.add(n, matchN(
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
	ifExpr, ifSub := s.schemaState(s.ifConstraint, s.allowedTypes, nil)
	if hasThen {
		// The allowed types of the "then" constraint are constrained both
		// by the current constraints and the "if" constraint.
		thenExpr, _ = s.schemaState(s.thenConstraint, s.allowedTypes&ifSub.allowedTypes, nil)
	}
	if hasElse {
		elseExpr, _ = s.schemaState(s.elseConstraint, s.allowedTypes, nil)
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

// matchN creates a matchN call expression and makes sure the first
// argument (which is often a lexcially-short constraint) is kept on
// the same line as the matchN token itself.
func matchN(args ...ast.Expr) *ast.CallExpr {
	if len(args) > 0 {
		ast.SetRelPos(args[0], token.NoSpace)
	}
	return ast.NewCall(ast.NewIdent("matchN"), args...)
}
