// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package modfiledata holds the underlying module.cue file
// representation. It is separate from the [cuelang.org/go/mod/modfile]
// package to allow the type to be used without incurring the
// dependency on the CUE evaluator brought in by the
// [modfile.Parse] and [modfile.Format] functions.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package modfiledata

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/module"
)

// TODO merge this back into [cuelang.org/go/mod/modfile] when
// we can remove the evaluator dependency from that package.

// Note that [File] and the types that it depends on are considered
// to be public types even though they are defined in an internal
// package because they are aliased in the [cuelang.org/go/mod/modfile]
// package.

// File represents the contents of a cue.mod/module.cue file.
// Use [cuelang.org/go/mod/modfile.Parse] to parse the file in
// its standard format.
type File struct {
	// Module holds the module path, which may
	// not contain a major version suffix.
	// Use the [File.QualifiedModule] method to obtain a module
	// path that's always qualified. See also the
	// [File.ModulePath] and [File.MajorVersion] methods.
	Module          string                    `json:"module"`
	Language        *Language                 `json:"language,omitempty"`
	Source          *Source                   `json:"source,omitempty"`
	Deps            map[string]*Dep           `json:"deps,omitempty"`
	Custom          map[string]map[string]any `json:"custom,omitempty"`
	versions        []module.Version
	versionByModule map[string]module.Version

	// defaultMajorVersions maps from module base path (the path
	// without its major version) to the major version default for that path.
	defaultMajorVersions map[string]string

	// replacements maps from module path (with major version) to its replacement.
	replacements map[string]Replacement
}

// QualifiedModule returns the fully qualified module path
// if there is one. It returns the empty string when [ParseLegacy]
// has been used and the module field is empty.
//
// Note that when the module field does not contain a major
// version suffix, "@v0" is assumed.
func (f *File) QualifiedModule() string {
	if strings.Contains(f.Module, "@") {
		return f.Module
	}
	if f.Module == "" {
		return ""
	}
	return f.Module + "@v0"
}

// Deprecated: this method is misnamed; use [File.ModuleRootPath]
// instead.
//
//go:fix inline
func (f *File) ModulePath() string {
	return f.ModuleRootPath()
}

// ModuleRootPath returns the path part of the module without
// its major version suffix.
func (f *File) ModuleRootPath() string {
	path, _, _ := ast.SplitPackageVersion(f.QualifiedModule())
	return path
}

// Source represents how to transform from a module's
// source to its actual contents.
type Source struct {
	Kind string `json:"kind"`
}

// Validate checks that src is well formed.
func (src *Source) Validate() error {
	switch src.Kind {
	case "git", "self":
		return nil
	}
	return fmt.Errorf("unrecognized source kind %q", src.Kind)
}

type Language struct {
	Version string `json:"version,omitempty"`
}

type Dep struct {
	Version string `json:"v"`
	Default bool   `json:"default,omitempty"`
	Replace string `json:"replace,omitempty"`
}

// Replacement represents a processed replace directive.
// Either New or LocalPath will be set, but not both.
type Replacement struct {
	// Old is the module being replaced.
	Old module.Version
	// New is the replacement module version (for remote replacements).
	New module.Version
	// LocalPath is set for local path replacements (starts with ./ or ../).
	LocalPath string
}

// Init initializes the private dependency-related fields of f from
// the public fields.
func (f *File) Init() error {
	return f.init(true)
}

// InitNonStrict is like [File.Init] but does not enforce full strictness
// in dependencies (for example, it allows dependency module paths
// without major version suffixes).
func (f *File) InitNonStrict() error {
	return f.init(false)
}

