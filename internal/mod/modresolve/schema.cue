// Copyright 2024 CUE Authors
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

// #file represents the registry configuration schema.
#file: {
	moduleRegistries: [#modulePath]: #registry
	defaultRegistry?: #registry
}

#registry: {
	pathEncoding: *"path" | _
}

#registry: {
	host!: #hostname
	insecure?: bool
	repository?: #repository
	pathEncoding?: "path" | "hashAsRepo" | "hashAsTag"
	prefixForTags?: #tag

	// stripPrefix specifies that the pattern prefix should be
	// stripped from the module path before using as a repository
	// path. This only applies when pathEncoding is "path".
	stripPrefix?: bool
}

// TODO more specific schemas below
#modulePath: string
#hostname: string
#repository: string
#tag: string
