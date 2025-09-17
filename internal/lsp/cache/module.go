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

	// dirtyFiles holds dirty files within the module's packages only:
	// i.e. any file that was loaded by any of the module's
	// packages. We gather these files in the Module rather than in
	// their own Package because when we reload a module, we don't care
	// which package a file ends up in (it can change package). We only
	// care that all the dirty files are loaded by _some_ package
	// within the module.
	dirtyFiles map[protocol.DocumentURI]struct{}
}

// NewModule creates a new [Module] and adds it to the workspace. The
// CUE module itself (that is, the cue.mod/module.cue file) is not
// loaded until [Module.ReloadModule] is called.
func NewModule(modFileUri protocol.DocumentURI, w *Workspace) *Module {
	m := &Module{
		workspace:  w,
		rootURI:    modFileUri.Dir().Dir(),
		modFileURI: modFileUri,
		packages:   make(map[ast.ImportPath]*Package),
		dirtyFiles: map[protocol.DocumentURI]struct{}{
			modFileUri: {},
		},
	}
	w.modules[m.rootURI] = m
	w.debugLog(fmt.Sprintf("%v Created", m))
	// We only create a new module when we discover a
	// cue.mod/module.cue file. Even without loading it, it's correct
	// to invalidate the workspace's active files+dirs. The other way
	// of looking at this is that a module only contains a single file
	// - the cue.mod/module.cue file. If the content of that file
	// changes, we do not need to invalidate active files+dirs. By
	// contrast, a package can contain a variable number of files,
	// which can change on every reload.
	w.invalidateActiveFilesAndDirs()
	return m
}

func (m *Module) String() string {
	if m.modFile == nil {
		return fmt.Sprintf("Module dir=%v module=unknown", m.rootURI)
	} else {
		return fmt.Sprintf("Module dir=%v module=%v", m.rootURI, m.modFile.QualifiedModule())
	}
}

// markFileDirty implements [packageOrModule]
func (m *Module) markFileDirty(file protocol.DocumentURI) {
	if file != m.modFileURI {
		panic(fmt.Sprintf("%v being told about file %v", m, file))
	}
	m.dirtyFiles[m.modFileURI] = struct{}{}
}

// MarkFileDirty implements [packageOrModule]
func (m *Module) encloses(file protocol.DocumentURI) bool {
	return m.modFileURI == file
}

// ReloadModule reloads the module's modfile iff the module's status
// is dirty. If an error is encountered when reloading the module, the
// module and all its packages are deleted from the workspace.
func (m *Module) ReloadModule() error {
	_, isDirty := m.dirtyFiles[m.modFileURI]
	if !isDirty {
		return nil
	}

	w := m.workspace
	fh, err := w.overlayFS.ReadFile(m.modFileURI)
	if err != nil {
		w.debugLog(fmt.Sprintf("%v Error when reloading: %v", m, err))
		m.delete()
		return ErrBadModule
	}
	modFile, err := modfile.ParseNonStrict(fh.Content(), m.modFileURI.Path())
	if err != nil {
		w.debugLog(fmt.Sprintf("%v Error when reloading: %v", m, err))
		m.delete()
		return ErrBadModule
	}
	m.modFile = modFile
	delete(m.dirtyFiles, m.modFileURI)
	for _, pkg := range m.packages {
		// TODO: might want to become smarter at this.
		pkg.markDirty()
	}
	w.debugLog(fmt.Sprintf("%v Reloaded", m))
	return nil
}

// ErrBadModule is returned by any method that cannot proceed
// because module is in a bad state (e.g. it's been deleted).
var ErrBadModule = errors.New("bad module")

// delete removes this module from the workspace, along with all the
// packages within this module.
func (m *Module) delete() {
	for _, pkg := range m.packages {
		pkg.delete()
	}
	w := m.workspace
	delete(w.modules, m.rootURI)
	w.debugLog(fmt.Sprintf("%v Deleted", m))
	w.invalidateActiveFilesAndDirs()
}

// ReadCUEFile attempts to read the file, using the language version
// extracted from the module's Language.Version field. This will fail
// if the module itself can't be loaded.
func (m *Module) ReadCUEFile(file protocol.DocumentURI) (*ast.File, parser.Config, fscache.FileHandle, error) {
	if err := m.ReloadModule(); err != nil {
		return nil, parser.Config{}, nil, err
	}
	fh, err := m.workspace.overlayFS.ReadFile(file)
	if err != nil {
		return nil, parser.Config{}, nil, err
	}
	versionOption := parser.Version(m.modFile.Language.Version)
	parsedFile, config, err := fh.ReadCUE(parser.NewConfig(versionOption))
	return parsedFile, config, fh, err
}

