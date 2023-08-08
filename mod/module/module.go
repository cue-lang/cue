//go:build ignore

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package module defines the module.Version type along with support code.
//
// The module.Version type is a simple Path, Version pair:
//
//	type Version struct {
//		Path string
//		Version string
//	}
//
// There are no restrictions imposed directly by use of this structure,
// but additional checking functions, most notably Check, verify that
// a particular path, version pair is valid.
//
// # Escaped Paths
//
// Module paths appear as substrings of file system paths
// (in the download cache) and of web server URLs in the proxy protocol.
// In general we cannot rely on file systems to be case-sensitive,
// nor can we rely on web servers, since they read from file systems.
// That is, we cannot rely on the file system to keep rsc.io/QUOTE
// and rsc.io/quote separate. Windows and macOS don't.
// Instead, we must never require two different casings of a file path.
// Because we want the download cache to match the proxy protocol,
// and because we want the proxy protocol to be possible to serve
// from a tree of static files (which might be stored on a case-insensitive
// file system), the proxy protocol must never require two different casings
// of a URL path either.
//
// One possibility would be to make the escaped form be the lowercase
// hexadecimal encoding of the actual path bytes. This would avoid ever
// needing different casings of a file path, but it would be fairly illegible
// to most programmers when those paths appeared in the file system
// (including in file paths in compiler errors and stack traces)
// in web server logs, and so on. Instead, we want a safe escaped form that
// leaves most paths unaltered.
//
// The safe escaped form is to replace every uppercase letter
// with an exclamation mark followed by the letter's lowercase equivalent.
//
// For example,
//
//	github.com/Azure/azure-sdk-for-go ->  github.com/!azure/azure-sdk-for-go.
//	github.com/GoogleCloudPlatform/cloudsql-proxy -> github.com/!google!cloud!platform/cloudsql-proxy
//	github.com/Sirupsen/logrus -> github.com/!sirupsen/logrus.
//
// Import paths that avoid upper-case letters are left unchanged.
// Note that because import paths are ASCII-only and avoid various
// problematic punctuation (like : < and >), the escaped form is also ASCII-only
// and avoids the same problematic punctuation.
//
// Import paths have never allowed exclamation marks, so there is no
// need to define how to escape a literal !.
//
// # Unicode Restrictions
//
// Today, paths are disallowed from using Unicode.
//
// Although paths are currently disallowed from using Unicode,
// we would like at some point to allow Unicode letters as well, to assume that
// file systems and URLs are Unicode-safe (storing UTF-8), and apply
// the !-for-uppercase convention for escaping them in the file system.
// But there are at least two subtle considerations.
//
// First, note that not all case-fold equivalent distinct runes
// form an upper/lower pair.
// For example, U+004B ('K'), U+006B ('k'), and U+212A ('K' for Kelvin)
// are three distinct runes that case-fold to each other.
// When we do add Unicode letters, we must not assume that upper/lower
// are the only case-equivalent pairs.
// Perhaps the Kelvin symbol would be disallowed entirely, for example.
// Or perhaps it would escape as "!!k", or perhaps as "(212A)".
//
// Second, it would be nice to allow Unicode marks as well as letters,
// but marks include combining marks, and then we must deal not
// only with case folding but also normalization: both U+00E9 ('é')
// and U+0065 U+0301 ('e' followed by combining acute accent)
// look the same on the page and are treated by some file systems
// as the same path. If we do allow Unicode marks in paths, there
// must be some kind of normalization to allow only one canonical
// encoding of any character used in an import path.
package module

// IMPORTANT NOTE
//
// This file essentially defines the set of valid import paths for the go command.
// There are many subtle considerations, including Unicode ambiguity,
// security, network, and file system representations.
//
// This file also defines the set of valid module path and version combinations,
// another topic with many subtle considerations.
//
// Changes to the semantics in this file require approval from rsc.

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/semver"
)