func (mf *File) init(strict bool) error {
	mainPath, mainMajor, ok := ast.SplitPackageVersion(mf.Module)
	if ok {
		if semver.Major(mainMajor) != mainMajor {
			return fmt.Errorf("module path %s should contain the major version only", mf.Module)
		}
	} else if mainPath != "" {
		if err := module.CheckPathWithoutVersion(mainPath); err != nil {
			return fmt.Errorf("module path %q is not valid: %v", mainPath, err)
		}
		// There's no main module major version: default to v0.
		mainMajor = "v0"
	} else {
		return fmt.Errorf("empty module path")
	}
	if mf.Language != nil {
		vers := mf.Language.Version
		if !semver.IsValid(vers) {
			return fmt.Errorf("language version %q is not well formed", vers)
		}
		if semver.Canonical(vers) != vers {
			return fmt.Errorf("language version %v is not canonical", vers)
		}
	}
	versionByModule := make(map[string]module.Version)
	var versions []module.Version
	defaultMajorVersions := make(map[string]string)
	replacements := make(map[string]Replacement)
	if mainPath != "" {
		// The main module is always the default for its own major version.
		defaultMajorVersions[mainPath] = mainMajor
	}
	// Check that major versions match dependency versions.
	for m, dep := range mf.Deps {
		// Handle replace directives
		if dep.Replace != "" {
			repl, err := parseReplacement(m, dep.Replace, strict)
			if err != nil {
				return err
			}
			replacements[m] = repl
		}

		// If version is empty and there's a replace, this is a version-independent
		// replacement - we don't add it to the versions list but still track the replacement.
		if dep.Version == "" {
			if dep.Replace == "" {
				return fmt.Errorf("module %q has no version and no replacement", m)
			}
			// Version-independent replacement - don't add to versions list
			continue
		}

		vers, err := module.NewVersion(m, dep.Version)
		if err != nil {
			return fmt.Errorf("cannot make version from module %q, version %q: %v", m, dep.Version, err)
		}
		versions = append(versions, vers)
		if strict && vers.Path() != m {
			return fmt.Errorf("no major version in %q", m)
		}
		if dep.Default {
			mp := vers.BasePath()
			if _, ok := defaultMajorVersions[mp]; ok {
				return fmt.Errorf("multiple default major versions found for %v", mp)
			}
			defaultMajorVersions[mp] = semver.Major(vers.Version())
		}
		versionByModule[vers.Path()] = vers
	}
	if mainPath != "" {
		// We don't necessarily have a full version for the main module.
		mainWithMajor := mainPath + "@" + mainMajor
		mainVersion, err := module.NewVersion(mainWithMajor, "")
		if err != nil {
			return err
		}
		versionByModule[mainWithMajor] = mainVersion
	}
	if len(defaultMajorVersions) == 0 {
		defaultMajorVersions = nil
	}
	if len(replacements) == 0 {
		replacements = nil
	}
	mf.versions = versions[:len(versions):len(versions)]
	slices.SortFunc(mf.versions, module.Version.Compare)
	mf.versionByModule = versionByModule
	mf.defaultMajorVersions = defaultMajorVersions
	mf.replacements = replacements
	return nil
}

// parseReplacement parses a replace directive value and returns a Replacement.
// The replace value can be either:
// - A local file path starting with "./" or "../"
// - A remote module path with version (e.g., "other.com/bar@v1.0.0")
func parseReplacement(oldPath, replace string, strict bool) (Replacement, error) {
	isLocal := strings.HasPrefix(replace, "./") || strings.HasPrefix(replace, "../")

	// Reject absolute paths - must use relative paths starting with ./ or ../
	if isAbsolutePath(replace) {
		return Replacement{}, fmt.Errorf("absolute path replacement %q not allowed; use relative path starting with ./ or ../", replace)
	}

	if strict && isLocal {
		return Replacement{}, fmt.Errorf("local path replacement %q not allowed in strict mode", replace)
	}

	// Parse the old module path to create a module.Version. We use an empty
	// version string because the replacement applies to all versions of the
	// module (unlike Go, CUE replacements don't support version-specific replacements).
	oldVers, err := module.NewVersion(oldPath, "")
	if err != nil {
		return Replacement{}, fmt.Errorf("invalid module path %q in replace directive: %v", oldPath, err)
	}

	repl := Replacement{
		Old: oldVers,
	}

	if isLocal {
		repl.LocalPath = replace
	} else {
		// Parse as module@version
		newVers, err := module.ParseVersion(replace)
		if err != nil {
			return Replacement{}, fmt.Errorf("invalid replacement %q: must be local path (./... or ../...) or module@version: %v", replace, err)
		}
		repl.New = newVers
	}

	return repl, nil
}

