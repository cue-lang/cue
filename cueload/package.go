// Copyright 2026 The CUE Authors
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

package cueload

import (
	"context"
	"iter"
	"slices"

	"cuelang.org/go/cue/ast"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// A Package is an immutable loaded CUE package. A loader returns one
// canonical *Package per import path: spellings such as foo.com/bar,
// foo.com/bar@v0 and foo.com/bar@v0:bar yield the same Package.
type Package struct {
	loader *Loader
	ip     ast.ImportPath // canonical import path
	dir    string         // display directory of the source files
	mod    *Module        // nil for stdlib and resolver-provided packages
	files  []*SourceFile
	data   []File
	err    error // aggregated load and parse errors

	isStd  bool                // standard library package
	fixed  *cue.Value          // NewValuePackage: the fixed value
	modpkg *modpkgload.Package // module-system package, if any

	// importSpellings maps each import path spelling occurring in the
	// package's files to the package it resolves to (nil when
	// unresolved), and imports holds the distinct resolved packages.
	// Both are fixed before the package is published and are read-only
	// afterwards.
	importSpellings map[string]*Package
	imports         []*Package

	// The fields below are guarded by loader.buildMu.
	buildState buildState
	vertex     *adt.Vertex // finalized package root
	value      cue.Value
	buildErr   error
}

// buildState tracks the build progress of a package; see Loader.build.
type buildState uint8

const (
	buildNotStarted buildState = iota
	buildInProgress
	buildDone
)

// ImportPath returns the package's canonical import path, with an
// explicit qualifier.
func (p *Package) ImportPath() ast.ImportPath {
	ip := p.ip
	ip.ExplicitQualifier = true
	return ip
}

// Name returns the package name (the import qualifier).
func (p *Package) Name() string {
	return p.ip.Qualifier
}

// Dir returns the directory of the package's source files.
func (p *Package) Dir() string {
	return p.dir
}

// Module returns the module containing the package, or nil for packages
// outside any module (standard library, resolver-provided).
func (p *Package) Module() *Module {
	return p.mod
}

// Files returns the package's CUE files in load order.
func (p *Package) Files() []*SourceFile {
	return slices.Clip(p.files)
}

// DataFiles returns the non-CUE files found alongside the package that
// the loader's codec set recognizes. They are not decoded; pass them to
// [Loader.Decode] as needed.
func (p *Package) DataFiles() []File {
	return slices.Clip(p.data)
}

// Imports returns the packages imported by this one.
func (p *Package) Imports() []*Package {
	return slices.Clip(p.imports)
}

// Err returns the aggregated load and parse errors of the package.
func (p *Package) Err() error {
	return p.err
}

// Value builds and returns the package's value. The result is computed
// once and cached; concurrent calls share the work. An error is
// returned for load, parse, and compile failures and for cancellation;
// evaluation errors are part of the returned value and surface when it
// is used.
//
// If the build is cancelled via ctx the result is not cached and a
// later call starts afresh.
func (p *Package) Value(ctx context.Context) (cue.Value, error) {
	l := p.loader
	l.buildMu.Lock()
	defer l.buildMu.Unlock()
	if err := l.buildLocked(ctx, p); err != nil {
		return cue.Value{}, err
	}
	return p.value, nil
}

// A SourceFile is one CUE file of a package.
type SourceFile struct {
	// Name is the file's name as used in positions and error messages.
	Name string

	// Syntax is the parsed file.
	Syntax *ast.File
}

// A Module describes a CUE module.
type Module struct {
	loader  *Loader
	path    string // qualified module path, e.g. foo.com/bar@v0
	version module.Version
	file    *modfile.File
	loc     module.SourceLoc // root location of the module contents
	rootDir string           // display path of the module root
}

// Path returns the module path, including its major version suffix
// (for example foo.com/bar@v0).
func (m *Module) Path() string {
	return m.path
}

// Version returns the module's resolved version, if any. The main
// module has no version.
func (m *Module) Version() module.Version {
	return m.version
}

// File returns the module's parsed cue.mod/module.cue file, if
// available.
func (m *Module) File() *modfile.File {
	return m.file
}

// Packages iterates over all packages contained in the module.
func (m *Module) Packages(ctx context.Context) iter.Seq2[*Package, error] {
	return func(yield func(*Package, error) bool) {
		l := m.loader
		modIP := ast.ParseImportPath(m.path)
		paths, err := expandModuleFiles(l, m.loc, modIP, ".", "")
		if err != nil {
			yield(nil, err)
			return
		}
		pkgs, err := l.ensurePackages(ctx, paths)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, path := range paths {
			if !yield(pkgs[path], nil) {
				return
			}
		}
	}
}

// stdlibPackage returns a Package representing a standard library
// package.
func (l *Loader) stdlibPackage(ip ast.ImportPath) *Package {
	return &Package{
		loader: l,
		ip:     ip.Canonical(),
		isStd:  true,
	}
}
