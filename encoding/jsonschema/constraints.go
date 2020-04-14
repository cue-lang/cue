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
	"math/big"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

type constraint struct {
	key string

	// phase indicates on which pass c constraint should be added. This ensures
	// that constraints are applied in the correct order. For instance, the
	// "required" constraint validates that a listed field is contained in
	// "properties". For this to work, "properties" must be processed before
	// "required" and thus must have a lower phase number than the latter.
	phase int

	// Indicates the draft number in which this constraint is defined.
	draft int
	fn    constraintFunc
}

// A constraintFunc converts a given JSON Schema constraint (specified in n)
// to a CUE constraint recorded in state.
type constraintFunc func(n cue.Value, s *state)

func p0(name string, f constraintFunc) *constraint {
	return &constraint{key: name, fn: f}
}

func p0d(name string, draft int, f constraintFunc) *constraint {
	return &constraint{key: name, draft: draft, fn: f}
}

func p1(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 1, fn: f}
}

func p2(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 2, fn: f}
}

// TODO:
// writeOnly, readOnly

var constraintMap = map[string]*constraint{}

func init() {
	for _, c := range constraints {
		constraintMap[c.key] = c
	}
}

func addDefinitions(n cue.Value, s *state) {
	if n.Kind() != cue.StructKind {
		s.errf(n, `"definitions" expected an object, found %v`, n.Kind)
	}

	if len(s.path) != 1 {
		s.errf(n, `"definitions" expected an object, found %v`, n.Kind)
	}

	s.processMap(n, func(key string, n cue.Value) {
		f := &ast.Field{
			Label: ast.NewString(s.path[len(s.path)-1]),
			Token: token.ISA,
			Value: s.schema(n),
		}
		f = &ast.Field{
			Label: ast.NewIdent(rootDefs),
			Value: ast.NewStruct(f),
		}
		ast.SetRelPos(f, token.NewSection)
		s.definitions = append(s.definitions, f)
	})
}

