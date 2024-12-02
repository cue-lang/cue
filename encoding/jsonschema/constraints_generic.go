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
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// Generic constraints

func constraintAddDefinitions(key string, n cue.Value, s *state) {
	if n.Kind() != cue.StructKind {
		s.errf(n, `"definitions" expected an object, found %s`, n.Kind())
	}

	s.processMap(n, func(key string, n cue.Value) {
		name := key

		var f *ast.Field

		ident := "#" + name
		if ast.IsValidIdent(ident) {
			expr, sub := s.schemaState(n, allTypes, []label{{ident, true}})
			f = &ast.Field{
				Label: ast.NewIdent(ident),
				Value: expr,
			}
			sub.doc(f)
		} else {
			expr, sub := s.schemaState(n, allTypes, []label{{"#", true}, {name: name}})
			inner := ast.NewStruct(&ast.Field{
				Label: ast.NewString(name),
				Value: expr,
			})
			// Ensure that we get `#: foo: ...` not `#: {foo: ...}`
			inner.Lbrace = token.NoPos
			ident = "#"
			f = &ast.Field{
				Label: ast.NewIdent("#"),
				Value: inner,
			}
			sub.doc(f)
		}

		ast.SetRelPos(f, token.NewSection)
		s.definitions = append(s.definitions, f)
		s.setField(label{name: ident, isDef: true}, f)
	})
}

func constraintComment(key string, n cue.Value, s *state) {
}

func constraintConst(key string, n cue.Value, s *state) {
	s.all.add(n, s.constValue(n))
	s.allowedTypes &= n.Kind()
	s.knownTypes &= n.Kind()
}

func constraintDefault(key string, n cue.Value, s *state) {
	sc := *s
	s.default_ = sc.value(n)
	// TODO: must validate that the default is subsumed by the normal value,
	// as CUE will otherwise broaden the accepted values with the default.
	s.examples = append(s.examples, s.default_)
}

func constraintDeprecated(key string, n cue.Value, s *state) {
	if s.boolValue(n) {
		s.deprecated = true
	}
}

func constraintDescription(key string, n cue.Value, s *state) {
	s.description, _ = s.strValue(n)
}

func constraintEnum(key string, n cue.Value, s *state) {
	var a []ast.Expr
	var types cue.Kind
	for _, x := range s.listItems("enum", n, true) {
		if (s.allowedTypes & x.Kind()) == 0 {
			// Enum value is redundant because it's
			// not in the allowed type set.
			continue
		}
		a = append(a, s.constValue(x))
		types |= x.Kind()
	}
	s.knownTypes &= types
	s.allowedTypes &= types
	if len(a) > 0 {
		s.all.add(n, ast.NewBinExpr(token.OR, a...))
	}
}

func constraintExamples(key string, n cue.Value, s *state) {
	if n.Kind() != cue.ListKind {
		s.errf(n, `value of "examples" must be an array, found %v`, n.Kind())
	}
	// TODO: implement examples properly.
	// for _, n := range s.listItems("examples", n, true) {
	// 	if ex := s.value(n); !isAny(ex) {
	// 		s.examples = append(s.examples, ex)
	// 	}
	// }
}

func constraintNullable(key string, n cue.Value, s *state) {
	// TODO: only allow for OpenAPI.
	null := ast.NewNull()
	setPos(null, n)
	s.nullable = null
}

func constraintRef(key string, n cue.Value, s *state) {
	u := s.resolveURI(n)

	fragmentParts, err := splitFragment(u)
	if err != nil {
		s.addErr(errors.Newf(n.Pos(), "%v", err))
		return
	}
	expr := s.makeCUERef(n, u, fragmentParts)
	if expr == nil {
		expr = &ast.BadExpr{From: n.Pos()}
	}

	s.all.add(n, expr)
}

func constraintTitle(key string, n cue.Value, s *state) {
	s.title, _ = s.strValue(n)
}

func constraintType(key string, n cue.Value, s *state) {
	var types cue.Kind
	set := func(n cue.Value) {
		str, ok := s.strValue(n)
		if !ok {
			s.errf(n, "type value should be a string")
		}
		switch str {
		case "null":
			types |= cue.NullKind
			s.setTypeUsed(n, nullType)
			// TODO: handle OpenAPI restrictions.
		case "boolean":
			types |= cue.BoolKind
			s.setTypeUsed(n, boolType)
		case "string":
			types |= cue.StringKind
			s.setTypeUsed(n, stringType)
		case "number":
			types |= cue.NumberKind
			s.setTypeUsed(n, numType)
		case "integer":
			types |= cue.IntKind
			s.setTypeUsed(n, numType)
			s.add(n, numType, ast.NewIdent("int"))
		case "array":
			types |= cue.ListKind
			s.setTypeUsed(n, arrayType)
		case "object":
			types |= cue.StructKind
			s.setTypeUsed(n, objectType)

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
}