// MajorVersion returns the major version of the module,
// not including the "@".
// If there is no module (which can happen when [ParseLegacy]
// is used or if Module is explicitly set to an empty string),
// it returns the empty string.
func (f *File) MajorVersion() string {
	_, vers, _ := ast.SplitPackageVersion(f.QualifiedModule())
	return vers
}

// DepVersions returns the versions of all the modules depended on by the
// file. The caller should not modify the returned slice.
//
// This always returns the same value, even if the contents
// of f are changed. If f was not created with [Parse], it returns nil.
func (f *File) DepVersions() []module.Version {
	return slices.Clip(f.versions)
}

// DefaultMajorVersions returns a map from module base path
// to the major version that's specified as the default for that module.
// The caller should not modify the returned map.
func (f *File) DefaultMajorVersions() map[string]string {
	return f.defaultMajorVersions
}

// Replacements returns the map of module replacements.
// The map is keyed by module path (with major version, e.g., "foo.com/bar@v0").
// The caller should not modify the returned map.
func (f *File) Replacements() map[string]Replacement {
	return f.replacements
}

// ModuleForImportPath returns the module that should contain the given
// import path and reports whether the module was found.
// It does not check to see if the import path actually exists within the module.
//
// It works entirely from information in f, meaning that it does
// not consult a registry to resolve a package whose module is not
// mentioned in the file, which means it will not work in general unless
// the module is tidy (as with `cue mod tidy`).
func (f *File) ModuleForImportPath(importPath string) (module.Version, bool) {
	ip := ast.ParseImportPath(importPath)
	for prefix := ip.Path; prefix != "."; prefix = path.Dir(prefix) {
		pkgVersion := ip.Version
		if pkgVersion == "" {
			if pkgVersion = f.defaultMajorVersions[prefix]; pkgVersion == "" {
				continue
			}
		}
		if mv, ok := f.versionByModule[prefix+"@"+pkgVersion]; ok {
			return mv, true
		}
	}
	return module.Version{}, false
}

// isAbsolutePath reports whether the given path is an absolute path
// on any supported platform (Unix or Windows).
func isAbsolutePath(p string) bool {
	return strings.HasPrefix(p, "/") || isWindowsAbs(p)
}

func isWindowsAbs(path string) bool {
	if isReservedWindowsName(path) {
		return true
	}
	volLen := windowsVolumeNameLen(path)
	if volLen == 0 {
		return false
	}
	if len(path) == volLen {
		// UNC roots like \\server\share are absolute.
		return len(path) >= 2 && isSlash(path[0]) && isSlash(path[1])
	}
	path = path[volLen:]
	return isSlash(path[0])
}

func isSlash(c byte) bool {
	return c == '\\' || c == '/'
}

var reservedWindowsNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
}

func isReservedWindowsName(path string) bool {
	if len(path) == 0 {
		return false
	}
	for _, reserved := range reservedWindowsNames {
		if strings.EqualFold(path, reserved) {
			return true
		}
	}
	return false
}

// windowsVolumeNameLen returns length of the leading volume name on Windows.
func windowsVolumeNameLen(path string) int {
	if len(path) < 2 {
		return 0
	}
	// with drive letter
	c := path[0]
	if path[1] == ':' && (('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')) {
		return 2
	}
	// UNC path
	if l := len(path); l >= 5 && isSlash(path[0]) && isSlash(path[1]) &&
		!isSlash(path[2]) && path[2] != '.' {
		// leading \\ then server name
		for n := 3; n < l-1; n++ {
			if isSlash(path[n]) {
				n++
				// share name
				if !isSlash(path[n]) {
					if path[n] == '.' {
						break
					}
					for ; n < l; n++ {
						if isSlash(path[n]) {
							break
						}
					}
					return n
				}
				break
			}
		}
	}
	return 0
}