var constraints = []*constraint{
	// Meta data.

	p0("$schema", func(n cue.Value, s *state) {
		// Identifies this as a JSON schema and specifies its version.
		// TODO: extract version.
		s.jsonschema, _ = s.strValue(n)
	}),

	p0("$id", func(n cue.Value, s *state) {
		// URL: https://domain.com/schemas/foo.json
		// Use Title(foo) as CUE identifier.
		// anchors: #identifier
		//
		// TODO: mark identifiers.
		s.id, _ = s.strValue(n)
	}),

	// Generic constraint

	p0("type", func(n cue.Value, s *state) {
		var types cue.Kind
		set := func(n cue.Value) {
			str, ok := s.strValue(n)
			if !ok {
				s.errf(n, "type value should be a string")
			}
			switch str {
			case "null":
				types |= cue.NullKind
				// TODO: handle OpenAPI restrictions.
			case "boolean":
				types |= cue.BoolKind
			case "string":
				types |= cue.StringKind
			case "number":
				types |= cue.NumberKind
			case "integer":
				types |= cue.IntKind
			case "array":
				types |= cue.ListKind
			case "object":
				types |= cue.StructKind

			default:
				s.errf(n, "unknown type %q", n)
			}
		}

		switch n.Kind() {
		case cue.StringKind:
			set(n)
		case cue.ListKind:
			for i, _ := n.List(); i.Next(); {
				set(i.Value())
			}
		default:
			s.errf(n, `value of "type" must be a string or list of strings`)
		}

		s.allowedTypes &= types
	}),

	p0("enum", func(n cue.Value, s *state) {
		var a []ast.Expr
		for _, x := range s.listItems("enum", n, true) {
			a = append(a, s.value(x))
		}
		s.addConjunct(ast.NewBinExpr(token.OR, a...))
		s.typeOptional = true
	}),

	p0d("const", 6, func(n cue.Value, s *state) {
		s.addConjunct(s.value(n))
	}),

	p0("default", func(n cue.Value, s *state) {
		allowed, used := s.allowedTypes, s.usedTypes
		s.default_ = s.value(n)
		s.allowedTypes, s.usedTypes = allowed, used
		// must validate that the default is subsumed by the normal value,
		// as CUE will otherwise broaden the accepted values with the default.
		s.examples = append(s.examples, s.default_)
	}),

	p0("deprecated", func(n cue.Value, s *state) {
		if s.boolValue(n) {
			s.deprecated = true
		}
	}),

	p0("examples", func(n cue.Value, s *state) {
		if n.Kind() != cue.ListKind {
			s.errf(n, `value of "examples" must be an array, found %v`, n.Kind)
		}
		for _, n := range s.listItems("examples", n, true) {
			if ex := s.schema(n); !isAny(ex) {
				s.examples = append(s.examples, ex)
			}
		}
	}),

	p0("description", func(n cue.Value, s *state) {
		s.description, _ = s.strValue(n)
	}),

	p0("title", func(n cue.Value, s *state) {
		s.title, _ = s.strValue(n)
	}),

	p0d("$comment", 7, func(n cue.Value, s *state) {
	}),

	p0("$def", addDefinitions),
	p0("definitions", addDefinitions),
	p0("$ref", func(n cue.Value, s *state) {
		s.usedTypes = allTypes
		str, _ := s.strValue(n)
		a := s.parseRef(n.Pos(), str)
		if a != nil {
			a = s.mapRef(n.Pos(), str, a)
		}
		if a == nil {
			s.addConjunct(&ast.BadExpr{From: n.Pos()})
			return
		}
		s.addConjunct(ast.NewSel(ast.NewIdent(a[0]), a[1:]...))
	}),

	// Combinators

	// TODO: work this out in more detail: oneOf and anyOf below have the same
	// implementation in CUE. The distinction is that for anyOf a result is
	// allowed to be ambiguous at the end, whereas for oneOf a disjunction must
	// be fully resolved. There is currently no easy way to set this distinction
	// in CUE.
	//
	// One could correctly write oneOf like this once 'not' is implemented:
	//
	//   oneOf(a, b, c) :-
	//      anyOf(
	//         allOf(a, not(b), not(c)),
	//         allOf(not(a), b, not(c)),
	//         allOf(not(a), not(b), c),
	//   ))
	//
	// This is not necessary if the values are mutually exclusive/ have a
	// discriminator.

	p1("allOf", func(n cue.Value, s *state) {
		var a []ast.Expr
		for _, v := range s.listItems("allOf", n, false) {
			x, sub := s.schemaState(v, s.allowedTypes, true)
			s.allowedTypes &= sub.allowedTypes
			s.usedTypes |= sub.usedTypes
			if sub.hasConstraints() {
				a = append(a, x)
			}
		}
		if len(a) > 0 {
			s.conjuncts = append(s.conjuncts, ast.NewBinExpr(token.AND, a...))
		}
	}),

	p1("anyOf", func(n cue.Value, s *state) {
		var types cue.Kind
		var a []ast.Expr
		for _, v := range s.listItems("anyOf", n, false) {
			x, sub := s.schemaState(v, s.allowedTypes, true)
			types |= sub.allowedTypes
			if sub.hasConstraints() {
				a = append(a, x)
			}
		}
		s.allowedTypes &= types
		if len(a) > 0 {
			s.conjuncts = append(s.conjuncts, ast.NewBinExpr(token.OR, a...))
		}
	}),

	p1("oneOf", func(n cue.Value, s *state) {
		var types cue.Kind
		var a []ast.Expr
		hasSome := false
		for _, v := range s.listItems("oneOf", n, false) {
			x, sub := s.schemaState(v, s.allowedTypes, true)
			types |= sub.allowedTypes

			// TODO: make more finegrained by making it two pass.
			if sub.hasConstraints() {
				hasSome = true
			}

			if !isAny(x) {
				a = append(a, x)
			}
		}
		s.allowedTypes &= types
		if len(a) > 0 && hasSome {
			s.usedTypes = allTypes
			s.conjuncts = append(s.conjuncts, ast.NewBinExpr(token.OR, a...))
		}

		// TODO: oneOf({a:x}, {b:y}, ..., not(anyOf({a:x}, {b:y}, ...))),
		// can be translated to {} | {a:x}, {b:y}, ...
	}),

	// String constraints

	p0("pattern", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StringKind
		s.addConjunct(&ast.UnaryExpr{Op: token.MAT, X: s.string(n)})
	}),

	p0("minLength", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StringKind
		min := s.number(n)
		strings := s.addImport("strings")
		s.addConjunct(ast.NewCall(ast.NewSel(strings, "MinRunes"), min))

	}),

	p0("maxLength", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StringKind
		max := s.number(n)
		strings := s.addImport("strings")
		s.addConjunct(ast.NewCall(ast.NewSel(strings, "MaxRunes"), max))
	}),

	p0d("contentMediaType", 7, func(n cue.Value, s *state) {
		s.usedTypes |= cue.StringKind
	}),

	p0d("contentEncoding", 7, func(n cue.Value, s *state) {
		s.usedTypes |= cue.StringKind
		// 7bit, 8bit, binary, quoted-printable and base64.
		// RFC 2054, part 6.1.
		// https://tools.ietf.org/html/rfc2045
		// TODO: at least handle bytes.
	}),

	// Number constraints

	p0("minimum", func(n cue.Value, s *state) {
		s.usedTypes |= cue.NumberKind
		s.addConjunct(&ast.UnaryExpr{Op: token.GEQ, X: s.number(n)})
	}),

	p0("exclusiveMinimum", func(n cue.Value, s *state) {
		// TODO: should we support Draft 4 booleans?
		s.usedTypes |= cue.NumberKind
		s.addConjunct(&ast.UnaryExpr{Op: token.GTR, X: s.number(n)})
	}),

	p0("maximum", func(n cue.Value, s *state) {
		s.usedTypes |= cue.NumberKind
		s.addConjunct(&ast.UnaryExpr{Op: token.LEQ, X: s.number(n)})
	}),

	p0("exclusiveMaximum", func(n cue.Value, s *state) {
		// TODO: should we support Draft 4 booleans?
		s.usedTypes |= cue.NumberKind
		s.addConjunct(&ast.UnaryExpr{Op: token.LSS, X: s.number(n)})
	}),

	p0("multipleOf", func(n cue.Value, s *state) {
		s.usedTypes |= cue.NumberKind
		multiple := s.number(n)
		var x big.Int
		_, _ = n.MantExp(&x)
		if x.Cmp(big.NewInt(0)) != 1 {
			s.errf(n, `"multipleOf" value must be < 0; found %s`, n)
		}
		math := s.addImport("math")
		s.addConjunct(ast.NewCall(ast.NewSel(math, "MultipleOf"), multiple))
	}),

	// Object constraints

	p0("properties", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind

		if s.obj == nil {
			s.obj = &ast.StructLit{}
		}
		if n.Kind() != cue.StructKind {
			s.errf(n, `"properties" expected an object, found %v`, n.Kind)
		}

		s.processMap(n, func(key string, n cue.Value) {
			// property?: value
			expr, state := s.schemaState(n, allTypes, false)
			f := &ast.Field{Label: ast.NewString(key), Value: expr}
			state.doc(f)
			f.Optional = token.Blank.Pos()
			if len(s.obj.Elts) > 0 && len(f.Comments()) > 0 {
				// TODO: change formatter such that either a a NewSection on the
				// field or doc comment will cause a new section.
				ast.SetRelPos(f.Comments()[0], token.NewSection)
			}
			if state.deprecated {
				switch expr.(type) {
				case *ast.StructLit:
					s.obj.Elts = append(s.obj.Elts, addTag(key, "deprecated", ""))
				default:
					f.Attrs = append(f.Attrs, internal.NewAttr("deprecated", ""))
				}
			}
			s.obj.Elts = append(s.obj.Elts, f)
		})
	}),

	p1("required", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind
		if n.Kind() != cue.ListKind {
			s.errf(n, `value of "required" must be list of strings, found %v`, n.Kind)
			return
		}

		if s.obj == nil {
			s.obj = &ast.StructLit{}
			// TODO: detect that properties is defined somewhere.
			// s.errf(n, `"required" without a "properties" field`)
		}

		// Create field map
		fields := map[string]*ast.Field{}
		for _, d := range s.obj.Elts {
			f := d.(*ast.Field)
			str, _, err := ast.LabelName(f.Label)
			if err == nil {
				fields[str] = f
			}
		}

		for _, n := range s.listItems("required", n, true) {
			str, ok := s.strValue(n)
			f := fields[str]
			if f == nil && ok {
				f := &ast.Field{
					Label: ast.NewString(str),
					Value: ast.NewIdent("_"),
				}
				fields[str] = f
				s.obj.Elts = append(s.obj.Elts, f)
				continue
			}
			if f.Optional == token.NoPos {
				s.errf(n, "duplicate required field %q", str)
			}
			f.Optional = token.NoPos
		}
	}),

	p0d("propertyNames", 6, func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind

		// [=~pattern]: _
		if names, _ := s.schemaState(n, cue.StringKind, false); !isAny(names) {
			s.addConjunct(ast.NewStruct(ast.NewList((names)), ast.NewIdent("_")))
		}
	}),

	// TODO: reenable when we have proper non-monotonic contraint validation.
	// p0("minProperties", func(n cue.Value, s *state) {
	// 	s.usedTypes |= cue.StructKind

	// 	pkg := s.addImport("struct")
	// 	s.addConjunct(ast.NewCall(ast.NewSel(pkg, "MinFields"), s.uint(n)))
	// }),

	p0("maxProperties", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind

		pkg := s.addImport("struct")
		s.addConjunct(ast.NewCall(ast.NewSel(pkg, "MaxFields"), s.uint(n)))
	}),

	p0("dependencies", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind

		// Schema and property dependencies.
		// TODO: the easiest implementation is with comprehensions.
		// The nicer implementation is with disjunctions. This has to be done
		// at the very end, replacing properties.
		/*
			*{ property?: _|_ } | {
				property: _
				schema
			}
		*/
	}),

	p1("patternProperties", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind
		if n.Kind() != cue.StructKind {
			s.errf(n, `value of "patternProperties" must be an an object, found %v`, n.Kind)
		}
		if s.obj == nil {
			s.obj = &ast.StructLit{}
		}
		existing := excludeFields(s.obj.Elts)
		s.processMap(n, func(key string, n cue.Value) {
			// [!~(properties) & pattern]: schema
			s.patterns = append(s.patterns,
				&ast.UnaryExpr{Op: token.NMAT, X: ast.NewString(key)})
			f := &ast.Field{
				Label: ast.NewList(ast.NewBinExpr(token.AND,
					&ast.UnaryExpr{Op: token.MAT, X: ast.NewString(key)},
					existing)),
				Value: s.schema(n),
			}
			ast.SetRelPos(f, token.NewSection)
			s.obj.Elts = append(s.obj.Elts, f)
		})
	}),

	p2("additionalProperties", func(n cue.Value, s *state) {
		s.usedTypes |= cue.StructKind
		switch n.Kind() {
		case cue.BoolKind:
			s.closeStruct = !s.boolValue(n)

		case cue.StructKind:
			s.closeStruct = true
			if s.obj == nil {
				s.obj = &ast.StructLit{}
			}
			if len(s.obj.Elts) == 0 {
				s.obj.Elts = append(s.obj.Elts, &ast.Field{
					Label: ast.NewList(ast.NewIdent("string")),
					Value: s.schema(n),
				})
				return
			}
			// [!~(properties|patternProperties)]: schema
			existing := append(s.patterns, excludeFields(s.obj.Elts))
			f := &ast.Field{
				Label: ast.NewList(ast.NewBinExpr(token.AND, existing...)),
				Value: s.schema(n),
			}
			ast.SetRelPos(f, token.NewSection)
			s.obj.Elts = append(s.obj.Elts, f)

		default:
			s.errf(n, `value of "additionalProperties" must be an object or boolean`)
		}
	}),

	// Array constraints.

	p0("items", func(n cue.Value, s *state) {
		s.usedTypes |= cue.ListKind
		switch n.Kind() {
		case cue.StructKind:
			elem := s.schema(n)
			ast.SetRelPos(elem, token.NoRelPos)
			s.addConjunct(ast.NewList(&ast.Ellipsis{Type: elem}))

		case cue.ListKind:
			var a []ast.Expr
			for _, n := range s.listItems("items", n, true) {
				v := s.schema(n)
				ast.SetRelPos(v, token.NoRelPos)
				a = append(a, v)
			}
			s.list = ast.NewList(a...)
			s.addConjunct(s.list)

		default:
			s.errf(n, `value of "items" must be an object or array`)
		}
	}),

	p0("additionalItems", func(n cue.Value, s *state) {
		s.usedTypes |= cue.ListKind
		if s.list != nil {
			elem := s.schema(n)
			s.list.Elts = append(s.list.Elts, &ast.Ellipsis{Type: elem})
		}
	}),

	p0("contains", func(n cue.Value, s *state) {
		s.usedTypes |= cue.ListKind
		list := s.addImport("list")
		// TODO: Passing non-concrete values is not yet supported in CUE.
		if x := s.schema(n); !isAny(x) {
			s.addConjunct(ast.NewCall(ast.NewSel(list, "Contains"), clearPos(x)))
		}
	}),

	// TODO: min/maxContains

	p0("minItems", func(n cue.Value, s *state) {
		s.usedTypes |= cue.ListKind
		a := []ast.Expr{}
		p, err := n.Uint64()
		if err != nil {
			s.errf(n, "invalid uint")
		}
		for ; p > 0; p-- {
			a = append(a, ast.NewIdent("_"))
		}
		s.addConjunct(ast.NewList(append(a, &ast.Ellipsis{})...))

		// TODO: use this once constraint resolution is properly implemented.
		// list := s.addImport("list")
		// s.addConjunct(ast.NewCall(ast.NewSel(list, "MinItems"), clearPos(s.uint(n))))
	}),

	p0("maxItems", func(n cue.Value, s *state) {
		s.usedTypes |= cue.ListKind
		list := s.addImport("list")
		s.addConjunct(ast.NewCall(ast.NewSel(list, "MaxItems"), clearPos(s.uint(n))))
	}),

	p0("uniqueItems", func(n cue.Value, s *state) {
		s.usedTypes |= cue.ListKind
		if s.boolValue(n) {
			list := s.addImport("list")
			s.addConjunct(ast.NewCall(ast.NewSel(list, "UniqueItems")))
		}
	}),
}

func clearPos(e ast.Expr) ast.Expr {
	ast.SetRelPos(e, token.NoRelPos)
	return e
}
