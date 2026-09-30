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
	"cmp"
	"fmt"
	"regexp/syntax"
	"slices"
	"strconv"
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

// constraintAdditionalProperties applies n to the properties that match
// none of the fields and patterns which the object has so far.
// [constraintUnevaluatedProperties] ends with that same step.
func constraintAdditionalProperties(key string, n cue.Value, s *state) {
	if s.patternSkipped && (n.Kind() != cue.BoolKind || !s.boolValue(n)) {
		// The properties of a skipped pattern cannot be told apart from
		// the remaining ones, so applying n could reject valid data.
		// Still decode n, which may hold errors or referenced schemas.
		s.schema(n)
		return
	}
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
		var existing []ast.Expr
		for _, pattern := range s.patterns {
			existing = append(existing, &ast.UnaryExpr{Op: token.NMAT, X: ast.NewString(pattern)})
		}
		existing = append(existing, excludeFields(obj.Elts)...)
		if len(existing) == 0 {
			existing = append(existing, ast.NewIdent("string"))
		}
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
		s.errf(n, `value of %q must be an object or boolean`, key)
		return
	}
	s.hasAdditionalProperties = true
}

// constraintUnevaluatedProperties implements "unevaluatedProperties" by
// declaring the properties that [evaluatedProps] finds and then treating
// the keyword like "additionalProperties".
func constraintUnevaluatedProperties(key string, n cue.Value, s *state) {
	if s.declareEvaluated(key, n) {
		constraintAdditionalProperties(key, n, s)
	} else {
		// Still decode n, which may hold errors or referenced schemas.
		s.schema(n)
	}
}

// declareEvaluated declares the properties evaluated by the in-place
// applicators of the schema in its object. It reports whether n, the
// value of the given keyword, can then be applied to the remaining ones.
//
// The fields which the object has already count as evaluated, including
// those added for "dependencies", just like for "additionalProperties".
func (s *state) declareEvaluated(key string, n cue.Value) bool {
	if s.pos.LookupPath(cue.MakePath(cue.Str("additionalProperties"))).Exists() {
		// A sibling "additionalProperties" evaluates every property
		// that "properties" and "patternProperties" do not.
		return false
	}
	if n.Kind() == cue.BoolKind && s.boolValue(n) {
		return true
	}
	if s.patternSkipped {
		return false // As in [constraintAdditionalProperties].
	}
	ev := evaluatedProps{
		root: s.schemaRoot(),
		seen: map[string]bool{s.pos.Path().String(): true},
	}
	ev.addApplicators(s.pos)
	if blocker := cmp.Or(ev.unknown, ev.inexact); blocker != "" && s.cfg.StrictFeatures {
		s.errf(n, "keyword %q not yet implemented in combination with %s", key, blocker)
		return false
	}
	if ev.unknown != "" || ev.all {
		// Either there is nothing left to constrain, or we cannot tell
		// what is left, where a partial answer would reject valid data.
		return false
	}
	obj := s.object(n)
	declared := fieldsByName(obj.Elts)
	for _, name := range ev.names {
		if declared[name] != nil {
			continue
		}
		f := &ast.Field{
			Label:      ast.NewString(name),
			Constraint: token.OPTION,
			Value:      top(),
		}
		declared[name] = f
		obj.Elts = append(obj.Elts, f)
	}
	for _, pattern := range ev.patterns {
		if !slices.Contains(s.patterns, pattern) {
			s.addPattern(obj, pattern, top())
		}
	}
	return true
}

// evaluatedProps collects the properties that the in-place applicators of
// a schema evaluate, which "unevaluatedProperties" then leaves alone.
//
// Which subschemas apply is only known statically for "allOf" and "$ref".
// The others, such as "anyOf", contribute the properties of those of
// their subschemas that an instance satisfies; these are all included,
// which never rejects a valid instance but does accept some invalid ones.
type evaluatedProps struct {
	// root is the root of the schema resource being inspected,
	// which "$ref" values are resolved against.
	root *state

	names    []string // keys of "properties"
	patterns []string // keys of "patternProperties"

	// all holds whether every property is evaluated.
	all bool

	// inexact describes the first instance-dependent keyword found, if any.
	inexact string

	// unknown describes the first keyword found, if any,
	// whose evaluated properties cannot be determined.
	unknown string

	// seen holds the paths of the schemas visited so far,
	// starting with the schema whose applicators are inspected.
	seen map[string]bool
}

