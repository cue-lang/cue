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
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
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
	var group, version string
	s.processMap(items[0], func(key string, n cue.Value) {
		if strings.HasPrefix(key, "x-") {
			// TODO are x- extension properties actually allowed in this context?
			return
		}
		switch key {
		case "group":
			group, _ = s.strValue(n)
		case "kind":
			s.k8sResourceKind, _ = s.strValue(n)
		case "version":
			version, _ = s.strValue(n)
		default:
			s.errf(n, "unknown field %q in x-kubernetes-group-version-kind item", key)
		}
	})
	if s.k8sResourceKind == "" || version == "" {
		s.errf(n, "x-kubernetes-group-version-kind needs both kind and version fields")
	}
	if group == "" {
		s.k8sAPIVersion = version
	} else {
		s.k8sAPIVersion = group + "/" + version
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
		f := embedStruct(ast.NewStruct(&ast.Field{
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

func embedStruct(s *ast.StructLit) *ast.EmbedDecl {
	e := &ast.EmbedDecl{Expr: s}
	if len(s.Elts) == 1 {
		d := s.Elts[0]
		astutil.CopyPosition(e, d)
		ast.SetRelPos(d, token.NoSpace)
		astutil.CopyComments(e, d)
		ast.SetComments(d, nil)
		if f, ok := d.(*ast.Field); ok {
			ast.SetRelPos(f.Label, token.NoSpace)
		}
	}
	s.Lbrace = token.Newline.Pos()
	s.Rbrace = token.NoSpace.Pos()
	return e
}

// constraintDependencies is used to implement all of the dependencies,
// dependentSchemas and dependentRequired keywords.
func constraintDependencies(key string, n cue.Value, s *state) {
	allowSchemas := false
	allowRequired := false
	switch key {
	case "dependencies":
		allowSchemas = true
		allowRequired = true
	case "dependentSchemas":
		allowSchemas = true
	case "dependentRequired":
		allowRequired = true
	}

	if n.Kind() != cue.StructKind {
		s.errf(n, `"%q expected an object, found %v`, key, n.Kind())
		return
	}
	// Our approach here is to make an outer-level struct which contains
	// all the fields we wish to test for as optional fields; then we add
	// a comprehension for each dependencies entry that tests for presence
	// and adds an appropriate constraint.
	//
	// e.g.
	// 	"dependencies": {
	//		"x": ["a", "b"],
	//		"y": {"maxProperties": 4}
	//	}
	//
	// is translated to:
	//
	// 	{
	// 		x?: _
	// 		if x != _|_ {
	// 			a!: _
	// 			b!: _
	// 		}
	// 		y?: _
	// 		if y != _|_ {
	// 			struct.MaxFields(4)
	// 		}
	// 	}

	obj := s.object(n)
	count := 0
	s.processMap(n, func(key string, n cue.Value) {
		var ident *ast.Ident
		// TODO we could potentially avoid declaring the field
		// by checking whether there's a field already in
		// scope with the correct name.
		var label ast.Label
		if ast.IsValidIdent(key) {
			// TODO if the inner schema contains a reference to some
			// outer-level entity that has the same identifier then this
			// will stop that from working correctly. It would be nice
			// if astutil.Sanitize was clever enough to deal with this
			// kind of alias issue.
			// Possible workaround:
			// - always make a local alias
			// - always make a local alias when the value is a schema but not when
			//   it's a list
			// - when the value is a schema, generate it and then inspect the
			//   resulting syntax to check for references.
			// - fix astutil.Sanitize
			ident = ast.NewIdent(key)
			label = ident
		} else {
			ident = ast.NewIdent(fmt.Sprintf("_t%d", count))
			count++
			label = &ast.Alias{
				Ident: ident,
				Expr:  ast.NewString(key),
			}
		}
		// TODO this is not quite right, because by adding this optional
		// field, we allow the field to exist, and that's not to-spec.
		// In particular, see the "additionalProperties doesn't consider dependentSchemas"
		// test in testdata/external/tests/draft2019-09/additionalProperties.json
		// which this approach causes to succeed inappropriately.
		// Given that people are unlikely to be checking for the existence of a field
		// without that field being a possibility, this is probably OK for now.
		// A better approach would involve "self" or otherwise obtaining a reference
		// to the current struct value.
		obj.Elts = append(obj.Elts, &ast.Field{
			Label:      label,
			Constraint: token.OPTION,
			Value:      ast.NewIdent("_"),
		})
		var consequence *ast.StructLit
		switch n.Kind() {
		case cue.ListKind:
			if !allowRequired {
				s.errf(n, "expected schema but got %v", n.Kind())
				return
			}
			required := &ast.StructLit{}
			for i, _ := n.List(); i.Next(); {
				f, ok := s.strValue(i.Value())
				if !ok {
					return
				}
				required.Elts = append(required.Elts, &ast.Field{
					Label:      ast.NewString(f),
					Constraint: token.NOT,
					Value:      ast.NewIdent("_"),
				})
			}
			consequence = required

		case cue.StructKind, cue.BoolKind:
			if !allowSchemas {
				s.errf(n, "expected schema but got %v", n.Kind())
				return
			}
			switch s := s.schema(n).(type) {
			case *ast.StructLit:
				consequence = s
			default:
				consequence = &ast.StructLit{
					Elts: []ast.Decl{
						&ast.EmbedDecl{
							Expr: s,
						},
					},
				}
			}
		default:
			s.errf(n, "dependency value must be array or schema. found %v", n.Kind())
			return
		}
		obj.Elts = append(obj.Elts, &ast.Comprehension{
			Clauses: []ast.Clause{
				&ast.IfClause{
					Condition: ast.NewBinExpr(token.NEQ, ident, &ast.BottomLit{}),
				},
			},
			Value: consequence,
		})
	})
	// Note: include an empty struct literal so that the comprehension
	// does cause the struct to disallow non-struct values.
	// See https://cuelang.org/issue/3994
	obj.Elts = append(obj.Elts, &ast.StructLit{})
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
		// 	[pattern]: schema
		f := embedStruct(ast.NewStruct(&ast.Field{
			Label: ast.NewList(&ast.UnaryExpr{Op: token.MAT, X: ast.NewString(key)}),
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
				f.Attrs = append(f.Attrs, &ast.Attribute{Text: "@deprecated()"})
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
