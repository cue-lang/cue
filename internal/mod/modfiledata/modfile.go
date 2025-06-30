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
	if mainPath != "" {
		// The main module is always the default for its own major version.
		defaultMajorVersions[mainPath] = mainMajor
	}
	// Check that major versions match dependency versions.
	for m, dep := range mf.Deps {
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
	mf.versions = versions[:len(versions):len(versions)]
	slices.SortFunc(mf.versions, module.Version.Compare)
	mf.versionByModule = versionByModule
	mf.defaultMajorVersions = defaultMajorVersions
	return nil
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
