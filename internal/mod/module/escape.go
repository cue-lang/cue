package module

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// EscapePath returns the escaped form of the given module path.
// It fails if the module path is invalid.
func EscapePath(path string) (escaped string, err error) {
	if err := CheckPath(path); err != nil {
		return "", err
	}
	// Technically there's no need to escape capital letters because CheckPath
	// doesn't allow them, but let's be defensive.
	return escapeString(path)
}

// EscapeVersion returns the escaped form of the given module version.
// Versions are allowed to be in non-semver form but must be valid file names
// and not contain exclamation marks.
func EscapeVersion(v string) (escaped string, err error) {
	if err := checkElem(v, filePath); err != nil || strings.Contains(v, "!") {
		return "", &InvalidVersionError{
			Version: v,
			Err:     fmt.Errorf("disallowed version string"),
		}
	}
	return escapeString(v)
}

func escapeString(s string) (escaped string, err error) {
	haveUpper := false
	for _, r := range s {
		if r == '!' || r >= utf8.RuneSelf {
			// This should be disallowed by CheckPath, but diagnose anyway.
			// The correctness of the escaping loop below depends on it.
			return "", fmt.Errorf("internal error: inconsistency in EscapePath")
		}
		if 'A' <= r && r <= 'Z' {
			haveUpper = true
		}
	}

	if !haveUpper {
		return s, nil
	}

	var buf []byte
	for _, r := range s {
		if 'A' <= r && r <= 'Z' {
			buf = append(buf, '!', byte(r+'a'-'A'))
		} else {
			buf = append(buf, byte(r))
		}
	}
	return string(buf), nil
}

// UnescapePath returns the module path for the given escaped path.
// It fails if the escaped path is invalid or describes an invalid path.
func UnescapePath(escaped string) (path string, err error) {
	path, ok := unescapeString(escaped)
	if !ok {
		return "", fmt.Errorf("invalid escaped module path %q", escaped)
	}
	if err := CheckPath(path); err != nil {
		return "", fmt.Errorf("invalid escaped module path %q: %v", escaped, err)
	}
	return path, nil
}

// UnescapeVersion returns the version string for the given escaped version.
// It fails if the escaped form is invalid or describes an invalid version.
// Versions are allowed to be in non-semver form but must be valid file names
// and not contain exclamation marks.
func UnescapeVersion(escaped string) (v string, err error) {
	v, ok := unescapeString(escaped)
	if !ok {
		return "", fmt.Errorf("invalid escaped version %q", escaped)
	}
	if err := checkElem(v, filePath); err != nil {
		return "", fmt.Errorf("invalid escaped version %q: %v", v, err)
	}
	return v, nil
}

func unescapeString(escaped string) (string, bool) {
	var buf []byte

	bang := false
	for _, r := range escaped {
		if r >= utf8.RuneSelf {
			return "", false
		}
		if bang {
			bang = false
			if r < 'a' || 'z' < r {
				return "", false
			}
			buf = append(buf, byte(r+'A'-'a'))
			continue
		}
		if r == '!' {
			bang = true
			continue
		}
		if 'A' <= r && r <= 'Z' {
			return "", false
		}
		buf = append(buf, byte(r))
	}
	if bang {
		return "", false
	}
	return string(buf), true
}
