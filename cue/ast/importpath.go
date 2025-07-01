package ast

import "strings"

// ParseImportPath returns the various components of an import path.
// It does not check the result for validity.
func ParseImportPath(p string) ImportPath {
	var parts ImportPath
	pathWithoutQualifier := p
	if i := strings.LastIndexAny(p, "/:"); i >= 0 && p[i] == ':' {
		pathWithoutQualifier = p[:i]
		parts.Qualifier = p[i+1:]
		parts.ExplicitQualifier = true
	}
	parts.Path = pathWithoutQualifier
	if path, version, ok := SplitPackageVersion(pathWithoutQualifier); ok {
		parts.Version = version
		parts.Path = path
	}
	if !parts.ExplicitQualifier {
		parts.Qualifier = impliedQualifier(parts.Path)
	}
	return parts
}

// ImportPath holds the various components of an import path.
type ImportPath struct {
	// Path holds the base package/directory path, similar
	// to that returned by [Version.BasePath].
	Path string

	// Version holds the version of the import
	// or empty if not present. Note: in general this
	// will contain a major version only, but there's no
	// guarantee of that.
	Version string

	// Qualifier holds the package qualifier within the path.
	// This will be derived from the last component of Path
	// if it wasn't explicitly present in the import path.
	// This is not guaranteed to be a valid CUE identifier.
	Qualifier string

	// ExplicitQualifier holds whether the qualifier will
	// always be added regardless of whether it matches
	// the final path element.
	ExplicitQualifier bool
}

// Canonical returns the canonical form of the import path.
// Specifically, it will only include the package qualifier
// if it's different from the last component of parts.Path.
//
// It also ensures that the Qualifier field is set when
// appropriate.
func (parts ImportPath) Canonical() ImportPath {
	q := impliedQualifier(parts.Path)
	if q == "" {
		parts.ExplicitQualifier = parts.Qualifier != ""
		return parts
	}
	if q == parts.Qualifier {
		// The qualifier matches the implied qualifier, so ensure that
		// it is not included in string representations.
		parts.ExplicitQualifier = false
	} else if parts.Qualifier == "" && !parts.ExplicitQualifier {
		// There's an implied qualifier but none set; this
		// could happen if someone has manually constructed the
		// ImportPath instance (it should never happen otherwise),
		// so be defensive and set the qualifier anyway.
		parts.Qualifier = q
		parts.ExplicitQualifier = false
	} else {
		// There's a qualifier set that does not match the implied
		// qualifier. This must be explicit.
		parts.ExplicitQualifier = true
	}
	return parts
}

// Unqualified returns the import path without any package qualifier.
func (parts ImportPath) Unqualified() ImportPath {
	parts.Qualifier = ""
	parts.ExplicitQualifier = false
	return parts
}

func (parts ImportPath) String() string {
	needQualifier := parts.ExplicitQualifier
	if !needQualifier && parts.Qualifier != "" {
		if impliedQualifier(parts.Path) != parts.Qualifier {
			needQualifier = true
		}
	}
	if parts.Version == "" && !needQualifier {
		// Fast path.
		return parts.Path
	}
	var buf strings.Builder
	buf.WriteString(parts.Path)
	if parts.Version != "" {
		buf.WriteByte('@')
		buf.WriteString(parts.Version)
	}
	if needQualifier {
		buf.WriteByte(':')
		buf.WriteString(parts.Qualifier)
	}
	return buf.String()
}

// impliedQualifier returns the package qualifier implied
// from the last component of the (bare) package path.
func impliedQualifier(path string) string {
	var q string
	if i := strings.LastIndex(path, "/"); i >= 0 {
		q = path[i+1:]
	} else {
		q = path
	}
	if !IsValidIdent(q) || strings.HasPrefix(q, "#") || q == "_" {
		return ""
	}
	return q
}

// SplitPackageVersion returns a prefix and version suffix such that
// prefix+"@"+version == path.
//
// SplitPackageVersion returns (path, "", false) when there is no `@`
// character splitting the path or if the version is empty.
//
// It does not check that the version is valid in any way other than
// checking that it is not empty.
//
// For example:
//
// SplitPackageVersion("foo.com/bar@v0.1") returns ("foo.com/bar", "v0.1", true).
// SplitPackageVersion("foo.com/bar@badvers") returns ("foo.com/bar", "badvers", true).
// SplitPackageVersion("foo.com/bar") returns ("foo.com/bar", "", false).
// SplitPackageVersion("foo.com/bar@") returns ("foo.com/bar@", "", false).
func SplitPackageVersion(path string) (prefix, version string, ok bool) {
	prefix, vers, ok := strings.Cut(path, "@")
	if vers == "" {
		ok = false
	}
	return prefix, vers, ok
}
