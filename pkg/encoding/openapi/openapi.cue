// Copyright 2023 CUE Authors
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

package openapi

// #Config represents options for generating OpenAPI.
#Config: {
	// version is fixed to 3.0.0 for now.
	version!: "3.0.0"

	info?: #Info

	// selfContained causes all non-expanded external references to be included
	// in this document.
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
