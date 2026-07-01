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

// Package openapi provides functionality for mapping CUE to and from OpenAPI.
//
// [Generate] and [Gen] translate a CUE value into the schema components of an
// OpenAPI v3.0 document. [GenerateV2] translates a CUE value shaped like a
// whole OpenAPI document (v3.0 or v3.1) into that document.
//
// # Schema positions and the "#" convention
//
// When generating a whole document with [GenerateV2], the input is ordinary
// CUE data mirroring the OpenAPI document structure (info, paths, servers,
// security and so on), and it is emitted verbatim. The exception is the
// positions where the OpenAPI specification expects a Schema Object: there,
// the generator looks for a "#" definition holding a CUE schema and converts
// that schema into a JSON Schema.
//
// A reference to an existing top-level schema is therefore written by placing
// that schema behind a "#" field rather than at the schema position directly:
//
//	#Person: {name: string, age?: int}
//
//	paths: "/people": get: responses: "200": content: "application/json": schema: #: #Person
//
// An inline schema is written the same way:
//
//	schema: #: {name: string, age?: int}
//
// Writing schema: #Person directly (without the intervening "#") does not have
// the same meaning. The "#" marker is required, rather than interpreting
// whatever is found at a schema position as a schema, for several reasons:
//
//   - It keeps the distinction between data and schema explicit. Everything
//     outside a "#" is data emitted verbatim; only the contents of a "#" are
//     interpreted as a CUE schema to be converted. Schema positions are not
//     limited to fields named "schema", so a name-based rule would not
//     suffice.
//   - It keeps the input a well-formed CUE encoding of an OpenAPI document.
//     Because "#" fields are definitions, the document still conforms to the
//     OpenAPI specification and can, for example, be exported as JSON with
//     cue export (the schema parts are simply omitted). Placing a bare CUE
//     schema at a Schema Object position would instead constrain the eventual
//     data there, so the document as a whole would no longer be a plain
//     OpenAPI document matching the specification, which requires a JSON
//     Schema in that position.
//
// If a schema position contains no "#" definition, whatever value is found
// there is used verbatim as the JSON Schema.
//
// GenerateV2 is experimental and its API might change.
//
// See https://spec.openapis.org/oas/ for the OpenAPI specification.
package openapi
