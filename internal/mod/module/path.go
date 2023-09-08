//go:build ignore

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

// Check checks that a given module path, version pair is valid.
// In addition to the path being a valid module path
// and the version being a valid semantic version,
// the two must correspond.
// For example, the path "yaml/v2" only corresponds to
// semantic versions beginning with "v2.".
func Check(path, version string) error {
	if err := CheckPath(path); err != nil {
		return err
	}
	if !semver.IsValid(version) {
		return &ModuleError{
			Path: path,
			Err:  &InvalidVersionError{Version: version, Err: errors.New("not a semantic version")},
		}
	}
	_, pathMajor, _ := SplitPathVersion(path)
	if err := CheckPathMajor(version, pathMajor); err != nil {
		return &ModuleError{Path: path, Err: err}
	}
	return nil
}

// firstPathOK reports whether r can appear in the first element of a module path.
// The first element of the path must be an LDH domain name, at least for now.
// To avoid case ambiguity, the domain name must be entirely lower case.
func firstPathOK(r rune) bool {
	return r == '-' || r == '.' ||
		'0' <= r && r <= '9' ||
		'a' <= r && r <= 'z'
}

// modPathOK reports whether r can appear in a module path element.
// Paths can be ASCII letters, ASCII digits, and limited ASCII punctuation: - . _ and ~.
//
// This matches what "go get" has historically recognized in import paths,
// and avoids confusing sequences like '%20' or '+' that would change meaning
// if used in a URL.
//
// TODO(rsc): We would like to allow Unicode letters, but that requires additional
// care in the safe encoding (see "escaped paths" above).
func modPathOK(r rune) bool {
	if r < utf8.RuneSelf {
		return r == '-' || r == '.' || r == '_' || r == '~' ||
			'0' <= r && r <= '9' ||
			'A' <= r && r <= 'Z' ||
			'a' <= r && r <= 'z'
	}
	return false
}

// importPathOK reports whether r can appear in a package import path element.
//
// Import paths are intermediate between module paths and file paths: we allow
// disallow characters that would be confusing or ambiguous as arguments to
// 'go get' (such as '@' and ' ' ), but allow certain characters that are
// otherwise-unambiguous on the command line and historically used for some
// binary names (such as '++' as a suffix for compiler binaries and wrappers).
func importPathOK(r rune) bool {
	return modPathOK(r) || r == '+'
}

// fileNameOK reports whether r can appear in a file name.
// For now we allow all Unicode letters but otherwise limit to pathOK plus a few more punctuation characters.
// If we expand the set of allowed characters here, we have to
// work harder at detecting potential case-folding and normalization collisions.
// See note about "escaped paths" above.
func fileNameOK(r rune) bool {
	if r < utf8.RuneSelf {
		// Entire set of ASCII punctuation, from which we remove characters:
		//     ! " # $ % & ' ( ) * + , - . / : ; < = > ? @ [ \ ] ^ _ ` { | } ~
		// We disallow some shell special characters: " ' * < > ? ` |
		// (Note that some of those are disallowed by the Windows file system as well.)
		// We also disallow path separators / : and \ (fileNameOK is only called on path element characters).
		// We allow spaces (U+0020) in file names.
		const allowed = "!#$%&()+,-.=@[]^_{}~ "
		if '0' <= r && r <= '9' || 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' {
			return true
		}
		return strings.ContainsRune(allowed, r)
	}
	// It may be OK to add more ASCII punctuation here, but only carefully.
	// For example Windows disallows < > \, and macOS disallows :, so we must not allow those.
	return unicode.IsLetter(r)
}

