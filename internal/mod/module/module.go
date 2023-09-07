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
package module

// IMPORTANT NOTE
//
// This file essentially defines the set of valid import paths for the cue command.
// There are many subtle considerations, including Unicode ambiguity,
// security, network, and file system representations.

import (
	"fmt"
	"sort"
	"strings"

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

func (m Version) Equal(m1 Version) bool {
	return m.path == m1.path && m.version == m1.version
}

func (m Version) BasePath() string {
	basePath, _, ok := SplitPathVersion(m.path)
	if !ok {
		panic(fmt.Errorf("broken invariant: failed to split version in %q", m.path))
	}
	return basePath
}

func (m Version) Version() string {
	return m.version
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
	basePath, vers, ok := SplitPathVersion(s)
	if !ok {
		return Version{}, fmt.Errorf("invalid module path@version %q", s)
	}
	if semver.Canonical(vers) != vers {
		return Version{}, fmt.Errorf("module version in %q is not canonical", s)
	}
	return Version{basePath + "@" + semver.Major(vers), vers}, nil
}

func MustNewVersion(path string, vers string) Version {
	v, err := NewVersion(path, vers)
	if err != nil {
		panic(err)
	}
	return v
}

// NewVersion forms a Version from the given path and version.
// The version must be canonical, empty or "none".
// If the path doesn't have a major version suffix, one will be added
// if the version isn't empty; if the version is empty, it's an error.
func NewVersion(path string, vers string) (Version, error) {
	if vers != "" && vers != "none" {
		if !semver.IsValid(vers) {
			return Version{}, fmt.Errorf("version %q (of module %q) is not well formed", vers, path)
		}
		if semver.Canonical(vers) != vers {
			return Version{}, fmt.Errorf("version %q (of module %q) is not canonical", vers, path)
		}
		maj := semver.Major(vers)
		_, vmaj, ok := SplitPathVersion(path)
		if ok && maj != vmaj {
			return Version{}, fmt.Errorf("mismatched major version suffix in %q (version %v)", path, vers)
		}
		if !ok {
			fullPath := path + "@" + maj
			if _, _, ok := SplitPathVersion(fullPath); !ok {
				return Version{}, fmt.Errorf("cannot form version path from %q, version %v", path, vers)
			}
			path = fullPath
		}
	} else {
		if _, _, ok := SplitPathVersion(path); !ok {
			return Version{}, fmt.Errorf("path %q has no major version", path)
		}
	}
	if vers == "" {
		if err := CheckPath(path); err != nil {
			return Version{}, err
		}
	} else {
		if err := Check(path, vers); err != nil {
			return Version{}, err
		}
	}
	return Version{
		path:    path,
		version: vers,
	}, nil
}

// Sort sorts the list by Path, breaking ties by comparing Version fields.
// The Version fields are interpreted as semantic versions (using semver.Compare)
// optionally followed by a tie-breaking suffix introduced by a slash character,
// like in "v0.0.1/module.cue".
func Sort(list []Version) {
	sort.Slice(list, func(i, j int) bool {
		mi := list[i]
		mj := list[j]
		if mi.path != mj.path {
			return mi.path < mj.path
		}
		// To help go.sum formatting, allow version/file.
		// Compare semver prefix by semver rules,
		// file by string order.
		vi := mi.version
		vj := mj.version
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
