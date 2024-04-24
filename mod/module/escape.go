package module

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"cuelang.org/go/internal/mod/semver"
)

// EscapePath returns the escaped form of the given module path
// (without the major version suffix).
// It fails if the module path is invalid.
func EscapePath(path string) (escaped string, err error) {
	if err := CheckPathWithoutVersion(path); err != nil {
		return "", err
	}
	// Technically there's no need to escape capital letters because CheckPath
	// doesn't allow them, but let's be defensive.
	return escapeString(path)
}

// EscapeVersion returns the escaped form of the given module version.
// Versions must be in (possibly non-canonical) semver form and must be valid file names
// and not contain exclamation marks.
func EscapeVersion(v string) (escaped string, err error) {
	if !semver.IsValid(v) {
		return "", &InvalidVersionError{
			Version: v,
			Err:     fmt.Errorf("version is not in semver syntax"),
		}
	}
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