// FindImportPathForFile calculates the import path and directories
// for the package implied by the given file. The file must be
// enclosed by the module's rootURI. The file will be read and parsed
// as a CUE file, in order to obtain its package.
//
// This method does not inspect existing packages, nor create any new
// package. Use [*Module.EnsurePackage] for that purpose.
func (m *Module) FindImportPathForFile(file protocol.DocumentURI) (*ast.ImportPath, []protocol.DocumentURI, error) {
	if !m.rootURI.Encloses(file) {
		panic(fmt.Sprintf("Attempt to read file %v from module with root %v", file, m.rootURI))
	}

	w := m.workspace
	parsedFile, _, _, err := m.ReadCUEFile(file)
	if parsedFile == nil {
		w.debugLog(fmt.Sprintf("%v Cannot read file %v: %v", m, file, err))
		return nil, nil, err
	}
	pkgName := parsedFile.PackageName()
	if pkgName == "" {
		// temporarily, we just completely ignore the file if it has no
		// package decl. TODO something better
		w.debugLog(fmt.Sprintf("%v No package found for %v", m, file))
		return nil, nil, nil
	}

	dirUri := file.Dir()
	// NB pkgPath will have a '/' at [0]  because m.rootURI will not have a trailing '/'
	pkgPath := strings.TrimPrefix(string(dirUri), string(m.rootURI))

	isOldMod := false
	ip := ast.ImportPath{Qualifier: pkgName}
	var dirUris []protocol.DocumentURI
	for _, prefix := range []string{"/cue.mod/gen/", "/cue.mod/pkg/", "/cue.mod/usr/"} {
		if pkgPathOldMod, wasCut := strings.CutPrefix(pkgPath, prefix); wasCut {
			isOldMod = true
			ip.Path = pkgPathOldMod
			dirUris = []protocol.DocumentURI{
				m.rootURI + "/cue.mod/gen/" + protocol.DocumentURI(pkgPathOldMod),
				m.rootURI + "/cue.mod/pkg/" + protocol.DocumentURI(pkgPathOldMod),
				m.rootURI + "/cue.mod/usr/" + protocol.DocumentURI(pkgPathOldMod),
			}
			break
		}
	}

	if !isOldMod {
		modPath, version, _ := ast.SplitPackageVersion(m.modFile.QualifiedModule())
		ip.Path = modPath + pkgPath
		ip.Version = version
		dirUris = []protocol.DocumentURI{dirUri}
	}

	ip = ip.Canonical()
	return &ip, dirUris, nil
}

// Package returns the [*Package], if any, for the given import path, within this module.
func (m *Module) Package(ip ast.ImportPath) *Package {
	return m.packages[ip]
}

// EnsurePackage returns the [*Package] for the given import path
// within this module, creating a new package if necessary.
func (m *Module) EnsurePackage(ip ast.ImportPath, dirUris []protocol.DocumentURI) *Package {
	pkg, found := m.packages[ip]
	if !found {
		pkg = m.newPackage(ip, dirUris)
	}
	return pkg
}

// DescendantPackages returns all the existing loaded packages within
// this module that correspond to the given import path, or would
// include the import path's files due to the ancestor-import
// mechanism. The ancestor-import mechanism is not available for the
// "old module" system, so only call this unless you know the import
// path corresponds to the new module system.
//
// This method only returns existing packages; it does not create any
// new packages.
func (m *Module) DescendantPackages(ip ast.ImportPath) []*Package {
	var pkgs []*Package
	pkg, found := m.packages[ip]
	if found {
		pkgs = append(pkgs, pkg)
	}
	for _, pkg := range m.packages {
		pkgIp := pkg.importPath
		if pkgIp.Qualifier == ip.Qualifier && strings.HasPrefix(pkgIp.Path, ip.Path+"/") {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// loadDirtyPackages identifies all dirty packages within the module,
// loads them and returns them. To do this, the modfile itself must be
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
		if !pkg.isDirty {
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

// activeFilesAndDirs implements [packageOrModule]
func (m *Module) activeFilesAndDirs(files map[protocol.DocumentURI][]packageOrModule, dirs map[protocol.DocumentURI]struct{}) {
	files[m.modFileURI] = []packageOrModule{m}
	dirs[m.modFileURI.Dir()] = struct{}{}
	dirs[m.rootURI] = struct{}{}
	for _, pkg := range m.packages {
		pkg.activeFilesAndDirs(files, dirs)
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
		// This can happen if the package was created for an import
		// declaration that could not be found.
		return ip

	} else if mod.IsLocal() {
		// "local" means it's using the old module system
		// (cue.mod/{gen|pkg|usr}) and there is no package versioning in
		// that system.
		return ip
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