// addApplicators adds the properties evaluated by the subschemas which
// the in-place applicators of the schema n apply to the same instance.
func (ev *evaluatedProps) addApplicators(n cue.Value) {
	ev.root.processMap(n, func(key string, n cue.Value) {
		switch key {
		case "$ref":
			if target, ok := ev.root.localRefTarget(n); ok {
				ev.addSchema(target)
			} else {
				ev.unknown = cmp.Or(ev.unknown, fmt.Sprintf("%q: %v", key, n))
			}
		case "$dynamicRef", "$recursiveRef":
			ev.unknown = cmp.Or(ev.unknown, strconv.Quote(key))
		case "allOf", "anyOf", "oneOf":
			if key != "allOf" {
				ev.inexact = cmp.Or(ev.inexact, strconv.Quote(key))
			}
			for i, _ := n.List(); i.Next(); {
				ev.addSchema(i.Value())
			}
		case "if", "then", "else":
			ev.inexact = cmp.Or(ev.inexact, strconv.Quote(key))
			ev.addSchema(n)
		case "dependentSchemas", "dependencies":
			ev.root.processMap(n, func(_ string, n cue.Value) {
				if n.Kind() == cue.ListKind {
					return // Required properties are not evaluated.
				}
				ev.inexact = cmp.Or(ev.inexact, strconv.Quote(key))
				ev.addSchema(n)
			})
		}
	})
}

// addSchema adds the properties evaluated by the subschema n itself
// as well as by its in-place applicators.
func (ev *evaluatedProps) addSchema(n cue.Value) {
	path := n.Path().String()
	if n.Kind() != cue.StructKind || ev.seen[path] {
		return // A boolean schema evaluates no properties.
	}
	ev.seen[path] = true
	ev.root.processMap(n, func(key string, v cue.Value) {
		switch key {
		case "$id":
			if path != ev.root.pos.Path().String() {
				// References inside another schema resource
				// resolve against its own base URI.
				ev.unknown = cmp.Or(ev.unknown, `a nested "$id"`)
			}
		case "properties":
			ev.root.processMap(v, func(name string, _ cue.Value) {
				ev.names = append(ev.names, name)
			})
		case "patternProperties":
			ev.root.processMap(v, func(pattern string, _ cue.Value) {
				if _, err := syntax.Parse(pattern, syntax.Perl); err != nil {
					ev.unknown = cmp.Or(ev.unknown, fmt.Sprintf("%q: %q", key, pattern))
					return
				}
				ev.patterns = append(ev.patterns, pattern)
			})
		case "additionalProperties", "unevaluatedProperties":
			ev.all = true
		}
	})
	ev.addApplicators(n)
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
	// aliasv2 is what enables the postfix alias form, whether because the
	// target language version has it stable or because it names it outright.
	postfixAliases := s.targetExp.AliasV2
	s.processMap(n, func(key string, n cue.Value) {
		var ident *ast.Ident
		// TODO we could potentially avoid declaring the field
		// by checking whether there's a field already in
		// scope with the correct name.
		var label ast.Label
		var alias *ast.PostfixAlias
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
			// A non-identifier label needs an alias to be referenced from
			// the guard below, in whichever spelling the target language
			// version parses.
			if postfixAliases {
				label = ast.NewString(key)
				alias = &ast.PostfixAlias{Field: ident}
			} else {
				label = &ast.Alias{Ident: ident, Expr: ast.NewString(key)}
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
			Alias:      alias,
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
			s.patternSkipped = true
			return
		}
		s.addPattern(obj, key, s.schema(n))
	})
}

// addPattern adds a pattern constraint of the form
//
//	[=~pattern]: value
//
// to obj, recording the pattern for potential use by additionalProperties
// because patternProperties are considered before additionalProperties.
func (s *state) addPattern(obj *ast.StructLit, pattern string, value ast.Expr) {
	s.patterns = append(s.patterns, pattern)
	f := embedStruct(ast.NewStruct(&ast.Field{
		Label: ast.NewList(&ast.UnaryExpr{Op: token.MAT, X: ast.NewString(pattern)}),
		Value: value,
	}))
	ast.SetRelPos(f, token.NewSection)
	obj.Elts = append(obj.Elts, f)
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
		if len(obj.Elts) > 0 && len(ast.Comments(f)) > 0 {
			// TODO: change formatter such that either a NewSection on the
			// field or doc comment will cause a new section.
			ast.SetRelPos(ast.Comments(f)[0], token.NewSection)
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

// fieldsByName returns the fields among decls, keyed by name.
func fieldsByName(decls []ast.Decl) map[string]*ast.Field {
	fields := map[string]*ast.Field{}
	for _, d := range decls {
		f, ok := d.(*ast.Field)
		if !ok {
			continue // Could be embedding? See cirrus.json
		}
		str, _, err := ast.LabelName(f.Label)
		if err == nil {
			fields[str] = f
		}
	}
	return fields
}

func constraintRequired(key string, n cue.Value, s *state) {
	if n.Kind() != cue.ListKind {
		s.errf(n, `value of "required" must be list of strings, found %v`, n.Kind())
		return
	}

	obj := s.object(n)
	fields := fieldsByName(obj.Elts)

	for _, n := range s.listItems("required", n, true) {
		str, ok := s.strValue(n)
		if !ok {
			continue // strValue already reported the error
		}
		f := fields[str]
		if f == nil {
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
