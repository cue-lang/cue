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


// #File represents the overarching module file schema.
#File: {
	// Reserve fields that are unimplemented for now.
	{
		deprecated?: unimplemented
		retract?:    unimplemented
		publish?:    unimplemented
		#Dep: {
			exclude?:    unimplemented
			replace?:    unimplemented
			replaceAll?: unimplemented
		}
	}
	// module indicates the module's path.
	module?: #Module | ""

	// version indicates the language version used by the code
	// in this module - the minimum version of CUE required
	// to evaluate the code in this module. When a later version of CUE
	// is evaluating code in this module, this will be used to
	// choose version-specific behavior. If an earlier version of CUE
	// is used, an error will be given.
	language?: version?: #Semver

	// description describes the purpose of this module.
	description?: string

	// When present, deprecated indicates that the module
	// is deprecated and includes information about that deprecation, ideally
	// mentioning an alternative that can be used instead.
	// TODO implement this.
	deprecated?: string

	// deps holds dependency information for modules, keyed by module path.
	deps?: [#Module]: #Dep

	#Dep: {
		// TODO use the below when mustexist is implemented.
		// replace and replaceAll are mutually exclusive.
		//mustexist(<=1, replace, replaceAll)
		// There must be at least one field specified for a given module.
		//mustexist(>=1, v, exclude, replace, replaceAll)

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

		// exclude excludes a set of versions of the module
		// TODO implement this.
		exclude?: [#Semver]: true

		// replace specifies replacements for specific versions of
		// the module. This field is exclusive with replaceAll.
		// TODO implement this.
		replace?: [#Semver]: #Replacement

		// replaceAll specifies a replacement for all versions of the module.
		// This field is exclusive with replace.
		// TODO implement this.
		replaceAll?: #Replacement
	}

	// retract specifies a set of previously published versions to retract.
	// TODO implement this.
	retract?: [... #RetractedVersion]

	// The publish section can be used to restrict the scope of a module to prevent
	// accidental publishing.
	// TODO complete the definition of this and implement it.
	publish?: _

	// #RetractedVersion specifies either a single version
	// to retract, or an inclusive range of versions to retract.
	#RetractedVersion: #Semver | {
		from!: #Semver
		// TODO constrain to to be after from?
		to!: #Semver
	}

	// #Replacement specifies a replacement for a module. It can either
	// be a reference to a local directory or an alternative module with associated
	// version.
	#Replacement: string | {
		m!: #Module
		v!: #Semver
	}

	// #Module constrains a module path.
	// The major version indicator is optional, but should always be present
	// in a normalized module.cue file.
	#Module: string

	// #Semver constrains a semantic version.
	#Semver: =~"."
}

// _#Strict can be unified with the top level schema to enforce the strict version
// of the schema required by the registry.
#Strict: #File & {
	// This regular expression is taken from https://semver.org/spec/v2.0.0.html
	#Semver: =~#"^v(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$"#

	// WIP: (([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))(/([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))*
	#Module: =~#"^[^@]+(@v(0|[1-9]\d*))$"#

	// We don't yet implement local file replacement (specified as a string)
	// so require a struct, thus allowing only replacement by some other module.
	#Replacement: {...}

	// The module declaration is required.
	module!: #Module

	// No null versions, because no replacements yet.
	#Dep: v!: #Semver

	// TODO require the CUE version?
	// language!: version!: _
}