// CheckPath checks that a module path is valid.
// A valid module path is a valid import path, as checked by CheckImportPath,
// with three additional constraints.
// First, the leading path element (up to the first slash, if any),
// by convention a domain name, must contain only lower-case ASCII letters,
// ASCII digits, dots (U+002E), and dashes (U+002D);
// it must contain at least one dot and cannot start with a dash.
// Second, for a final path element of the form /vN, where N looks numeric
// (ASCII digits and dots) must not begin with a leading zero, must not be /v1,
// and must not contain any dots. For paths beginning with "gopkg.in/",
// this second requirement is replaced by a requirement that the path
// follow the gopkg.in server's conventions.
// Third, no path element may begin with a dot.
func CheckPath(path string) (err error) {
	defer func() {
		if err != nil {
			err = &InvalidPathError{Kind: "module", Path: path, Err: err}
		}
	}()

	if err := checkPath(path, modulePath); err != nil {
		return err
	}
	i := strings.Index(path, "/")
	if i < 0 {
		i = len(path)
	}
	if i == 0 {
		return fmt.Errorf("leading slash")
	}
	if !strings.Contains(path[:i], ".") {
		return fmt.Errorf("missing dot in first path element")
	}
	if path[0] == '-' {
		return fmt.Errorf("leading dash in first path element")
	}
	for _, r := range path[:i] {
		if !firstPathOK(r) {
			return fmt.Errorf("invalid char %q in first path element", r)
		}
	}
	if _, _, ok := SplitPathVersion(path); !ok {
		return fmt.Errorf("invalid version")
	}
	return nil
}

// CheckImportPath checks that an import path is valid.
//
// A valid import path consists of one or more valid path elements
// separated by slashes (U+002F). (It must not begin with nor end in a slash.)
//
// A valid path element is a non-empty string made up of
// ASCII letters, ASCII digits, and limited ASCII punctuation: - . _ and ~.
// It must not end with a dot (U+002E), nor contain two dots in a row.
//
// The element prefix up to the first dot must not be a reserved file name
// on Windows, regardless of case (CON, com1, NuL, and so on). The element
// must not have a suffix of a tilde followed by one or more ASCII digits
// (to exclude paths elements that look like Windows short-names).
//
// CheckImportPath may be less restrictive in the future, but see the
// top-level package documentation for additional information about
// subtleties of Unicode.
func CheckImportPath(path string) error {
	if err := checkPath(path, importPath); err != nil {
		return &InvalidPathError{Kind: "import", Path: path, Err: err}
	}
	return nil
}

// pathKind indicates what kind of path we're checking. Module paths,
// import paths, and file paths have different restrictions.
type pathKind int

const (
	modulePath pathKind = iota
	importPath
	filePath
)

// checkPath checks that a general path is valid. kind indicates what
// specific constraints should be applied.
//
// checkPath returns an error describing why the path is not valid.
// Because these checks apply to module, import, and file paths,
// and because other checks may be applied, the caller is expected to wrap
// this error with InvalidPathError.
func checkPath(path string, kind pathKind) error {
	if !utf8.ValidString(path) {
		return fmt.Errorf("invalid UTF-8")
	}
	if path == "" {
		return fmt.Errorf("empty string")
	}
	if path[0] == '-' && kind != filePath {
		return fmt.Errorf("leading dash")
	}
	if strings.Contains(path, "//") {
		return fmt.Errorf("double slash")
	}
	if path[len(path)-1] == '/' {
		return fmt.Errorf("trailing slash")
	}
	elemStart := 0
	for i, r := range path {
		if r == '/' {
			if err := checkElem(path[elemStart:i], kind); err != nil {
				return err
			}
			elemStart = i + 1
		}
	}
	if err := checkElem(path[elemStart:], kind); err != nil {
		return err
	}
	return nil
}

// checkElem checks whether an individual path element is valid.
func checkElem(elem string, kind pathKind) error {
	if elem == "" {
		return fmt.Errorf("empty path element")
	}
	if strings.Count(elem, ".") == len(elem) {
		return fmt.Errorf("invalid path element %q", elem)
	}
	if elem[0] == '.' && kind == modulePath {
		return fmt.Errorf("leading dot in path element")
	}
	if elem[len(elem)-1] == '.' {
		return fmt.Errorf("trailing dot in path element")
	}
	for _, r := range elem {
		ok := false
		switch kind {
		case modulePath:
			ok = modPathOK(r)
		case importPath:
			ok = importPathOK(r)
		case filePath:
			ok = fileNameOK(r)
		default:
			panic(fmt.Sprintf("internal error: invalid kind %v", kind))
		}
		if !ok {
			return fmt.Errorf("invalid char %q", r)
		}
	}

	// Windows disallows a bunch of path elements, sadly.
	// See https://docs.microsoft.com/en-us/windows/desktop/fileio/naming-a-file
	short := elem
	if i := strings.Index(short, "."); i >= 0 {
		short = short[:i]
	}
	for _, bad := range badWindowsNames {
		if strings.EqualFold(bad, short) {
			return fmt.Errorf("%q disallowed as path element component on Windows", short)
		}
	}

	if kind == filePath {
		// don't check for Windows short-names in file names. They're
		// only an issue for import paths.
		return nil
	}

	// Reject path components that look like Windows short-names.
	// Those usually end in a tilde followed by one or more ASCII digits.
	if tilde := strings.LastIndexByte(short, '~'); tilde >= 0 && tilde < len(short)-1 {
		suffix := short[tilde+1:]
		suffixIsDigits := true
		for _, r := range suffix {
			if r < '0' || r > '9' {
				suffixIsDigits = false
				break
			}
		}
		if suffixIsDigits {
			return fmt.Errorf("trailing tilde and digits in path element")
		}
	}

	return nil
}

