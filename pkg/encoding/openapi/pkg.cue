// Copyright 2026 The CUE Authors
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

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

// Package openapi provides OpenAPI encoding and decoding functionality.
//
// This is an EXPERIMENTAL API.
package openapi

// Marshal returns the OpenAPI encoding of schema for the given OpenAPI version.
// The optional config value can be used to make further adjustments.
//
// Experimental: this API may change.
//
// schema can have the following fields:
//
// 	#Config: {
// 	  // version holds the OpenAPI version to use when marshaling.
// 	  // Currently only "3.0.0" is supported.
// 	  version!: "3.0.0" // currently "3.0.0" only
//
// 	  // selfContained has no effect; all non-expanded external
// 	  // references are always included.
// 	  selfContained?: bool
//
// 	  // expandReferences replaces references with actual objects when generating
// 	  // OpenAPI Schema. It is an error for an CUE value to refer to itself
// 	  // if this option is used.
// 	  expandReferences?: bool
//
// 	  // info specifies the info section of the OpenAPI document. To be a valid
// 	  // OpenAPI document, it must include at least the title and version fields.
// 	  info?: {
// 	    title: string
// 	    description: string
// 	    version: string
// 	  }
// 	}
MarshalSchema: func(config: _, schema: _ @schema()) -> string

// #Config represents options for generating OpenAPI.
#Config: {
	// version is fixed to 3.0.0 for now.
	version!: "3.0.0"

	info?: #Info

	// selfContained has no effect; all non-expanded external references are
	// always included in the generated document.
	selfContained: bool | *false

	// expandReferences replaces references with actual objects when generating
	// OpenAPI Schema. It is an error for an CUE value to refer to itself
	// if this option is used.
	expandReferences: bool | *false
}

// #Info represents metadata about the API.
#Info: {
	title!:          string
	version!:        string
	summary?:        string
	description?:    string
	termsOfService?: string
	contact?:        #Contact
	license?:        #License
}

// #Contact represents contact information for the exposed API.
#Contact: {
	name?:  string
	url?:   string
	email?: string
}

// #License represents license information for the exposed API.
#License: {
	name!: string
	url?:  string
}
