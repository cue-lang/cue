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

package cache

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// Module models a single CUE module.
type Module struct {
	// immutable: set once at construction
	rootURI protocol.DocumentURI
	// immutable: set once at construction
	modFileURI protocol.DocumentURI
	// mutable: set whenever the module is reloaded
	modFile *modfile.File

	// immutable: set once at construction
	registry Registry
	// immutable: set once at construction
	fs *fscache.OverlayFS

	// immutable: set once at construction
	debugLog func(string)

	// mutable
	packages map[ast.ImportPath]*Package
	// mutable
	status status

	// These are dirty files within the module's packages only:
	// i.e. any file that was loaded by any of the module's
	// packages. We gather these files here because when we reload a
	// module, we don't care which package a file ends up in (and it
	// can change package). We only care that all the dirty files are
	// loaded by _some_ package within the module.
	//
	// This field gets assigned a new map whenever the module reloads
	// its packages.
	dirtyFiles map[protocol.DocumentURI]struct{}
}

// NewModule creates a new Module. The fh is the handle for the
// cue.mod/module.cue file, which must be within the FS reachable via
// overlayFS (though not necessarily an overlay). The CUE module is
// not loaded until [Module.ReloadModule] is called.
func NewModule(fh fscache.FileHandle, registry Registry, overlayFS *fscache.OverlayFS, debugLog func(string)) (*Module, error) {
	uri := fh.URI()
	modFile, err := modfile.ParseNonStrict(fh.Content(), uri.Path())
	if err != nil {
		return nil, err
	}
	rootUri := uri.Dir().Dir()
	m := &Module{
		rootURI:    rootUri,
		modFileURI: uri,
		modFile:    modFile,
		registry:   registry,
		fs:         overlayFS,
		debugLog:   debugLog,
		packages:   make(map[ast.ImportPath]*Package),
		dirtyFiles: make(map[protocol.DocumentURI]struct{}),
	}
	return m, nil
}

func (m *Module) String() string {
	if m.modFile == nil {
		return fmt.Sprintf("Module dir=%v module=unknown", m.rootURI)
	} else {
		return fmt.Sprintf("Module dir=%v module=%v", m.rootURI, m.modFile.QualifiedModule())
	}
}

func (m *Module) MarkFileDirty(file protocol.DocumentURI) {
	if file != m.modFileURI {
		panic(fmt.Sprintf("%v being told about file %v", m, file))
	}
	m.status = dirty
}

func (m *Module) Encloses(file protocol.DocumentURI) bool {
	return m.rootURI.Encloses(file)
}

// Reload's the module's modfile iff the module's status is dirty.
// Returns true iff the module ends up in a splendid status.
//
// As a general rule, any method on Module that relies on any state
// within the module, should start by calling ReloadModule, and should
// bail if ReloadModule does not return true.
func (m *Module) ReloadModule() bool {
	switch m.status {
	case dirty:
		fh, err := m.fs.ReadFile(m.modFileURI)
		if err != nil {
			m.status = deleted
			return false
		}
		modFile, err := modfile.ParseNonStrict(fh.Content(), m.modFileURI.Path())
		if err != nil {
			m.status = deleted
			return false
		}
		m.modFile = modFile
		m.status = splendid
		delete(m.dirtyFiles, m.modFileURI)
		for _, pkg := range m.packages {
			// TODO: might want to become smarter at this.
			pkg.status = dirty
		}
		m.debugLog(fmt.Sprintf("%v Reloaded", m))
		return true
	case splendid:
		return true
	case deleted:
		return false
	default:
		panic(fmt.Sprintf("Unknown status %v", m.status))
	}
}

var ErrModuleInvalid = errors.New("Module invalid")

