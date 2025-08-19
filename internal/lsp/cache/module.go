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
	"path/filepath"
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
	// immutable fields: all set at construction only

	workspace *Workspace

	// rootURI (the module "root") is the directory that contains the
	// cue.mod directory which itself contains the module.cue file.
	rootURI protocol.DocumentURI
	// modFileURI is the uri of the module's cue.mod/module.cue file.
	modFileURI protocol.DocumentURI

	// mutable fields:

	// modFile is the loaded module.cue file. This is set whenever the
	// module is reloaded.
	modFile *modfile.File

	// packages are the loaded packages within the module. Packages are
	// only loaded on demand, so there may well be other unloaded
	// packages within this module. You must make sure changes to this
	// map are mirrored to the workspace's packages map.
	packages map[ast.ImportPath]*Package

	// status of this Module
	status status

	// dirtyFiles holds dirty files within the module's packages only:
	// i.e. any file that was loaded by any of the module's
	// packages. We gather these files in the Module rather than in
	// their own Package because when we reload a module, we don't care
	// which package a file ends up in (it can change package). We only
	// care that all the dirty files are loaded by _some_ package
	// within the module.
	//
	// This field gets assigned a new map whenever the module reloads
	// its packages.
	dirtyFiles map[protocol.DocumentURI]struct{}
}

// NewModule creates a new Module. The CUE module itself (that is, the
// cue.mod/module.cue file) is not loaded until [Module.ReloadModule]
// is called.
func NewModule(modFileUri protocol.DocumentURI, w *Workspace) *Module {
	return &Module{
		workspace:  w,
		rootURI:    modFileUri.Dir().Dir(),
		modFileURI: modFileUri,
		packages:   make(map[ast.ImportPath]*Package),
		dirtyFiles: make(map[protocol.DocumentURI]struct{}),
	}
}

func (m *Module) String() string {
	if m.modFile == nil {
		return fmt.Sprintf("Module dir=%v module=unknown", m.rootURI)
	} else {
		return fmt.Sprintf("Module dir=%v module=%v", m.rootURI, m.modFile.QualifiedModule())
	}
}

// MarkFileDirty implements [packageOrModule]
func (m *Module) MarkFileDirty(file protocol.DocumentURI) {
	if file != m.modFileURI {
		panic(fmt.Sprintf("%v being told about file %v", m, file))
	}
	m.status = dirty
}

// MarkFileDirty implements [packageOrModule]
func (m *Module) Encloses(file protocol.DocumentURI) bool {
	return m.modFileURI == file
}

// ReloadModule reloads the module's modfile iff the module's status
// is dirty. An error is returned if any problem is encountered when
// reloading the module, or if the module has been marked as being
// deleted.
func (m *Module) ReloadModule() error {
	switch m.status {
	case dirty:
		w := m.workspace
		fh, err := w.overlayFS.ReadFile(m.modFileURI)
		if err != nil {
			m.status = deleted
			return err
		}
		modFile, err := modfile.ParseNonStrict(fh.Content(), m.modFileURI.Path())
		if err != nil {
			m.status = deleted
			return err
		}
		m.modFile = modFile
		m.status = splendid
		delete(m.dirtyFiles, m.modFileURI)
		for _, pkg := range m.packages {
			// TODO: might want to become smarter at this.
			pkg.setStatus(dirty)
		}
		w.debugLog(fmt.Sprintf("%v Reloaded", m))
		return nil
	case splendid:
		return nil
	case deleted:
		return ErrModuleDeleted
	default:
		panic(fmt.Sprintf("Unknown status %v", m.status))
	}
}

// ErrModuleDeleted is returned by any method that cannot proceed
// because module has been marked deleted.
var ErrModuleDeleted = errors.New("Module deleted")