// CheckFilePath checks that a slash-separated file path is valid.
// The definition of a valid file path is the same as the definition
// of a valid import path except that the set of allowed characters is larger:
// all Unicode letters, ASCII digits, the ASCII space character (U+0020),
// and the ASCII punctuation characters
// “!#$%&()+,-.=@[]^_{}~”.
// (The excluded punctuation characters, " * < > ? ` ' | / \ and :,
// have special meanings in certain shells or operating systems.)
//
// CheckFilePath may be less restrictive in the future, but see the
// top-level package documentation for additional information about
// subtleties of Unicode.
func CheckFilePath(path string) error {
	if err := checkPath(path, filePath); err != nil {
		return &InvalidPathError{Kind: "file", Path: path, Err: err}
	}
	return nil
}

// badWindowsNames are the reserved file path elements on Windows.
// See https://docs.microsoft.com/en-us/windows/desktop/fileio/naming-a-file
var badWindowsNames = []string{
	"CON",
	"PRN",
	"AUX",
	"NUL",
	"COM1",
	"COM2",
	"COM3",
	"COM4",
	"COM5",
	"COM6",
	"COM7",
	"COM8",
	"COM9",
	"LPT1",
	"LPT2",
	"LPT3",
	"LPT4",
	"LPT5",
	"LPT6",
	"LPT7",
	"LPT8",
	"LPT9",
}

// SplitPathVersion returns prefix and major version such that prefix+pathMajor == path
// and version is either empty or "/vN" for N >= 2.
// As a special case, gopkg.in paths are recognized directly;
// they require ".vN" instead of "/vN", and for all N, not just N >= 2.
// SplitPathVersion returns with ok = false when presented with
// a path whose last path element does not satisfy the constraints
// applied by CheckPath, such as "example.com/pkg/v1" or "example.com/pkg/v1.2".
func SplitPathVersion(path string) (prefix, pathMajor string, ok bool) {
	if strings.HasPrefix(path, "gopkg.in/") {
		return splitGopkgIn(path)
	}

	i := len(path)
	dot := false
	for i > 0 && ('0' <= path[i-1] && path[i-1] <= '9' || path[i-1] == '.') {
		if path[i-1] == '.' {
			dot = true
		}
		i--
	}
	if i <= 1 || i == len(path) || path[i-1] != 'v' || path[i-2] != '/' {
		return path, "", true
	}
	prefix, pathMajor = path[:i-2], path[i-2:]
	if dot || len(pathMajor) <= 2 || pathMajor[2] == '0' || pathMajor == "/v1" {
		return path, "", false
	}
	return prefix, pathMajor, true
}

// splitGopkgIn is like SplitPathVersion but only for gopkg.in paths.
func splitGopkgIn(path string) (prefix, pathMajor string, ok bool) {
	if !strings.HasPrefix(path, "gopkg.in/") {
		return path, "", false
	}
	i := len(path)
	if strings.HasSuffix(path, "-unstable") {
		i -= len("-unstable")
	}
	for i > 0 && ('0' <= path[i-1] && path[i-1] <= '9') {
		i--
	}
	if i <= 1 || path[i-1] != 'v' || path[i-2] != '.' {
		// All gopkg.in paths must end in vN for some N.
		return path, "", false
	}
	prefix, pathMajor = path[:i-2], path[i-2:]
	if len(pathMajor) <= 2 || pathMajor[2] == '0' && pathMajor != ".v0" {
		return path, "", false
	}
	return prefix, pathMajor, true
}

