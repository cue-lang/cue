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
	"cuelang.org/go/internal"
)

// Object constraints

func constraintAdditionalProperties(key string, n cue.Value, s *state) {
	switch n.Kind() {
	case cue.BoolKind:
		s.closeStruct = !s.boolValue(n)
		_ = s.object(n)

	case cue.StructKind:
		s.closeStruct = true
		obj := s.object(n)
		if len(obj.Elts) == 0 {
			obj.Elts = append(obj.Elts, &ast.Field{
				Label: ast.NewList(ast.NewIdent("string")),
				Value: s.schema(n),
			})
			return
		}
		// [!~(properties|patternProperties)]: schema
		existing := append(s.patterns, excludeFields(obj.Elts)...)
		f := internal.EmbedStruct(ast.NewStruct(&ast.Field{
			Label: ast.NewList(ast.NewBinExpr(token.AND, existing...)),
			Value: s.schema(n),
		}))
		obj.Elts = append(obj.Elts, f)

	default:
		s.errf(n, `value of "additionalProperties" must be an object or boolean`)
	}
}

func constraintDependencies(key string, n cue.Value, s *state) {
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
}

func constraintMaxProperties(key string, n cue.Value, s *state) {
	pkg := s.addImport(n, "struct")
	x := ast.NewCall(ast.NewSel(pkg, "MaxFields"), s.uint(n))
	s.add(n, objectType, x)
}

func constraintMinProperties(key string, n cue.Value, s *state) {
	pkg := s.addImport(n, "struct")
	x := ast.NewCall(ast.NewSel(pkg, "MinFields"), s.uint(n))
	s.add(n, objectType, x)
}

func constraintPatternProperties(key string, n cue.Value, s *state) {
	if n.Kind() != cue.StructKind {
		s.errf(n, `value of "patternProperties" must be an object, found %v`, n.Kind())
	}
	obj := s.object(n)
	existing := excludeFields(s.obj.Elts)
	s.processMap(n, func(key string, n cue.Value) {
		if !s.checkRegexp(n, key) {
			return
		}

		// Record the pattern for potential use by
		// additionalProperties because patternProperties are
		// considered before additionalProperties.
		s.patterns = append(s.patterns,
			&ast.UnaryExpr{Op: token.NMAT, X: ast.NewString(key)})

		// We'll make a pattern constraint of the form:
		// 	[pattern & !~(properties)]: schema
		f := internal.EmbedStruct(ast.NewStruct(&ast.Field{
			Label: ast.NewList(ast.NewBinExpr(
				token.AND,
				append([]ast.Expr{&ast.UnaryExpr{Op: token.MAT, X: ast.NewString(key)}}, existing...)...,
			)),
			Value: s.schema(n),
		}))
		ast.SetRelPos(f, token.NewSection)
		obj.Elts = append(obj.Elts, f)
	})
}

func constraintProperties(key string, n cue.Value, s *state) {
	obj := s.object(n)

	if n.Kind() != cue.StructKind {
		s.errf(n, `"properties" expected an object, found %v`, n.Kind())
	}

	s.processMap(n, func(key string, n cue.Value) {
		// property?: value
		name := ast.NewString(key)
		expr, state := s.schemaState(n, allTypes)
		f := &ast.Field{Label: name, Value: expr}
		if doc := state.comment(); doc != nil {
			ast.SetComments(f, []*ast.CommentGroup{doc})
		}
		f.Optional = token.Blank.Pos()
		if len(obj.Elts) > 0 && len(f.Comments()) > 0 {
			// TODO: change formatter such that either a NewSection on the
			// field or doc comment will cause a new section.
			ast.SetRelPos(f.Comments()[0], token.NewSection)
		}
		if state.deprecated {
			switch expr.(type) {
			case *ast.StructLit:
				obj.Elts = append(obj.Elts, addTag(name, "deprecated", ""))
			default:
				f.Attrs = append(f.Attrs, internal.NewAttr("deprecated", ""))
			}
		}
		obj.Elts = append(obj.Elts, f)
	})
}

func constraintPropertyNames(key string, n cue.Value, s *state) {
	// [=~pattern]: _
	if names, _ := s.schemaState(n, cue.StringKind); !isTop(names) {
		x := ast.NewStruct(ast.NewList(names), top())
		s.add(n, objectType, x)
	}
}

func constraintRequired(key string, n cue.Value, s *state) {
	if n.Kind() != cue.ListKind {
		s.errf(n, `value of "required" must be list of strings, found %v`, n.Kind())
		return
	}

	obj := s.object(n)

	// Create field map
	fields := map[string]*ast.Field{}
	for _, d := range obj.Elts {
		f, ok := d.(*ast.Field)
		if !ok {
			continue // Could be embedding? See cirrus.json
		}
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
				Label:      ast.NewString(str),
				Value:      top(),
				Constraint: token.NOT,
			}
			fields[str] = f
			obj.Elts = append(obj.Elts, f)
			continue
		}
		if f.Optional == token.NoPos {
			s.errf(n, "duplicate required field %q", str)
		}
		f.Constraint = token.NOT
		f.Optional = token.NoPos
	}
}
