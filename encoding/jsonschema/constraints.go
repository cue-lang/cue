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
	"net/url"
	"path"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
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

func combineSequence(name string, n cue.Value, s *state, op token.Token, f func(n cue.Value) ast.Expr) {
	if n.Kind() != cue.ListKind {
		s.errf(n, `value of %q must be an array, found %v`, name, n.Kind())
	}
	var a ast.Expr
	for _, n := range list(n) {
		if a == nil {
			a = f(n)
			continue
		}
		a = ast.NewBinExpr(token.OR, a, f(n))
	}
	if a == nil {
		s.errf(n, `empty array for %q`, name)
		return
	}
	s.add(a)
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
	s.kind |= cue.StructKind
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
		switch n.Kind() {
		case cue.StringKind:
			s.types = append(s.types, n)
		case cue.ListKind:
			for i, _ := n.List(); i.Next(); {
				s.types = append(s.types, i.Value())
			}
		default:
			s.errf(n, `value of "type" must be a string or list of strings`)
		}
	}),

	p0("enum", func(n cue.Value, s *state) {
		combineSequence("enum", n, s, token.OR, s.value)
		s.typeOptional = true
	}),

	p0d("const", 6, func(n cue.Value, s *state) {
		s.add(s.value(n))
	}),

	p0("default", func(n cue.Value, s *state) {
		s.default_ = s.value(n)
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
		for _, n := range list(n) {
			s.examples = append(s.examples, s.schema(n))
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
		if str, ok := s.strValue(n); ok {
			u, err := url.Parse(str)
			if err != nil {
				s.add(s.errf(n, "invalid JSON reference: %s", err))
				return
			}

			if u.Host != "" || u.Path != "" {
				s.add(s.errf(n, "external references (%s) not supported", str))
				// TODO: handle
				//    host:
				//      If the host corresponds to a package known to cue,
				//      load it from there. It would prefer schema converted to
				//      CUE, although we could consider loading raw JSON schema
				//      if present.
				//      If not present, advise the user to run cue get.
				//    path:
				//      Look up on file system or relatively to authority location.
				return
			}

			if !path.IsAbs(u.Fragment) {
				s.add(s.errf(n, "anchors (%s) not supported", u.Fragment))
				// TODO: support anchors
				return
			}

			// NOTE: Go bug?: url.URL has no raw representation of the fragment.
			// This means that %2F gets translated to `/` before it can be
			// split. This, in turn, means that field names cannot have a `/`
			// as name.
			a := strings.Split(u.Fragment[1:], "/")
			if a[0] != "definitions" && a[0] != "$def" {
				s.add(s.errf(n, "reference %q must resolve to definition", u.Fragment))
				return
			}
			s.add(ast.NewSel(ast.NewIdent(rootDefs), a[1:]...))

			// TODO: technically, a references could reference a non-definition.
			// In that case this will not resolve. We should detect cases that
			// are not definitions and then resolve those as literal values.
		}
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

	p0("allOf", func(n cue.Value, s *state) {
		combineSequence("allOf", n, s, token.AND, s.schema)
	}),

	p0("anyOf", func(n cue.Value, s *state) {
		combineSequence("anyOf", n, s, token.OR, s.schema)
	}),

	p0("oneOf", func(n cue.Value, s *state) {
		combineSequence("allOf", n, s, token.OR, s.schema)
	}),

	// String constraints

	p0("pattern", func(n cue.Value, s *state) {
		s.kind |= cue.StringKind
		s.add(&ast.UnaryExpr{Op: token.MAT, X: s.string(n)})
	}),

	p0d("contentMediaType", 7, func(n cue.Value, s *state) {
		s.kind |= cue.StringKind
	}),

	p0d("contentEncoding", 7, func(n cue.Value, s *state) {
		s.kind |= cue.StringKind
		// 7bit, 8bit, binary, quoted-printable and base64.
		// RFC 2054, part 6.1.
		// https://tools.ietf.org/html/rfc2045
		// TODO: at least handle bytes.
	}),

	// Number constraints

	p0("minimum", func(n cue.Value, s *state) {
		s.kind |= cue.NumberKind
		s.add(&ast.UnaryExpr{Op: token.GEQ, X: s.number(n)})
	}),

	p0("exclusiveMinimum", func(n cue.Value, s *state) {
		// TODO: should we support Draft 4 booleans?
		s.kind |= cue.NumberKind
		s.add(&ast.UnaryExpr{Op: token.GTR, X: s.number(n)})
	}),

	p0("maximum", func(n cue.Value, s *state) {
		s.kind |= cue.NumberKind
		s.add(&ast.UnaryExpr{Op: token.LEQ, X: s.number(n)})
	}),

	p0("exclusiveMaximum", func(n cue.Value, s *state) {
		// TODO: should we support Draft 4 booleans?
		s.kind |= cue.NumberKind
		s.add(&ast.UnaryExpr{Op: token.LSS, X: s.number(n)})
	}),

	p0("multipleOf", func(n cue.Value, s *state) {
		s.kind |= cue.NumberKind
		multiple := s.number(n)
		var x big.Int
		_, _ = n.MantExp(&x)
		if x.Cmp(big.NewInt(0)) != 1 {
			s.errf(n, `"multipleOf" value must be < 0; found %s`, n)
		}
		math := s.addImport("math")
		s.add(ast.NewCall(ast.NewSel(math, "MultipleOf"), multiple))
	}),

	// Object constraints

	p0("properties", func(n cue.Value, s *state) {
		s.kind |= cue.StructKind
		if s.obj == nil {
			s.obj = &ast.StructLit{}
		}
		if n.Kind() != cue.StructKind {
			s.errf(n, `"properties" expected an object, found %v`, n.Kind)
		}

		s.processMap(n, func(key string, n cue.Value) {
			// property?: value
			expr, state := s.schemaState(n)
			f := &ast.Field{Label: ast.NewString(key), Value: expr}
			state.doc(f)
			f.Optional = token.Blank.Pos()
			if len(s.obj.Elts) > 0 && len(f.Comments()) > 0 {
				// TODO: change formatter such that either a a NewSection on the
				// field or doc comment will cause a new section.
				ast.SetRelPos(f.Comments()[0], token.NewSection)
			}
			s.obj.Elts = append(s.obj.Elts, f)
		})
	}),

	p1("required", func(n cue.Value, s *state) {
		s.kind |= cue.StructKind
		if s.obj == nil {
			s.errf(n, `"required" without a "properties" field`)
		}
		if n.Kind() != cue.ListKind {
			s.errf(n, `value of "required" must be list of strings, found %v`, n.Kind)
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

		for _, n := range list(n) {
			str, ok := s.strValue(n)
			f := fields[str]
			if f == nil && ok {
				s.errf(n, "required field %q not in properties", str)
				continue
			}
			if f.Optional == token.NoPos {
				s.errf(n, "duplicate required field %q", str)
			}
			f.Optional = token.NoPos
		}
	}),

	p0d("propertyNames", 6, func(n cue.Value, s *state) {
		s.kind |= cue.StructKind
		// [=~pattern]: _
		s.add(ast.NewStruct(ast.NewList(s.schema(n)), ast.NewIdent("_")))
	}),

	p0("minProperties", func(n cue.Value, s *state) {
		s.kind |= cue.StructKind
		pkg := s.addImport("struct")
		s.add(ast.NewCall(ast.NewSel(pkg, "MinFields"), s.uint(n)))
	}),

	p0("maxProperties", func(n cue.Value, s *state) {
		s.kind |= cue.StructKind
		pkg := s.addImport("struct")
		s.add(ast.NewCall(ast.NewSel(pkg, "MaxFields"), s.uint(n)))
	}),

	p0("dependencies", func(n cue.Value, s *state) {
		s.kind |= cue.StructKind
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
		s.kind |= cue.StructKind
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
		s.kind |= cue.StructKind
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
		s.kind |= cue.ListKind
		if s.list != nil {
			s.errf(n, `"items" declared more than once, previous declaration at %s`, s.list.Pos())
		}
		switch n.Kind() {
		case cue.StructKind:
			elem := s.schema(n)
			ast.SetRelPos(elem, token.NoRelPos)
			s.add(ast.NewList(&ast.Ellipsis{Type: elem}))

		case cue.ListKind:
			var a []ast.Expr
			for _, n := range list(n) {
				v := s.schema(n)
				ast.SetRelPos(v, token.NoRelPos)
				a = append(a, v)
			}
			s.add(ast.NewList(a...))

		default:
			s.errf(n, `value of "items" must be an object or array`)
		}
	}),

	p0("contains", func(n cue.Value, s *state) {
		s.kind |= cue.ListKind
		list := s.addImport("list")
		// TODO: Passing non-concrete values is not yet supported in CUE.
		s.add(ast.NewCall(ast.NewSel(list, "Contains"), clearPos(s.schema(n))))
	}),

	p0("minItems", func(n cue.Value, s *state) {
		s.kind |= cue.ListKind
		list := s.addImport("list")
		s.add(ast.NewCall(ast.NewSel(list, "MinItems"), clearPos(s.uint(n))))
	}),

	p0("maxItems", func(n cue.Value, s *state) {
		s.kind |= cue.ListKind
		list := s.addImport("list")
		s.add(ast.NewCall(ast.NewSel(list, "MaxItems"), clearPos(s.uint(n))))
	}),

	p0("uniqueItems", func(n cue.Value, s *state) {
		s.kind |= cue.ListKind
		if s.boolValue(n) {
			list := s.addImport("list")
			s.add(ast.NewCall(ast.NewSel(list, "UniqueItems")))
		}
	}),
}

func clearPos(e ast.Expr) ast.Expr {
	ast.SetRelPos(e, token.NoRelPos)
	return e
}
