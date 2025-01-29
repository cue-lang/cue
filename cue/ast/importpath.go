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
		if i := strings.LastIndex(parts.Path, "/"); i >= 0 {
			parts.Qualifier = parts.Path[i+1:]
		} else {
			parts.Qualifier = parts.Path
		}
		if !IsValidIdent(parts.Qualifier) || strings.HasPrefix(parts.Qualifier, "#") || parts.Qualifier == "_" {
			parts.Qualifier = ""
		}
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
func (parts ImportPath) Canonical() ImportPath {
	if i := strings.LastIndex(parts.Path, "/"); i >= 0 && parts.Path[i+1:] == parts.Qualifier {
		parts.Qualifier = ""
		parts.ExplicitQualifier = false
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
		_, last, _ := cutLast(parts.Path, "/")
		if last != "" && last != parts.Qualifier {
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

func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return "", s, false
}