// ReadCUEFile attempts to read the file, using the language version
// extracted from the module's Language.Version field. This will fail
// if the module's module.cue file is invalid, and ErrModuleInvalid
// will be returned.
func (m *Module) ReadCUEFile(file protocol.DocumentURI) (*ast.File, fscache.FileHandle, error) {
	if err := m.ReloadModule(); err != nil {
		return nil, nil, err
	}
	fh, err := m.workspace.overlayFS.ReadFile(file)
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

// FindPackagesOrModulesForFile searches for the given file in both
// the module itself, and packages within the module, returning a
// (possibly empty) list of [packageOrModule]s to which the file
// belongs. The file must be enclosed by the module's rootURI.
//
// The file will belong to the module if the file is the module's
// modFileURI.
//
// Otherwise, the file will be read and parsed as a CUE file, in order
// to obtain its package. If the module doesn't already have a
// suitable package one will be created.
//
// Already-loaded packages which include this file via
// ancestor-imports are also returned in the list. However, if such
// packages exist but are not loaded, they are not discovered
// here. For example, if file is file:///a/b.cue, and it contains
// "package x", and a package a/c:x within the same module is already
// loaded, then the results will contain both package a:x and a/c:x,
// but only if a/c:x is already loaded.
func (m *Module) FindPackagesOrModulesForFile(file protocol.DocumentURI) ([]packageOrModule, error) {
	if !m.rootURI.Encloses(file) {
		panic(fmt.Sprintf("Attempt to read file %v from module with root %v", file, m.rootURI))
	}
	if err := m.ReloadModule(); err != nil {
		return nil, err
	}

	if file == m.modFileURI {
		return []packageOrModule{m}, nil
	}

	w := m.workspace
	parsedFile, _, err := m.ReadCUEFile(file)
	if err != nil {
		return nil, err
	}
	pkgName := parsedFile.PackageName()
	if pkgName == "" {
		// temporarily, we just completely ignore the file if it has no
		// package decl. TODO something better
		w.debugLog(fmt.Sprintf("%v No package found for %v", m, file))
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
	// Search also for descendent packages that might include the file
	// by virtue of the ancestor-import-path pattern.
	for _, pkg := range m.packages {
		pkgIp := pkg.importPath
		if pkgIp.Qualifier == ip.Qualifier && strings.HasPrefix(pkgIp.Path, ip.Path+"/") {
			pkgs = append(pkgs, pkg)
		}
	}

	w.debugLog(fmt.Sprintf("%v For file %v found %v", m, file, pkgs))

	return pkgs, nil
}

// loadDirtyPackages identifies all dirty packages within the module,
// loads them and returs them. To do this, the modfile itself must be
// successfully loaded. The only non-nil error this method returns is
// if the modfile cannot be loaded.
func (m *Module) loadDirtyPackages() (*modpkgload.Packages, error) {
	if err := m.ReloadModule(); err != nil {
		return nil, err
	}

	w := m.workspace

	// 1. Gather all the dirty packages.
	var pkgPaths []string
	for _, pkg := range m.packages {
		if pkg.status != dirty {
			continue
		}
		pkgPaths = append(pkgPaths, pkg.importPath.String())
	}

	if len(pkgPaths) == 0 {
		return nil, nil
	}

	// 2. Load all the packages found
	modPath := m.modFile.QualifiedModule()
	reqs := modrequirements.NewRequirements(modPath, w.registry, m.modFile.DepVersions(), m.modFile.DefaultMajorVersions())
	rootUri := m.rootURI
	ctx := context.Background()
	loc := module.SourceLoc{
		FS:  w.overlayFS.IoFS(rootUri.Path()),
		Dir: ".", // NB can't be ""
	}
	// Determinism in log messages:
	slices.Sort(pkgPaths)
	w.debugLog(fmt.Sprintf("%v Loading packages %v", m, pkgPaths))
	loadedPkgs := modpkgload.LoadPackages(ctx, modPath, loc, reqs, w.registry, pkgPaths, nil)

	return loadedPkgs, nil
}

// ActiveFilesAndDirs implements [packageOrModule]
func (m *Module) ActiveFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	files[m.modFileURI] = []packageOrModule{m}
	dirs[m.modFileURI.Dir()] = struct{}{}
	dirs[m.rootURI] = struct{}{}
	for _, pkg := range m.packages {
		pkg.ActiveFilesAndDirs(files, dirs)
	}
}

// normalizeImportPath is used to normalize and canonicalize import
// paths from [modpkgload.Package]. The ImportPath from
// modpkgload.Package reflects how the import was spelt in the cue
// file. This means it could be missing the major version suffix. We
// always want all import paths to be canonical, and with non-empty
// major versions.
func normalizeImportPath(pkg *modpkgload.Package) ast.ImportPath {
	ip := ast.ParseImportPath(pkg.ImportPath()).Canonical()
	if ip.Version != "" || pkg.IsStdlibPackage() {
		return ip
	}

	mod := pkg.Mod()
	if !mod.IsValid() {
		panic(fmt.Sprintf("unable to normalize import path %v", pkg.ImportPath()))
	}

	// Favour extracting the major version from path over
	// semver.Major(mod.Version) because for an import of a package
	// within this module, the mod.Version is left blank, but the
	// mod.Path will have a major version suffix.
	_, ip.Version, _ = ast.SplitPackageVersion(mod.Path())

	return ip
}

// moduleRootURI determines the URI for the package's module root
// (i.e. the URI of the directory that contains the mod.cue
// directory). This function will panic if pkg is stdlib.
func moduleRootURI(pkg *modpkgload.Package) protocol.DocumentURI {
	modRoot := pkg.ModRoot()
	modFS := modRoot.FS.(module.OSRootFS)
	modRootPath := filepath.Join(modFS.OSRoot(), modRoot.Dir)
	return protocol.URIFromPath(modRootPath)
}
