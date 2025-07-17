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

// Note: OpenAPI is excluded from version sets by default, as it does not fit in
// the linear progression of the rest of the JSON Schema versions.

var constraints = []*constraint{
	px("$anchor", constraintTODO, vfrom(VersionDraft2019_09)),
	p2("$comment", constraintComment, vfrom(VersionDraft7)),
	p2("$defs", constraintAddDefinitions, allVersions),
	px("$dynamicAnchor", constraintTODO, vfrom(VersionDraft2020_12)),
	px("$dynamicRef", constraintTODO, vfrom(VersionDraft2020_12)),
	p1("$id", constraintID, vfrom(VersionDraft6)),
	px("$recursiveAnchor", constraintTODO, vbetween(VersionDraft2019_09, VersionDraft2020_12)),
	px("$recursiveRef", constraintTODO, vbetween(VersionDraft2019_09, VersionDraft2020_12)),
	p2("$ref", constraintRef, allVersions|openAPI|k8sAPI),
	p0("$schema", constraintSchema, allVersions),
	px("$vocabulary", constraintTODO, vfrom(VersionDraft2019_09)),
	p4("additionalItems", constraintAdditionalItems, vto(VersionDraft2019_09)),
	p4("additionalProperties", constraintAdditionalProperties, allVersions|openAPILike),
	p3("allOf", constraintAllOf, allVersions|openAPILike),
	p3("anyOf", constraintAnyOf, allVersions|openAPILike),
	p2("const", constraintConst, vfrom(VersionDraft6)),
	p2("contains", constraintContains, vfrom(VersionDraft6)),
	p2("contentEncoding", constraintContentEncoding, vfrom(VersionDraft7)),
	p2("contentMediaType", constraintContentMediaType, vfrom(VersionDraft7)),
	px("contentSchema", constraintTODO, vfrom(VersionDraft2019_09)),
	p2("default", constraintDefault, allVersions|openAPILike),
	p2("definitions", constraintAddDefinitions, allVersions),
	p2("dependencies", constraintDependencies, allVersions),
	px("dependentRequired", constraintDependencies, vfrom(VersionDraft2019_09)),
	px("dependentSchemas", constraintDependencies, vfrom(VersionDraft2019_09)),
	p2("deprecated", constraintDeprecated, vfrom(VersionDraft2019_09)|openAPI),
	p2("description", constraintDescription, allVersions|openAPILike),
	px("discriminator", constraintTODO, openAPI),
	p1("else", constraintElse, vfrom(VersionDraft7)),
	p2("enum", constraintEnum, allVersions|openAPILike),
	px("example", constraintTODO, openAPILike),
	p2("examples", constraintExamples, vfrom(VersionDraft6)),
	p2("exclusiveMaximum", constraintExclusiveMaximum, allVersions|openAPILike),
	p2("exclusiveMinimum", constraintExclusiveMinimum, allVersions|openAPILike),
	px("externalDocs", constraintTODO, openAPILike),
	p1("format", constraintFormat, allVersions|openAPILike),
	p1("id", constraintID, vto(VersionDraft4)),
	p1("if", constraintIf, vfrom(VersionDraft7)),
	p2("items", constraintItems, allVersions|openAPILike),
	p1("maxContains", constraintMaxContains, vfrom(VersionDraft2019_09)),
	p2("maxItems", constraintMaxItems, allVersions|openAPILike),
	p2("maxLength", constraintMaxLength, allVersions|openAPILike),
	p2("maxProperties", constraintMaxProperties, allVersions|openAPILike),
	p3("maximum", constraintMaximum, allVersions|openAPILike),
	p1("minContains", constraintMinContains, vfrom(VersionDraft2019_09)),
	p2("minItems", constraintMinItems, allVersions|openAPILike),
	p2("minLength", constraintMinLength, allVersions|openAPILike),
	p1("minProperties", constraintMinProperties, allVersions|openAPILike),
	p3("minimum", constraintMinimum, allVersions|openAPILike),
	p2("multipleOf", constraintMultipleOf, allVersions|openAPILike),
	p3("not", constraintNot, allVersions|openAPILike),
	p2("nullable", constraintNullable, openAPILike),
	p3("oneOf", constraintOneOf, allVersions|openAPILike),
	p2("pattern", constraintPattern, allVersions|openAPILike),
	p3("patternProperties", constraintPatternProperties, allVersions),
	p2("prefixItems", constraintPrefixItems, vfrom(VersionDraft2020_12)),
	p2("properties", constraintProperties, allVersions|openAPILike),
	p2("propertyNames", constraintPropertyNames, vfrom(VersionDraft6)),
	px("readOnly", constraintTODO, vfrom(VersionDraft7)|openAPI),
	p3("required", constraintRequired, allVersions|openAPILike),
	p1("then", constraintThen, vfrom(VersionDraft7)),
	p2("title", constraintTitle, allVersions|openAPILike),
	p2("type", constraintType, allVersions|openAPILike),
	px("unevaluatedItems", constraintTODO, vfrom(VersionDraft2019_09)),
	px("unevaluatedProperties", constraintTODO, vfrom(VersionDraft2019_09)),
	p2("uniqueItems", constraintUniqueItems, allVersions|openAPILike),
	px("writeOnly", constraintTODO, vfrom(VersionDraft7)|openAPI),
	px("xml", constraintTODO, openAPI),
	p1("x-kubernetes-embedded-resource", constraintEmbeddedResource, k8s),
	p1("x-kubernetes-group-version-kind", constraintGroupVersionKind, k8sAPI),
	p2("x-kubernetes-int-or-string", constraintIntOrString, k8s),
	px("x-kubernetes-list-map-keys", constraintIgnore, k8s),
	px("x-kubernetes-list-type", constraintIgnore, k8s),
	px("x-kubernetes-map-type", constraintIgnore, k8s),
	px("x-kubernetes-patch-merge-key", constraintIgnore, k8s),
	px("x-kubernetes-patch-strategy", constraintIgnore, k8s),
	p2("x-kubernetes-preserve-unknown-fields", constraintPreserveUnknownFields, k8s),
	px("x-kubernetes-validations", constraintTODO, k8s),
}

// px represents a TODO constraint that we haven't decided on a phase for yet.
func px(name string, f constraintFunc, versions versionSet) *constraint {
	return p1(name, f, versions)
}

func p0(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 0, versions: versions, fn: f}
}

func p1(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 1, versions: versions, fn: f}
}

func p2(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 2, versions: versions, fn: f}
}

func p3(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 3, versions: versions, fn: f}
}

func p4(name string, f constraintFunc, versions versionSet) *constraint {
	return &constraint{key: name, phase: 4, versions: versions, fn: f}
}