func (m *Module) ReadCUEFile(file protocol.DocumentURI) (*ast.File, fscache.FileHandle, error) {
	if valid := m.ReloadModule(); !valid {
		return nil, nil, ErrModuleInvalid
	}
	fh, err := m.fs.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	versionOption := parser.Version(m.modFile.Language.Version)
	parsedFile, err := fh.ReadCUE(parser.NewConfig(versionOption))
	if err != nil {
		return nil, nil, err
	}
	return parsedFile, fh, nil
}

func (m *Module) FindPackagesOrModulesForFile(file protocol.DocumentURI) ([]packageOrModule, error) {
	if !m.rootURI.Encloses(file) {
		panic(fmt.Sprintf("Attempt to read file %v from module with root %v", file, m.rootURI))
	}
	if valid := m.ReloadModule(); !valid {
		return nil, ErrModuleInvalid
	}

	if file == m.modFileURI {
		return []packageOrModule{m}, nil
	}

	parsedFile, _, err := m.ReadCUEFile(file)
	if err != nil {
		return nil, err
	}
	pkgName := parsedFile.PackageName()
	if pkgName == "" {
		// temporarily, we just completely ignore the file if it has no
		// package decl. TODO something better
		m.debugLog(fmt.Sprintf("%v No package found for %v", m, file))
		return nil, nil
	}

	dirUri := file.Dir()
	// NB pkgPath will have a '/' at [0]  because m.rootURI will not have a trailing '/'
	pkgPath := strings.TrimPrefix(string(dirUri), string(m.rootURI))

	modPath, version, _ := ast.SplitPackageVersion(m.modFile.QualifiedModule())

	ip := ast.ImportPath{
		Path:      modPath + pkgPath,
		Version:   version,
		Qualifier: pkgName,
	}.Canonical()

	// the exact package is always needed:
	pkg, found := m.packages[ip]
	if !found {
		pkg = NewPackage(m, ip, dirUri)
		m.packages[ip] = pkg
	}
	pkgs := []packageOrModule{pkg}
	// we also search for descendent packages, for the ancestor-import-path pattern
	for _, pkg := range m.packages {
		pkgIp := pkg.importPath
		if pkgIp.Qualifier == ip.Qualifier && strings.HasPrefix(pkgIp.Path, ip.Path+"/") {
			pkgs = append(pkgs, pkg)
		}
	}

	m.debugLog(fmt.Sprintf("%v For file %v found %v", m, file, pkgs))

	return pkgs, nil
}

