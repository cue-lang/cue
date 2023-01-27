// module indicates the module's import path.
// For legacy reasons, we allow a missing module
// directory and an empty module directive.
module?: *#Module | ""

// cue indicates the minimum required version of CUE
// for using to evaluated code in this module.
cue?: #Semver

// When present, deprecated indicates that the module
// is deprecated and includes information about that deprecation.
deprecated?: string

// deps holds dependency information for modules, keyed by module path.
deps?: [#Module]: {
	// TODO numexist(<=1, replace, replaceAll)
	// TODO numexist(>=1, v, exclude, replace, replaceAll)

	// v indicates the required version of the module.
	// It is usually a #Semver but it can also name a branch
	// of the module. cue mod tidy will rewrite such branch
	// names to their canonical versions.
	v?: string

	// exclude excludes a set of versions of the module
	exclude?: [#Semver]: true

	// replace specifies replacements for specific versions of
	// the module. This field is exclusive with replaceAll.
	replace?: [#Semver]: #Replacement

	// replaceAll specifies a replacement for all versions of the module.
	// This field is exlusive with replace.
	replaceAll?: #Replacement
}

// retract specifies a set of previously published versions to retract.
retract?: [... #RetractedVersion]

// #RetractedVersion specifies either a single version
// to retract, or an inclusive range of versions to retract.
#RetractedVersion: #Semver | {
	from: #Semver		// TODO required
	to: #Semver		// TODO required
}

// #Replacement specifies a replacement for a module. It can either
// be a reference to a local directory or an alternative module with associated
// version.
#Replacement: #LocalPath | {
	m: #Module		// TODO required
	v: #Semver		// TODO required
}

// #LocalPath constrains a filesystem path used for a module replacement,
// which must be either explicitly relative to the current directory or root-based.
#LocalPath:         =~"^(./|../|/)"

// #Module constraints a module path.
// TODO encode the module path rules as regexp:
// WIP: (([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))(/([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))*
#Module:            =~"^[^@]+$"

// #Semver constrains a semantic version. This regular expression is taken from
// https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
#Semver:            =~#"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$"#

