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

	// Indicates the draft number in which this constraint is defined.
	draft int
	fn    constraintFunc
}

// A constraintFunc converts a given JSON Schema constraint (specified in n)
// to a CUE constraint recorded in state.
type constraintFunc func(key string, n cue.Value, s *state)

func p0(name string, f constraintFunc) *constraint {
	return &constraint{key: name, fn: f}
}

func p1d(name string, draft int, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 1, draft: draft, fn: f}
}

func p1(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 1, fn: f}
}

func p2(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 2, fn: f}
}

func p3(name string, f constraintFunc) *constraint {
	return &constraint{key: name, phase: 3, fn: f}
}

// TODO:
// writeOnly, readOnly

var constraintMap = map[string]*constraint{}

func init() {
	for _, c := range constraints {
		constraintMap[c.key] = c
	}
}

// Note: the following table is ordered lexically by keyword name.
// The various implementations are grouped by kind in the constaint-*.go files.

var constraints = []*constraint{
	p1d("$comment", 7, constraintComment),
	p1("$defs", constraintAddDefinitions),
	p0("$id", constraintID),
	p0("$schema", constraintSchema),
	p1("$ref", constraintRef),
	p1("additionalItems", constraintAdditionalItems),
	p3("additionalProperties", constraintAdditionalProperties),
	p2("allOf", constraintAllOf),
	p2("anyOf", constraintAnyOf),
	p1d("const", 6, constraintConst),
	p1("contains", constraintContains),
	p1d("contentEncoding", 7, constraintContentEncoding),
	p1d("contentMediaType", 7, constraintContentMediaType),
	p1("default", constraintDefault),
	p1("definitions", constraintAddDefinitions),
	p1("dependencies", constraintDependencies),
	p1("deprecated", constraintDeprecated),
	p1("description", constraintDescription),
	p1("enum", constraintEnum),
	p1("examples", constraintExamples),
	p1("exclusiveMaximum", constraintExclusiveMaximum),
	p1("exclusiveMinimum", constraintExclusiveMinimum),
	p0("id", constraintID),
	p1("items", constraintItems),
	p1("minItems", constraintMinItems),
	p1("maxItems", constraintMaxItems),
	p1("maxLength", constraintMaxLength),
	p1("maxProperties", constraintMaxProperties),
	p2("maximum", constraintMaximum),
	p1("minLength", constraintMinLength),
	p2("minimum", constraintMinimum),
	p1("multipleOf", constraintMultipleOf),
	p2("oneOf", constraintOneOf),
	p1("nullable", constraintNullable),
	p1("pattern", constraintPattern),
	p2("patternProperties", constraintPatternProperties),
	p1("properties", constraintProperties),
	p1d("propertyNames", 6, constraintPropertyNames),
	p2("required", constraintRequired),
	p1("title", constraintTitle),
	p1("type", constraintType),
	p1("uniqueItems", constraintUniqueItems),
}
