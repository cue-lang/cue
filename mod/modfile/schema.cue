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

// This schema constrains a module.cue file. This form constrains module.cue files
// outside of the main module. For the module.cue file in a main module, the schema
// is less restrictive, because wherever #Semver is used, a less specific version may be
// used instead, which will be rewritten to the canonical form by the cue command tooling.
// To check against that form,

// Note: we're using 1&2 rather than _|_ because
// use of _|_ causes the source location of the errors
// to be lost. See https://github.com/cue-lang/cue/issues/2319.
let unimplemented = 1&2

// versions holds an element for each supported version
// of the schema. The version key specifies that
// the schema covers all versions from then until the
// next version present in versions, or until the current CUE version,
// whichever is earlier.
versions: [string]: {
	// #File represents the overarching module file schema.
	#File!: {
		// We always require the language version, as we use
		// that to determine how to parse the file itself.
		// Note that this schema is not used by [ParseLegacy]
		// because legacy module.cue files did not support
		// language.version field.
		language!: version!: string
	}
	// #Strict can be unified with the top level schema to enforce the strict version
	// of the schema required when publishing a module.
	#Strict!: _
}

versions: "v0.8.0": {
	// Define this version in terms of the later versions
	// rather than the other way around, so that
	// the latest version is clearest.
	versions["v0.9.0-alpha.0"]

	// The source field was added in v0.9.0, so "remove"
	// it here by marking it as unimplemented.
	#File: source?: unimplemented
}

versions: "v0.9.0-alpha.0": {
	#File: {
		// module indicates the module's path.
		module?: #Module | ""

		// source holds information about the source of the files within the
		// module. This field is mandatory at publish time.
		source?: #Source

		// version indicates the language version used by the code
		// in this module - the minimum version of CUE required
		// to evaluate the code in this module. When a later version of CUE
		// is evaluating code in this module, this will be used to
		// choose version-specific behavior. If an earlier version of CUE
		// is used, an error will be given.
		language?: version?: #Semver

		// description describes the purpose of this module.
		description?: string

		// deps holds dependency information for modules, keyed by module path.
		deps?: [#Module]: #Dep

		#Dep: {
			// v indicates the minimum required version of the module.
			// This can be null if the version is unknown and the module
			// entry is only present to be replaced.
			v!: #Semver | null

			// default indicates this module is used as a default in case
			// more than one major version is specified for the same module
			// path. Imports must specify the exact major version for a
			// module path if there is more than one major version for that
			// path and default is not set for exactly one of them.
			// TODO implement this.
			default?: bool
		}

		// #Module constrains a module path.
		// The major version indicator is optional, but should always be present
		// in a normalized module.cue file.
		#Module: string

		// #Semver constrains a semantic version.
		#Semver: =~"."
	}

	// #Strict can be unified with the top level schema to enforce the strict version
	// of the schema required by the registry.
	#Strict: #File & {
		// This regular expression is taken from https://semver.org/spec/v2.0.0.html
		#Semver: =~#"^v(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$"#

		// WIP: (([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))(/([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))*
		#Module: =~#"^[^@]+(@v(0|[1-9]\d*))$"#

		// The module declaration is required.
		module!: #Module

		// No null versions, because no replacements yet.
		#Dep: v!: #Semver
	}

	// #Source describes a source of truth for a module's content.
	#Source: {
		// kind specifies the kind of source.
		//
		// The special value "none" signifies that the module that is
		// standalone, associated with no particular source other than
		// the contents of the module itself.
		kind!: "self" | "git"

		// TODO support for other VCSs:
		// kind!: "self" | "git" | "bzr" | "hg" | "svn"
	}
}