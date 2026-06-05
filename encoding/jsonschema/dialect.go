// Copyright 2026 CUE Authors
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

// tupleStyle describes how a dialect renders heterogeneous (tuple) lists.
type tupleStyle int

const (
	// tuplePrefixItems uses prefixItems for the known prefix and items
	// for the rest (JSON Schema 2020-12 and later).
	tuplePrefixItems tupleStyle = iota
	// tupleItemsArray uses an array-valued items keyword for the known
	// prefix and additionalItems for the rest (drafts up to and
	// including draft-07).
	tupleItemsArray
	// tupleWiden has no tuple support: the prefix and rest schemas are
	// widened into a single items schema (OpenAPI 3.0).
	tupleWiden
)

// dialect describes how a particular schema version renders the
// version-independent item tree built by [generator.makeItem]. All
// version-specific divergence in the generated output is expressed here.
type dialect struct {
	version Version

	// emitSchemaKeyword reports whether a top-level $schema keyword is
	// emitted. OpenAPI schema objects do not carry $schema.
	emitSchemaKeyword bool

	// refPrefix holds the JSON Pointer tokens identifying where
	// definitions live within the generated document, for example
	// {"$defs"}, {"definitions"} or {"components", "schemas"}. It is
	// used both to nest the definitions and to construct $ref pointers.
	refPrefix []string

	// allowTypeArray reports whether the type keyword may hold an array
	// of type names. OpenAPI 3.0 requires a single type string.
	allowTypeArray bool

	// nullViaNullable reports whether nullability is expressed with a
	// nullable: true keyword rather than including "null" in type.
	nullViaNullable bool

	// numericExclusive reports whether exclusiveMinimum/exclusiveMaximum
	// hold numbers (draft-06 and later). When false they are booleans
	// accompanying minimum/maximum (draft-04 and OpenAPI 3.0).
	numericExclusive bool

	// supportsConst reports whether the const keyword is available.
	// When false a single-valued enum is emitted instead.
	supportsConst bool

	// tuples describes how heterogeneous lists are rendered.
	tuples tupleStyle

	// supportsContains reports whether the contains keyword is available.
	supportsContains bool

	// supportsMinMaxContains reports whether minContains/maxContains are
	// available (2019-09 and later).
	supportsMinMaxContains bool

	// supportsPatternProperties reports whether patternProperties is
	// available.
	supportsPatternProperties bool

	// supportsIfThenElse reports whether if/then/else are available
	// (draft-07 and later).
	supportsIfThenElse bool

	// refComposesWithSiblings reports whether keywords adjacent to a
	// $ref are honored. Before 2019-09 (so for draft-07 and OpenAPI 3.0)
	// a $ref causes all sibling keywords to be ignored, so they must be
	// kept out of any schema object that contains a $ref.
	refComposesWithSiblings bool
}

// dialectFor returns the dialect used to generate the given version,
// or nil if generation is not supported for that version.
func dialectFor(v Version) *dialect {
	switch v {
	case VersionDraft2020_12:
		return &dialect{
			version:                   v,
			emitSchemaKeyword:         true,
			refPrefix:                 []string{"$defs"},
			allowTypeArray:            true,
			nullViaNullable:           false,
			numericExclusive:          true,
			supportsConst:             true,
			tuples:                    tuplePrefixItems,
			supportsContains:          true,
			supportsMinMaxContains:    true,
			supportsPatternProperties: true,
			supportsIfThenElse:        true,
			refComposesWithSiblings:   true,
		}
	case VersionDraft7:
		return &dialect{
			version:                   v,
			emitSchemaKeyword:         true,
			refPrefix:                 []string{"definitions"},
			allowTypeArray:            true,
			nullViaNullable:           false,
			numericExclusive:          true,
			supportsConst:             true,
			tuples:                    tupleItemsArray,
			supportsContains:          true,
			supportsMinMaxContains:    false,
			supportsPatternProperties: true,
			supportsIfThenElse:        true,
			refComposesWithSiblings:   false,
		}
	case VersionOpenAPI:
		return &dialect{
			version:                   v,
			emitSchemaKeyword:         false,
			refPrefix:                 []string{"components", "schemas"},
			allowTypeArray:            false,
			nullViaNullable:           true,
			numericExclusive:          false,
			supportsConst:             false,
			tuples:                    tupleWiden,
			supportsContains:          false,
			supportsMinMaxContains:    false,
			supportsPatternProperties: false,
			supportsIfThenElse:        false,
			refComposesWithSiblings:   false,
		}
	}
	return nil
}
