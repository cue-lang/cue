// module indicates the module's import path.
// For legacy reasons, we allow a missing module
// directory and an empty module directive.
module?: #Module | ""

// deps holds dependency information for modules, keyed by module path.
deps?: [#Module]: {
	// TODO numexist(<=1, replace, replaceAll)
	// TODO numexist(>=1, v, exclude, replace, replaceAll)

	// v indicates the required version of the module.
	// It is usually a #Semver but it can also name a branch
	// of the module. cue mod tidy will rewrite such branch
	// names to their canonical versions.
	v?: string
}

// #Module constraints a module path.
// TODO encode the module path rules as regexp:
// WIP: (([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))(/([\-_~a-zA-Z0-9][.\-_~a-zA-Z0-9]*[\-_~a-zA-Z0-9])|([\-_~a-zA-Z0-9]))*
#Module:            =~"^[^@]+$"

// TODO add the rest of the module schema definition.
