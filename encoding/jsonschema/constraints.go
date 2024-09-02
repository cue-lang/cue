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

	"cuelang.org/go/cue"
)

// TODO: skip invalid regexps containing ?! and foes.
// alternatively, fall back to  https://github.com/dlclark/regexp2

type constraint struct {
	key string

	// phase indicates on which pass c constraint should be added. This ensures
	// that constraints are applied in the correct order. For instance, the
	// "required" constraint validates that a listed field is contained in
	// "properties". For this to work, "properties" must be processed before
	// "required" and thus must have a lower phase number than the latter.
	phase int

	// versions holds the versions for which this constraint is defined.
	versions versionSet
	fn       constraintFunc
}

// A constraintFunc converts a given JSON Schema constraint (specified in n)
// to a CUE constraint recorded in state.
type constraintFunc func(key string, n cue.Value, s *state)

var constraintMap = map[string]*constraint{}

func init() {
	for _, c := range constraints {
		if _, ok := constraintMap[c.key]; ok {
			panic(fmt.Errorf("duplicate constraint entry for %q", c.key))
		}
		constraintMap[c.key] = c
	}
}

// Note: the following table is ordered lexically by keyword name.
// The various implementations are grouped by kind in the constraint-*.go files.

const numPhases = 5

var constraints = []*constraint{
	todo("$anchor", vfrom(VersionDraft2019_09)),
	p2d("$comment", constraintComment, vfrom(VersionDraft7)),
	p2("$defs", constraintAddDefinitions),
	todo("$dynamicAnchor", vfrom(VersionDraft2020_12)),
	todo("$dynamicRef", vfrom(VersionDraft2020_12)),
	p1d("$id", constraintID, vfrom(VersionDraft6)),
	todo("$recursiveAnchor", vbetween(VersionDraft2019_09, VersionDraft2020_12)),
	todo("$recursiveRef", vbetween(VersionDraft2019_09, VersionDraft2020_12)),
	p2("$ref", constraintRef),
	p0("$schema", constraintSchema),
	todo("$vocabulary", vfrom(VersionDraft2019_09)),
	p2d("additionalItems", constraintAdditionalItems, vto(VersionDraft2019_09)),
	p4("additionalProperties", constraintAdditionalProperties),
	p3("allOf", constraintAllOf),
	p3("anyOf", constraintAnyOf),
	p2d("const", constraintConst, vfrom(VersionDraft6)),
	p2d("contains", constraintContains, vfrom(VersionDraft6)),
	p2d("contentEncoding", constraintContentEncoding, vfrom(VersionDraft7)),
	p2d("contentMediaType", constraintContentMediaType, vfrom(VersionDraft7)),
	todo("contentSchema", vfrom(VersionDraft2019_09)),
	p2("default", constraintDefault),
	p2("definitions", constraintAddDefinitions),
	p2("dependencies", constraintDependencies),
	todo("dependentRequired", vfrom(VersionDraft2019_09)),
	todo("dependentSchemas", vfrom(VersionDraft2019_09)),
	p2("deprecated", constraintDeprecated),
	p2("description", constraintDescription),
	todo("else", vfrom(VersionDraft7)),
	p2("enum", constraintEnum),
	p2d("examples", constraintExamples, vfrom(VersionDraft6)),
	p2("exclusiveMaximum", constraintExclusiveMaximum),
	p2("exclusiveMinimum", constraintExclusiveMinimum),
	todo("format", allVersions),
	p1d("id", constraintID, vto(VersionDraft4)),
	todo("if", vfrom(VersionDraft7)),
	p2("items", constraintItems),
	p1d("maxContains", constraintMaxContains, vfrom(VersionDraft2019_09)),
	p2("maxItems", constraintMaxItems),
	p2("maxLength", constraintMaxLength),
	p2("maxProperties", constraintMaxProperties),
	p3("maximum", constraintMaximum),
	p1d("minContains", constraintMinContains, vfrom(VersionDraft2019_09)),
	p2("minItems", constraintMinItems),
	p2("minLength", constraintMinLength),
	todo("minProperties", allVersions),
	p3("minimum", constraintMinimum),
	p2("multipleOf", constraintMultipleOf),
	p3("not", constraintNot),
	p2("nullable", constraintNullable),
	p3("oneOf", constraintOneOf),
	p2("pattern", constraintPattern),
	p3("patternProperties", constraintPatternProperties),
	todo("prefixItems", vfrom(VersionDraft2020_12)),
	p2("properties", constraintProperties),
	p2d("propertyNames", constraintPropertyNames, vfrom(VersionDraft6)),
	todo("readOnly", vfrom(VersionDraft7)),
	p3("required", constraintRequired),
	todo("then", vfrom(VersionDraft7)),
	p2("title", constraintTitle),
	p2("type", constraintType),
	todo("unevaluatedItems", vfrom(VersionDraft2019_09)),
	todo("unevaluatedProperties", vfrom(VersionDraft2019_09)),
	p2("uniqueItems", constraintUniqueItems),
	todo("writeOnly", vfrom(VersionDraft7)),
}

func todo(name string, versions versionSet) *constraint {
	return &constraint{key: name, phase: 1, versions: versions, fn: constraintTODO}
}

func p0(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 0, versions: allVersions, fn: f}
}

func p1(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 1, versions: allVersions, fn: f}
}

func p2(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 2, versions: allVersions, fn: f}
}

func p3(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 3, versions: allVersions, fn: f}
}

func p4(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 4, versions: allVersions, fn: f}
}

func p1d(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 1, versions: versions, fn: f}
}

func p2d(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 2, versions: versions, fn: f}
}
