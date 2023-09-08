// This schema constrains a module.cue file. This form constrains module.cue files
// outside of the main module. For the module.cue file in a main module, the schema
// is less restrictive, because wherever #Semver is used, a less specific version may be
// used instead, which will be rewritten to the canonical form by the cue command tooling.
// To check against that form,

_#ModuleFile: {
	// module indicates the module's path.
	module?: #Module | ""

	// cue indicates the language version used by the code
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
		default?: bool

		// exclude excludes a set of versions of the module
		exclude?: [#Semver]: true

		// replace specifies replacements for specific versions of
		// the module. This field is exclusive with replaceAll.
		replace?: [#Semver]: #Replacement

		// replaceAll specifies a replacement for all versions of the module.
		// This field is exclusive with replace.
		replaceAll?: #Replacement
	}

	// retract specifies a set of previously published versions to retract.
	retract?: [... #RetractedVersion]

	// The publish section can be used to restrict the scope of a module to prevent
	// accidental publishing. This cannot be overridden on the command line.
	// A published module cannot widen the scope of what is reported here.
	publish?: {
		// Define the scope that is allowed by default.
		allow: #Scope

		// default overrides the default scope that is used on the command line.
		default: #Scope
	}

	#Scope: *"private" | "public"

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
	#Replacement: #LocalPath | {
		m!: #Module
		v!: #Semver
	}

	// #LocalPath constrains a filesystem path used for a module replacement,
	// which must be either explicitly relative to the current directory or root-based.
	#LocalPath: =~"^(./|../|/)"

	// #Module constrains a module path.
	// The major version indicator is optional, but should always be present
	// in a normalized module.cue file.
	// TODO encode the module path rules as regexp:
	#Module: string
	//#Module: =~#"^[^@]+(@v(0|[1-9]\d*))?$"#

	// #Semver constrains a semantic version. This regular expression is taken from
	// https://semver.org/spec/v2.0.0.html
	#Semver: =~"."
}
_#ModuleFile

// _#Strict can be unified with the top level schema to enforce the strict version
// of the schema required by the registry.
_#Strict: {
	#Semver: =~#"^v(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$"#

	// WIP: (([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))(/([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))*
	#Module: =~#"^[^@]+(@v(0|[1-9]\d*))$"#

	// No local paths allowed in the registry.
	#Replacement: {...}

	// The module declaration is required.
	module!: #Module

	// No null versions, because no replacements yet.
	#Dep: v!: #Semver

	// TODO require the CUE version?
	// cue!: _
}

// _#Reserved can be unified with the top level schema to reserve features that
// aren't yet implemented, so we can potentially change their definition
// later.
// TODO _#Reserved:
{
	// Note: we're using 1&2 rather than _|_ because
	// use of _|_ causes the source location of the errors
	// to be lost. See https://github.com/cue-lang/cue/issues/2319.

	deprecated?: 1&2
	retract?:    1&2
	publish?:    1&2
	#Dep: {
		exclude?:    1&2
		replace?:    1&2
		replaceAll?: 1&2
	}
}
