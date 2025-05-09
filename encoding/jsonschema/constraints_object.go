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
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// Object constraints

func constraintPreserveUnknownFields(key string, n cue.Value, s *state) {
	// x-kubernetes-preserve-unknown-fields stops the API server decoding
	// step from pruning fields which are not specified in the validation
	// schema. This affects fields recursively, but switches back to normal
	// pruning behaviour if nested properties or additionalProperties are
	// specified in the schema. This can either be true or undefined. False
	// is forbidden.
	// Note: by experimentation, "nested properties" means "within a schema
	// within a nested property" not "within a schema that has the properties keyword".
	if !s.boolValue(n) {
		s.errf(n, "x-kubernetes-preserve-unknown-fields value may not be false")
		return
	}
	// TODO check that it's specified on an object type. This requires
	// either setting a bool (hasPreserveUnknownFields?) and checking
	// later or making a new phase and placing this after "type" but
	// before "allOf", because it's important that this value be
	// passed down recursively to allOf and friends.
	s.preserveUnknownFields = true
}

func constraintGroupVersionKind(key string, n cue.Value, s *state) {
	// x-kubernetes-group-version-kind is used by Kubernetes schemas
	// to indicate the required values of the apiVersion and kind fields.
	items := s.listItems(key, n, false)
	if len(items) != 1 {
		// When there's more than one item, we _could_ generate
		// a disjunction over apiVersion and kind but for now, we'll
		// just ignore it.
		// TODO implement support for multiple items
		return
	}
	s.processMap(items[0], func(key string, n cue.Value) {
		if strings.HasPrefix(key, "x-") {
			// TODO are x- extension properties actually allowed in this context?
			return
		}
		switch key {
		case "group":
			return
		case "kind":
			s.k8sResourceKind, _ = s.strValue(n)
		case "version":
			s.k8sAPIVersion, _ = s.strValue(n)
		default:
			s.errf(n, "unknown field %q in x-kubernetes-group-version-kind item", key)
		}
	})
	if s.k8sResourceKind == "" || s.k8sAPIVersion == "" {
		s.errf(n, "x-kubernetes-group-version-kind needs both kind and version fields")
	}
}

func constraintAdditionalProperties(key string, n cue.Value, s *state) {
	switch n.Kind() {
	case cue.BoolKind:
		if s.boolValue(n) {
			s.openness = explicitlyOpen
		} else {
			if s.schemaVersion == VersionKubernetesCRD {
				s.errf(n, "additionalProperties may not be set to false in a CRD schema")
				return
			}
			s.openness = explicitlyClosed
		}
		_ = s.object(n)

	case cue.StructKind:
		obj := s.object(n)
		if len(obj.Elts) == 0 {
			obj.Elts = append(obj.Elts, &ast.Field{
				Label: ast.NewList(ast.NewIdent("string")),
				Value: s.schema(n),
			})
			s.openness = allFieldsCovered
			return
		}
		// [!~(properties|patternProperties)]: schema
		existing := append(s.patterns, excludeFields(obj.Elts)...)
		expr, _ := s.schemaState(n, allTypes, func(s *state) {
			s.preserveUnknownFields = false
		})
		f := internal.EmbedStruct(ast.NewStruct(&ast.Field{
			Label: ast.NewList(ast.NewBinExpr(token.AND, existing...)),
			Value: expr,
		}))
		obj.Elts = append(obj.Elts, f)
		s.openness = allFieldsCovered

	default:
		s.errf(n, `value of "additionalProperties" must be an object or boolean`)
		return
	}
	s.hasAdditionalProperties = true
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

func constraintEmbeddedResource(key string, n cue.Value, s *state) {
	// TODO:
	// - should fail if type has not been specified as "object"
	// - should fail if neither x-kubernetes-preserve-unknown-fields or properties have been specified

	// Note: this runs in a phase before the properties keyword so
	// that the embedded expression always comes first in the struct
	// literal.
	resourceDefinitionPath := cue.MakePath(cue.Hid("_embeddedResource", "_"))
	obj := s.object(n)

	// Generate a reference to a shared schema that all embedded resources
	// can share. If it already exists, that's fine.
	// TODO add an attribute to make it clear what's going on here
	// when encoding a CRD from CUE?
	s.builder.put(resourceDefinitionPath, ast.NewStruct(
		"apiVersion", token.NOT, ast.NewIdent("string"),
		"kind", token.NOT, ast.NewIdent("string"),
		"metadata", token.OPTION, ast.NewStruct(&ast.Ellipsis{}),
	), nil)
	refExpr, err := s.builder.getRef(resourceDefinitionPath)
	if err != nil {
		s.errf(n, `cannot get reference to embedded resource definition: %v`, err)
	} else {
		obj.Elts = append(obj.Elts, &ast.EmbedDecl{
			Expr: refExpr,
		})
	}
	s.allowedTypes &= cue.StructKind
}

func constraintProperties(key string, n cue.Value, s *state) {
	obj := s.object(n)

	if n.Kind() != cue.StructKind {
		s.errf(n, `"properties" expected an object, found %v`, n.Kind())
	}
	hasKind := false
	hasAPIVersion := false
	s.processMap(n, func(key string, n cue.Value) {
		// property?: value
		name := ast.NewString(key)
		expr, state := s.schemaState(n, allTypes, func(s *state) {
			s.preserveUnknownFields = false
		})
		f := &ast.Field{Label: name, Value: expr}
		if doc := state.comment(); doc != nil {
			ast.SetComments(f, []*ast.CommentGroup{doc})
		}
		f.Constraint = token.OPTION
		if s.k8sResourceKind != "" && key == "kind" {
			// Define a regular field with the specified kind value.
			f.Constraint = token.ILLEGAL
			f.Value = ast.NewString(s.k8sResourceKind)
			hasKind = true
		}
		if s.k8sAPIVersion != "" && key == "apiVersion" {
			// Define a regular field with the specified value.
			f.Constraint = token.ILLEGAL
			f.Value = ast.NewString(s.k8sAPIVersion)
			hasAPIVersion = true
		}
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
	// It's not entirely clear whether it's OK to have an x-kubernetes-group-version-kind
	// keyword without the kind and apiVersion properties but be defensive
	// and add them anyway even if they're not there already.
	if s.k8sAPIVersion != "" && !hasAPIVersion {
		obj.Elts = append(obj.Elts, &ast.Field{
			Label: ast.NewString("apiVersion"),
			Value: ast.NewString(s.k8sAPIVersion),
		})
	}
	if s.k8sResourceKind != "" && !hasKind {
		obj.Elts = append(obj.Elts, &ast.Field{
			Label: ast.NewString("kind"),
			Value: ast.NewString(s.k8sResourceKind),
		})
	}
	s.hasProperties = true
}

func constraintPropertyNames(key string, n cue.Value, s *state) {
	// [=~pattern]: _
	if names, _ := s.schemaState(n, cue.StringKind, nil); !isTop(names) {
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
		if f.Constraint == token.NOT {
			s.errf(n, "duplicate required field %q", str)
		}
		f.Constraint = token.NOT
	}
}