func (m *Module) ReloadPackages() error {
	if valid := m.ReloadModule(); !valid {
		return ErrModuleInvalid
	}

	// 1. Gather all the dirty packages.
	packages := m.packages
	var pkgsToLoadWorklist []*Package
	for _, pkg := range packages {
		if pkg.status == dirty {
			pkgsToLoadWorklist = append(pkgsToLoadWorklist, pkg)
		}
	}

	if len(pkgsToLoadWorklist) == 0 {
		return nil
	}

	// 2. From the dirty packages, follow the inverted import graph,
	// finding all packages that import (transitively) a dirty package.
	var pkgPaths []string
	pkgsToLoadSet := make(map[*Package]struct{})
	for i := 0; i < len(pkgsToLoadWorklist); i++ {
		pkg := pkgsToLoadWorklist[i]
		if _, seen := pkgsToLoadSet[pkg]; seen {
			continue
		}
		pkgsToLoadSet[pkg] = struct{}{}
		pkgPaths = append(pkgPaths, pkg.importPath.String())
		pkgsToLoadWorklist = append(pkgsToLoadWorklist, pkg.importedBy...)
	}

	// 3. Load all the packages found in (1+2)
	modPath := m.modFile.QualifiedModule()
	reqs := modrequirements.NewRequirements(modPath, m.registry, m.modFile.DepVersions(), m.modFile.DefaultMajorVersions())
	rootUri := m.rootURI
	ctx := context.Background()
	loc := module.SourceLoc{
		FS:  m.fs.IoFS(rootUri.Path()),
		Dir: ".", // NB can't be ""
	}
	// Determinism in log messages:
	slices.Sort(pkgPaths)
	m.debugLog(fmt.Sprintf("%v Loading packages %v", m, pkgPaths))
	loadedPkgs := modpkgload.LoadPackages(ctx, modPath, loc, reqs, m.registry, pkgPaths, nil)

	dirtyFiles := m.dirtyFiles
	m.dirtyFiles = make(map[protocol.DocumentURI]struct{})

	// 4. Process the results of loading the packages. We need to do
	// this in two passes to ensure we create all necessary packages
	// before trying to build/update the inverted import graph.
	//
	// Note, we use loadedPkgs.All() and not loadedPkgs.Roots(). This
	// is because if we happen to successfully load other packages
	// within this module, we should track all of them.
	pkgsImportsWorklist := make(map[*Package]*modpkgload.Package)
	for _, loadedPkg := range loadedPkgs.All() {
		if loadedPkg.FromExternalModule() {
			// Because we don't currently support "replace" in module.cue
			// files, we cannot have one local module importing another
			// local module. Therefore, there's no need to attempt to
			// find the correct local module (and [Module]) for "external
			// modules" - currently, by definition, they are from the
			// registry, and so not in any LSP WorkspaceFolder.
			//
			// TODO: we might (or might not) need to fix this once we do
			// support "replace".
			continue
		}

		ip := m.ParseImportPath(loadedPkg.ImportPath())

		if loadedPkg.Error() != nil {
			// It could be that the last file within this package was
			// deleted, so attempting to load it will create an error. So
			// the correct thing to do now is just nuke any record of the
			// pkg.
			//
			// TODO: if packages contains this ip, then we should
			// probably look at its importedBy field as that would tell
			// us about packages that now have dangling imports.
			delete(packages, ip)
			continue
		}

		// Imagine some cue file in package foo.com/x has "import
		// foo.com/y" in it. Imagine that we knew that both packages x
		// and y are dirty, so when we called modpkgload.LoadPackages,
		// we had pkgPaths set to [foo.com/x@v0, foo.com/y@v0]. Because
		// modpkgload.LoadPackages does not normalise import paths, we
		// end up with two loadings of y - one from the explicit pkgPath
		// foo.com/y@v0, and one from the import foo.com/y (the
		// different spelling is the critical thing).
		//
		// So we need to test whether we've already seen this package in
		// the results of this load. If we have, we can skip.

		pkg, found := packages[ip]
		if found {
			if _, seen := pkgsImportsWorklist[pkg]; seen {
				continue
			}
		} else {
			// Every package contains cue sources from one "leaf"
			// directory and optionally any ancestor directory. Here we
			// determine that "leaf" directory:
			dirUri := protocol.DocumentURI("")
			for _, loc := range loadedPkg.Locations() {
				uri := protocol.DocumentURI(string(rootUri) + "/" + loc.Dir)
				if dirUri == "" || dirUri.Encloses(uri) {
					dirUri = uri
				}
			}
			pkg = NewPackage(m, ip, dirUri)
			packages[ip] = pkg
		}
		// capture the old loadedPkg (if it exists) so we can correct
		// the import graph later.
		pkgsImportsWorklist[pkg] = pkg.pkg
		pkg.pkg = loadedPkg
		pkg.status = splendid
		m.debugLog(fmt.Sprintf("%v Loaded %v", m, pkg))

		if len(dirtyFiles) != 0 {
			for _, file := range loadedPkg.Files() {
				fileUri := protocol.DocumentURI(string(rootUri) + "/" + file.FilePath)
				delete(dirtyFiles, fileUri)
				if len(dirtyFiles) == 0 {
					break
				}
			}
		}
	}

	// 5. 2nd pass: create/correct inverted import graph now that all
	// necessary Packages exist. pkgsImportsWorklist will only contain
	// local packages (i.e. packages within this module)
	imports := make(map[ast.ImportPath]struct{})
	for pkg, oldPkg := range pkgsImportsWorklist {
		clear(imports)
		if oldPkg != nil {
			for _, i := range oldPkg.Imports() {
				imports[m.ParseImportPath(i.ImportPath())] = struct{}{}
			}
		}
		for _, i := range pkg.pkg.Imports() {
			ip := m.ParseImportPath(i.ImportPath())
			if _, found := imports[ip]; found {
				// both new and old pkgs import ip. Noop
				delete(imports, ip)
			} else if importedPkg, found := packages[ip]; found {
				// only new pkg imports ip. Add the back-pointer.
				importedPkg.EnsureImportedBy(pkg)
			}
		}
		for ip := range imports {
			if importedPkg, found := packages[ip]; found {
				// only old pkg imports ip. Remove the back-pointer.
				importedPkg.RemoveImportedBy(pkg)
			}
		}
	}
	// Note that there's a potential memory leak here: we might load a
	// package "foo" because it's imported by "bar". If "bar" is edited
	// so that it no longer imports "foo" then we'll notice that, and
	// our "foo" Package will get updated so that its importedBy field
	// is empty. But we never currently remove the "foo" package from
	// the module. Ideally, we should keep track within each Package of
	// the number of its files open in the editor/client. If that drops
	// to zero, and the importedBy field is empty, then we should
	// remove the package from the module. TODO.

	// defensive: just asserting internal logic - if it was due to be
	// loaded, and it has been loaded successfully, then it really
	// should exist in the packages map (and the inverse too).
	for pkg := range pkgsToLoadSet {
		shouldExist := pkg.status == splendid
		_, exists := packages[pkg.importPath]
		if shouldExist != exists {
			panic(fmt.Sprintf("%v: shouldExist? %v; exists? %v", pkg, shouldExist, exists))
		}
	}

	// We need to watch out for when a dirty file moves package, either
	// to an existing package which we've not reloaded, or to a package
	// that we've never loaded. In both cases, the file will still be
	// within this module.
	repeatReload := false
	for fileUri := range dirtyFiles {
		pkgs, err := m.FindPackagesOrModulesForFile(fileUri)
		if err != nil {
			return err
		}
		if len(pkgs) != 0 {
			for _, pkg := range pkgs {
				pkg.MarkFileDirty(fileUri)
			}
			repeatReload = true
		}
		// if len(pkgs) == 0 and no error, then it means the file had no
		// package declaration. For the time being, we're ignoring
		// that. TODO something better
	}
	if repeatReload {
		return m.ReloadPackages()
	}
	return nil
}