// A Version (for clients, a module.Version) is defined by a module path and version pair.
// These are stored in their plain (unescaped) form.
type Version struct {
	// Path is a module path, like "golang.org/x/text" or "rsc.io/quote/v2".
	Path string

	// Version is usually a semantic version in canonical form.
	// There are three exceptions to this general rule.
	// First, the top-level target of a build has no specific version
	// and uses Version = "".
	// Second, during MVS calculations the version "none" is used
	// to represent the decision to take no version of a given module.
	// Third, filesystem paths found in "replace" directives are
	// represented by a path with an empty version.
	Version string `json:",omitempty"`
}

// String returns a representation of the Version suitable for logging
// (Path@Version, or just Path if Version is empty).
func (m Version) String() string {
	if m.Version == "" {
		return m.Path
	}
	return m.Path + "@" + m.Version
}

// A ModuleError indicates an error specific to a module.
type ModuleError struct {
	Path    string
	Version string
	Err     error
}

// VersionError returns a ModuleError derived from a Version and error,
// or err itself if it is already such an error.
func VersionError(v Version, err error) error {
	var mErr *ModuleError
	if errors.As(err, &mErr) && mErr.Path == v.Path && mErr.Version == v.Version {
		return err
	}
	return &ModuleError{
		Path:    v.Path,
		Version: v.Version,
		Err:     err,
	}
}

func (e *ModuleError) Error() string {
	if v, ok := e.Err.(*InvalidVersionError); ok {
		return fmt.Sprintf("%s@%s: invalid %s: %v", e.Path, v.Version, v.noun(), v.Err)
	}
	if e.Version != "" {
		return fmt.Sprintf("%s@%s: %v", e.Path, e.Version, e.Err)
	}
	return fmt.Sprintf("module %s: %v", e.Path, e.Err)
}

func (e *ModuleError) Unwrap() error { return e.Err }

// An InvalidVersionError indicates an error specific to a version, with the
// module path unknown or specified externally.
//
// A ModuleError may wrap an InvalidVersionError, but an InvalidVersionError
// must not wrap a ModuleError.
type InvalidVersionError struct {
	Version string
	Pseudo  bool
	Err     error
}

// noun returns either "version" or "pseudo-version", depending on whether
// e.Version is a pseudo-version.
func (e *InvalidVersionError) noun() string {
	if e.Pseudo {
		return "pseudo-version"
	}
	return "version"
}

func (e *InvalidVersionError) Error() string {
	return fmt.Sprintf("%s %q invalid: %s", e.noun(), e.Version, e.Err)
}

func (e *InvalidVersionError) Unwrap() error { return e.Err }

// An InvalidPathError indicates a module, import, or file path doesn't
// satisfy all naming constraints. See CheckPath, CheckImportPath,
// and CheckFilePath for specific restrictions.
type InvalidPathError struct {
	Kind string // "module", "import", or "file"
	Path string
	Err  error
}

func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("malformed %s path %q: %v", e.Kind, e.Path, e.Err)
}

func (e *InvalidPathError) Unwrap() error { return e.Err }

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

// CanonicalVersion returns the canonical form of the version string v.
// It is the same as semver.Canonical(v) except that it preserves the special build suffix "+incompatible".
func CanonicalVersion(v string) string {
	cv := semver.Canonical(v)
	if semver.Build(v) == "+incompatible" {
		cv += "+incompatible"
	}
	return cv
}

// Sort sorts the list by Path, breaking ties by comparing Version fields.
// The Version fields are interpreted as semantic versions (using semver.Compare)
// optionally followed by a tie-breaking suffix introduced by a slash character,
// like in "v0.0.1/go.mod".
func Sort(list []Version) {
	sort.Slice(list, func(i, j int) bool {
		mi := list[i]
		mj := list[j]
		if mi.Path != mj.Path {
			return mi.Path < mj.Path
		}
		// To help go.sum formatting, allow version/file.
		// Compare semver prefix by semver rules,
		// file by string order.
		vi := mi.Version
		vj := mj.Version
		var fi, fj string
		if k := strings.Index(vi, "/"); k >= 0 {
			vi, fi = vi[:k], vi[k:]
		}
		if k := strings.Index(vj, "/"); k >= 0 {
			vj, fj = vj[:k], vj[k:]
		}
		if vi != vj {
			return semver.Compare(vi, vj) < 0
		}
		return fi < fj
	})
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