// MatchPathMajor reports whether the semantic version v
// matches the path major version pathMajor.
//
// MatchPathMajor returns true if and only if CheckPathMajor returns nil.
func MatchPathMajor(v, pathMajor string) bool {
	return CheckPathMajor(v, pathMajor) == nil
}

// CheckPathMajor returns a non-nil error if the semantic version v
// does not match the path major version pathMajor.
func CheckPathMajor(v, pathMajor string) error {
	// TODO(jayconrod): return errors or panic for invalid inputs. This function
	// (and others) was covered by integration tests for cmd/go, and surrounding
	// code protected against invalid inputs like non-canonical versions.
	if strings.HasPrefix(pathMajor, ".v") && strings.HasSuffix(pathMajor, "-unstable") {
		pathMajor = strings.TrimSuffix(pathMajor, "-unstable")
	}
	if strings.HasPrefix(v, "v0.0.0-") && pathMajor == ".v1" {
		// Allow old bug in pseudo-versions that generated v0.0.0- pseudoversion for gopkg .v1.
		// For example, gopkg.in/yaml.v2@v2.2.1's go.mod requires gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405.
		return nil
	}
	m := semver.Major(v)
	if pathMajor == "" {
		if m == "v0" || m == "v1" || semver.Build(v) == "+incompatible" {
			return nil
		}
		pathMajor = "v0 or v1"
	} else if pathMajor[0] == '/' || pathMajor[0] == '.' {
		if m == pathMajor[1:] {
			return nil
		}
		pathMajor = pathMajor[1:]
	}
	return &InvalidVersionError{
		Version: v,
		Err:     fmt.Errorf("should be %s, not %s", pathMajor, semver.Major(v)),
	}
}

// PathMajorPrefix returns the major-version tag prefix implied by pathMajor.
// An empty PathMajorPrefix allows either v0 or v1.
//
// Note that MatchPathMajor may accept some versions that do not actually begin
// with this prefix: namely, it accepts a 'v0.0.0-' prefix for a '.v1'
// pathMajor, even though that pathMajor implies 'v1' tagging.
func PathMajorPrefix(pathMajor string) string {
	if pathMajor == "" {
		return ""
	}
	if pathMajor[0] != '/' && pathMajor[0] != '.' {
		panic("pathMajor suffix " + pathMajor + " passed to PathMajorPrefix lacks separator")
	}
	if strings.HasPrefix(pathMajor, ".v") && strings.HasSuffix(pathMajor, "-unstable") {
		pathMajor = strings.TrimSuffix(pathMajor, "-unstable")
	}
	m := pathMajor[1:]
	if m != semver.Major(m) {
		panic("pathMajor suffix " + pathMajor + "passed to PathMajorPrefix is not a valid major version")
	}
	return m
}

// EscapePath returns the escaped form of the given module path.
// It fails if the module path is invalid.
func EscapePath(path string) (escaped string, err error) {
	if err := CheckPath(path); err != nil {
		return "", err
	}

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

// MatchPrefixPatterns reports whether any path prefix of target matches one of
// the glob patterns (as defined by path.Match) in the comma-separated globs
// list. This implements the algorithm used when matching a module path to the
// GOPRIVATE environment variable, as described by 'go help module-private'.
//
// It ignores any empty or malformed patterns in the list.
// Trailing slashes on patterns are ignored.
func MatchPrefixPatterns(globs, target string) bool {
	for globs != "" {
		// Extract next non-empty glob in comma-separated list.
		var glob string
		if i := strings.Index(globs, ","); i >= 0 {
			glob, globs = globs[:i], globs[i+1:]
		} else {
			glob, globs = globs, ""
		}
		glob = strings.TrimSuffix(glob, "/")
		if glob == "" {
			continue
		}

		// A glob with N+1 path elements (N slashes) needs to be matched
		// against the first N+1 path elements of target,
		// which end just before the N+1'th slash.
		n := strings.Count(glob, "/")
		prefix := target
		// Walk target, counting slashes, truncating at the N+1'th slash.
		for i := 0; i < len(target); i++ {
			if target[i] == '/' {
				if n == 0 {
					prefix = target[:i]
					break
				}
				n--
			}
		}
		if n > 0 {
			// Not enough prefix elements.
			continue
		}
		matched, _ := path.Match(glob, prefix)
		if matched {
			return true
		}
	}
	return false
}