// See [packageOrModule.ActiveFilesAndDirs]
func (m *Module) ActiveFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	if err := m.ReloadPackages(); err != nil {
		return
	}
	files[m.modFileURI] = []packageOrModule{m}
	dirs[m.modFileURI.Dir()] = struct{}{}
	dirs[m.rootURI] = struct{}{}
	for _, pkg := range m.packages {
		pkg.ActiveFilesAndDirs(files, dirs)
	}
}

// ParseImportPath is used to canonicalise import paths from
// [modpkgload.Package]. The ImportPath from modpkgload.Package
// reflects how the import was spelt in the cue file. This means it
// could be missing the major version suffix. We always want all
// import paths to be canonical, and with non-empty major versions.
func (m *Module) ParseImportPath(importPath string) ast.ImportPath {
	if valid := m.ReloadModule(); !valid {
		panic("ParseLocalImportPath can only be used when the module is valid")
	}

	ip := ast.ParseImportPath(importPath).Canonical()
	if ip.Version != "" {
		return ip
	}

	vers, ok := m.modFile.ModuleForImportPath(importPath)
	if !ok {
		panic(fmt.Sprintf("ParseLocalImportPath in module %v, unable to parse import path %v", m.modFile.QualifiedModule(), importPath))
	}

	_, ip.Version, _ = ast.SplitPackageVersion(vers.Path())
	return ip
}
