// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package module defines the [Version] type along with support code.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
//
// The [Version] type holds a pair of module path and version.
// The module path conforms to the checks implemented by [Check].
//
// # Escaped Paths
//
// Module versions appear as substrings of file system paths (as stored by
// the modcache package).
// In general we cannot rely on file systems to be case-sensitive. Although
// module paths cannot currently contain upper case characters because
// OCI registries forbid that, versions can. That
// is, we cannot rely on the file system to keep foo.com/v@v1.0.0-PRE and
// foo.com/v@v1.0.0-PRE separate. Windows and macOS don't. Instead, we must
// never require two different casings of a file path.
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
//	foo.com/v@v1.0.0-PRE ->  foo.com/v@v1.0.0-!p!r!e
//
// Versions that avoid upper-case letters are left unchanged.
// Note that because import paths are ASCII-only and avoid various
// problematic punctuation (like : < and >), the escaped form is also ASCII-only
// and avoids the same problematic punctuation.
//
// Neither versions nor module paths allow exclamation marks, so there is no
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
// This file essentially defines the set of valid import paths for the cue command.
// There are many subtle considerations, including Unicode ambiguity,
// security, network, and file system representations.

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/semver"
)

// A Version (for clients, a module.Version) is defined by a module path and version pair.
// These are stored in their plain (unescaped) form.
// This type is comparable.
type Version struct {
	path    string
	version string
}

// Path returns the module path part of the Version,
// which always includes the major version suffix
// unless a module path, like "github.com/foo/bar@v0".
// Note that in general the path should include the major version suffix
// even though it's implied from the version. The Canonical
// method can be used to add the major version suffix if not present.
// The BasePath method can be used to obtain the path without
// the suffix.
func (m Version) Path() string {
	return m.path
}

// Equal reports whether m is equal to m1.
func (m Version) Equal(m1 Version) bool {
	return m.path == m1.path && m.version == m1.version
}

func (m Version) Compare(m1 Version) int {
	if c := cmp.Compare(m.path, m1.path); c != 0 {
		return c
	}
	// To help go.sum formatting, allow version/file.
	// Compare semver prefix by semver rules,
	// file by string order.
	va, fa, _ := strings.Cut(m.version, "/")
	vb, fb, _ := strings.Cut(m1.version, "/")
	if c := semver.Compare(va, vb); c != 0 {
		return c
	}
	return cmp.Compare(fa, fb)
}

// BasePath returns the path part of m without its major version suffix.
func (m Version) BasePath() string {
	if m.IsLocal() {
		return m.path
	}
	basePath, _, ok := ast.SplitPackageVersion(m.path)
	if !ok {
		panic(fmt.Errorf("broken invariant: failed to split version in %q", m.path))
	}
	return basePath
}

// Version returns the version part of m. This is either
// a canonical semver version or "none" or the empty string.
func (m Version) Version() string {
	return m.version
}

// IsValid reports whether m is non-zero.
func (m Version) IsValid() bool {
	return m.path != ""
}

// IsCanonical reports whether m is valid and has a canonical
// semver version.
func (m Version) IsCanonical() bool {
	return m.IsValid() && m.version != "" && m.version != "none"
}

func (m Version) IsLocal() bool {
	return m.path == "local"
}

// String returns the string form of the Version:
// (Path@Version, or just Path if Version is empty).
func (m Version) String() string {
	if m.version == "" {
		return m.path
	}
	return m.BasePath() + "@" + m.version
}

func MustParseVersion(s string) Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

// ParseVersion parses a $module@$version
// string into a Version.
// The version must be canonical (i.e. it can't be
// just a major version).
func ParseVersion(s string) (Version, error) {
	basePath, vers, ok := ast.SplitPackageVersion(s)
	if !ok {
		return Version{}, fmt.Errorf("invalid module path@version %q", s)
	}
	if semver.Canonical(vers) != vers {
		return Version{}, fmt.Errorf("module version in %q is not canonical", s)
	}
	return Version{basePath + "@" + semver.Major(vers), vers}, nil
}

func MustNewVersion(path string, version string) Version {
	v, err := NewVersion(path, version)
	if err != nil {
		panic(err)
	}
	return v
}

// NewVersion forms a Version from the given path and version.
// The version must be canonical, empty or "none".
// If the path doesn't have a major version suffix, one will be added
// if the version isn't empty; if the version is empty, it's an error.
//
// As a special case, the path "local" is used to mean all packages
// held in the gen, pkg and usr directories.
func NewVersion(path string, version string) (Version, error) {
	switch {
	case path == "local":
		if version != "" {
			return Version{}, fmt.Errorf("module 'local' cannot have version")
		}
	case version != "" && version != "none":
		if !semver.IsValid(version) {
			return Version{}, fmt.Errorf("version %q (of module %q) is not well formed", version, path)
		}
		if semver.Canonical(version) != version {
			return Version{}, fmt.Errorf("version %q (of module %q) is not canonical", version, path)
		}
		maj := semver.Major(version)
		_, vmaj, ok := ast.SplitPackageVersion(path)
		if ok && maj != vmaj {
			return Version{}, fmt.Errorf("mismatched major version suffix in %q (version %v)", path, version)
		}
		if !ok {
			fullPath := path + "@" + maj
			if _, _, ok := ast.SplitPackageVersion(fullPath); !ok {
				return Version{}, fmt.Errorf("cannot form version path from %q, version %v", path, version)
			}
			path = fullPath
		}
	default:
		base, _, ok := ast.SplitPackageVersion(path)
		if !ok {
			return Version{}, fmt.Errorf("path %q has no major version", path)
		}
		if base == "local" {
			return Version{}, fmt.Errorf("module 'local' cannot have version")
		}
	}
	if version == "" {
		if err := CheckPath(path); err != nil {
			return Version{}, err
		}
	} else {
		if err := Check(path, version); err != nil {
			return Version{}, err
		}
	}
	return Version{
		path:    path,
		version: version,
	}, nil
}

// Sort sorts the list by Path, breaking ties by comparing Version fields.
// The Version fields are interpreted as semantic versions (using semver.Compare)
// optionally followed by a tie-breaking suffix introduced by a slash character,
// like in "v0.0.1/module.cue".
//
// Deprecated: use [slices.SortFunc] with [Version.Compare].
//
//go:fix inline
func Sort(list []Version) {
	slices.SortFunc(list, Version.Compare)
}
