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
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/buildattr"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// ensurePackages returns the loader's canonical *Package for each of
// the given canonical import paths, creating them as needed. For each
// path it consults, in order: the loader's cache, Config.Resolve, the
// standard library, and the module system. It must not be called with
// buildMu held: it invokes the Resolve hook and performs module I/O.
func (l *Loader) ensurePackages(ctx context.Context, paths []string) (map[string]*Package, error) {
	result := make(map[string]*Package, len(paths))
	var missing []string
	for _, p := range paths {
		if _, ok := result[p]; ok {
			continue
		}
		pkg, err := l.lookupPackage(ctx, p)
		if err != nil {
			return nil, err
		}
		if pkg != nil {
			result[p] = pkg
			continue
		}
		missing = append(missing, p)
	}
	if len(missing) == 0 {
		return result, nil
	}
	if l.module == nil {
		for _, p := range missing {
			result[p] = l.internPackage(p, &Package{
				loader: l,
				ip:     ast.ParseImportPath(p).Canonical(),
				err:    fmt.Errorf("no CUE module found: cannot resolve import %q", p),
			})
		}
		return result, nil
	}
	loaded := modpkgload.LoadPackages(
		ctx,
		l.module.path,
		l.module.loc,
		l.reqs,
		l.registry,
		nil, // TODO(cueload): module replacements (cue.mod/local-module.cue)
		missing,
		l.shouldIncludeFile,
	)
	if err := l.convertPackages(ctx, loaded); err != nil {
		return nil, err
	}
	for _, p := range missing {
		pkg := l.cachedPackage(p)
		if pkg == nil {
			// Should not happen: modpkgload reports every root.
			pkg = l.internPackage(p, &Package{
				loader: l,
				ip:     ast.ParseImportPath(p).Canonical(),
				err:    fmt.Errorf("cannot load package %q", p),
			})
		}
		result[p] = pkg
	}
	return result, nil
}

// lookupPackage resolves a single canonical import path through the
// cache, the Resolve hook, and the standard library. It returns nil if
// the path needs module resolution.
func (l *Loader) lookupPackage(ctx context.Context, p string) (*Package, error) {
	if pkg := l.cachedPackage(p); pkg != nil {
		return pkg, nil
	}
	ip := ast.ParseImportPath(p)
	if l.cfg.Resolve != nil {
		pkg, err := l.cfg.Resolve.ResolvePackage(ctx, ip)
		if err != nil {
			return nil, err
		}
		if pkg != nil {
			if pkg.loader != l {
				return nil, fmt.Errorf("cueload: resolver returned a package from a different loader for %q", p)
			}
			return l.internPackage(p, pkg), nil
		}
	}
	if modpkgload.IsStdlibPackage(ip.Path) {
		return l.internPackage(p, l.stdlibPackage(ip)), nil
	}
	return nil, nil
}

// cachedPackage returns the cached package for a canonical import path.
func (l *Loader) cachedPackage(p string) *Package {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pkgs[p]
}

// internPackage stores pkg under the given canonical import path,
// returning the already-stored package if there is one.
func (l *Loader) internPackage(p string, pkg *Package) *Package {
	l.mu.Lock()
	defer l.mu.Unlock()
	if prev, ok := l.pkgs[p]; ok {
		return prev
	}
	l.pkgs[p] = pkg
	return pkg
}

// convertPackages converts the packages loaded by modpkgload into
// loader Packages, parsing their files and wiring up their imports.
// Newly created packages are published (interned) only once fully
// wired, so that concurrent loads never observe a partial package.
func (l *Loader) convertPackages(ctx context.Context, loaded *modpkgload.Packages) error {
	all := loaded.All()
	// First create every package, so that import wiring below can rely
	// on all of them being present. Packages already known to the
	// loader — cached, resolver-provided, or stdlib — are not
	// converted; lookupPackage interns those directly (they are
	// complete on creation).
	local := make(map[string]*Package, len(all))
	var localKeys []string
	for _, modpkg := range all {
		key := l.canonicalKey(modpkg.ImportPath())
		if local[key] != nil {
			continue
		}
		pkg, err := l.lookupPackage(ctx, key)
		if err != nil {
			return err
		}
		if pkg != nil {
			continue
		}
		local[key] = l.newModulePackage(key, modpkg)
		localKeys = append(localKeys, key)
	}
	dep := func(key string) *Package {
		if p, ok := local[key]; ok {
			return p
		}
		return l.cachedPackage(key)
	}
	// Wire up the imports of the newly created packages.
	for _, modpkg := range all {
		pkg := local[l.canonicalKey(modpkg.ImportPath())]
		if pkg == nil || pkg.modpkg != modpkg {
			continue
		}
		spellings := make(map[string]*Package)
		for _, sf := range pkg.files {
			for spec := range sf.Syntax.ImportSpecs() {
				spelling, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				if _, ok := spellings[spelling]; ok {
					continue
				}
				spellings[spelling] = dep(l.canonicalKey(modpkg.CanonicalImportPath(spelling)))
			}
		}
		pkg.importSpellings = spellings
		var imports []*Package
		seen := make(map[*Package]bool)
		for _, spelling := range slices.Sorted(maps.Keys(spellings)) {
			if dep := spellings[spelling]; dep != nil && !seen[dep] {
				seen[dep] = true
				imports = append(imports, dep)
			}
		}
		pkg.imports = imports
	}
	// Publish. On a conflict with a concurrent load the first package
	// wins; our local one becomes unreachable, and its wired imports
	// point at packages that resolve identically.
	for _, key := range localKeys {
		l.internPackage(key, local[key])
	}
	return nil
}

// newModulePackage creates a Package from a package loaded through the
// module system, parsing its files in full and applying @tag injection
// for main-module packages.
func (l *Loader) newModulePackage(key string, modpkg *modpkgload.Package) *Package {
	p := &Package{
		loader: l,
		ip:     ast.ParseImportPath(key).Canonical(),
		modpkg: modpkg,
	}
	if err := modpkg.Error(); err != nil {
		p.err = err
		return p
	}
	if modpkg.IsStdlibPackage() {
		p.isStd = true
		return p
	}
	p.mod = l.moduleFor(modpkg)
	locs := modpkg.Locations()
	if len(locs) == 0 {
		p.err = fmt.Errorf("no source locations for package %q", key)
		return p
	}
	p.dir = l.displayPath(p.mod, locs[0].Dir)

	// Parse the package files in full. The syntax carried by modpkgload
	// was parsed with ImportsOnly and cannot be used for building.
	pcfg := l.parserCfg
	if p.mod != l.module {
		pcfg = parser.NewConfig(parser.ParseComments)
		if mf := p.mod.file; mf != nil && mf.Language != nil && mf.Language.Version != "" {
			pcfg = pcfg.Apply(parser.Version(mf.Language.Version))
		}
	}
	var errs errors.Error
	for _, mf := range modpkg.Files() {
		data, err := fs.ReadFile(locs[0].FS, mf.FilePath)
		if err != nil {
			errs = errors.Append(errs, errors.Promote(err, "load"))
			continue
		}
		name := l.displayPath(p.mod, mf.FilePath)
		syntax, err := l.parseFile(name, data, pcfg)
		if err != nil {
			errs = errors.Append(errs, errors.Promote(err, "parse"))
			continue
		}
		p.files = append(p.files, &SourceFile{Name: name, Syntax: syntax})
	}

	// Inject @tag values into main-module packages.
	if p.mod == l.module && len(p.files) > 0 {
		syntax := make([]*ast.File, len(p.files))
		for i, sf := range p.files {
			syntax[i] = sf.Syntax
		}
		errs = errors.Append(errs, l.injectTags(syntax))
	}

	// Collect recognized non-CUE files alongside the package.
	if entries, err := fs.ReadDir(locs[0].FS, locs[0].Dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := path.Ext(name)
			if ext == ".cue" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			if _, ok := l.codecs.ByExtension(ext); ok {
				p.data = append(p.data, File{
					Name: l.displayPath(p.mod, path.Join(locs[0].Dir, name)),
				})
			}
		}
	}

	if errs != nil {
		p.err = errs
	}
	return p
}

// moduleFor returns the Module describing the module that modpkg
// belongs to, creating and caching it as needed.
func (l *Loader) moduleFor(modpkg *modpkgload.Package) *Module {
	mv := modpkg.Mod()
	if l.module != nil && mv.Path() == l.module.path {
		return l.module
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if m, ok := l.modules[mv]; ok {
		return m
	}
	m := &Module{
		loader:  l,
		path:    mv.Path(),
		version: mv,
		loc:     modpkg.ModRoot(),
	}
	if osFS, ok := m.loc.FS.(module.OSRootFS); ok {
		if root := osFS.OSRoot(); root != "" {
			m.rootDir = path.Join(root, m.loc.Dir)
		}
	}
	// Best-effort: parse the module's own module.cue for Module.File.
	if data, err := fs.ReadFile(m.loc.FS, path.Join(m.loc.Dir, "cue.mod/module.cue")); err == nil {
		if mf, err := modfile.ParseNonStrict(data, "cue.mod/module.cue"); err == nil {
			m.file = mf
		}
	}
	l.modules[mv] = m
	return m
}

// displayPath maps a path relative to a module root to the name used
// in positions and error messages.
func (l *Loader) displayPath(m *Module, rel string) string {
	rel = path.Clean(rel)
	if rel == "." {
		rel = ""
	}
	switch {
	case m == nil:
		return rel
	case m.rootDir != "":
		return path.Join(m.rootDir, rel)
	default:
		return path.Join(m.path+"@"+m.version.Version(), rel)
	}
}

// shouldIncludeFile is the modpkgload file filter: it applies the
// _test.cue/_tool.cue rules and @if()/@ignore() build attributes,
// mirroring cue/load.
func (l *Loader) shouldIncludeFile(pkgPath string, mod module.Version, fsys fs.FS, mf modimports.ModuleFile) bool {
	if !l.cfg.IncludeTools && strings.HasSuffix(mf.FilePath, "_tool.cue") {
		return false
	}
	isTest := strings.HasSuffix(mf.FilePath, "_test.cue")
	var tagIsSet func(string) bool
	if l.module != nil && mod.Path() == l.module.path {
		// In the main module.
		if isTest && !l.cfg.IncludeTests {
			return false
		}
		tagIsSet = func(key string) bool { return l.buildTagSet[key] }
	} else {
		// Outside the main module.
		if isTest {
			return false
		}
		tagIsSet = func(string) bool { return false }
	}
	ok, _, err := buildattr.ShouldBuildFile(mf.Syntax, tagIsSet)
	return err == nil && ok
}

// canonicalKey returns the canonical form of an import path, resolving
// a missing major version against the main module and its dependency
// defaults where possible. This is what maps the various spellings of
// an import path (foo.com/bar, foo.com/bar@v0, foo.com/bar@v0:bar) to
// one canonical *Package.
func (l *Loader) canonicalKey(p string) string {
	ip := ast.ParseImportPath(p).Canonical()
	if ip.Version == "" && l.module != nil && !modpkgload.IsStdlibPackage(ip.Path) {
		if v, ok := l.defaultMajorVersion(ip.Path); ok {
			ip.Version = v
		}
	}
	return ip.String()
}

// defaultMajorVersion reports the default major version for an import
// path: the main module's own version for packages inside it, or the
// default major version of a dependency prefix.
func (l *Loader) defaultMajorVersion(pkgPath string) (string, bool) {
	mainIP := ast.ParseImportPath(l.module.path)
	if pkgPath == mainIP.Path || strings.HasPrefix(pkgPath, mainIP.Path+"/") {
		return mainIP.Version, mainIP.Version != ""
	}
	defaults := l.modFile.DefaultMajorVersions()
	for prefix := pkgPath; prefix != "." && prefix != "/" && prefix != ""; prefix = path.Dir(prefix) {
		if v, ok := defaults[prefix]; ok {
			return v, true
		}
	}
	return "", false
}

// resolveFileImports resolves the import spellings of a package whose
// files were provided directly (NewPackage), wiring its imports. It
// must not be called with buildMu held.
func (l *Loader) resolveFileImports(ctx context.Context, p *Package) error {
	spellings := make(map[string]string) // spelling -> canonical key
	for _, sf := range p.files {
		for spec := range sf.Syntax.ImportSpecs() {
			spelling, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("invalid import path %s in %s", spec.Path.Value, sf.Name)
			}
			spellings[spelling] = l.canonicalKey(spelling)
		}
	}
	if len(spellings) == 0 {
		return nil
	}
	deps, err := l.ensurePackages(ctx, slices.Sorted(maps.Values(spellings)))
	if err != nil {
		return err
	}
	p.importSpellings = make(map[string]*Package, len(spellings))
	seen := make(map[*Package]bool)
	for _, spelling := range slices.Sorted(maps.Keys(spellings)) {
		dep := deps[spellings[spelling]]
		p.importSpellings[spelling] = dep
		if dep != nil && !seen[dep] {
			seen[dep] = true
			p.imports = append(p.imports, dep)
		}
	}
	return nil
}

// buildFiles compiles a set of parsed CUE files as an ad-hoc package
// (Loader.Build, PkgFiles), resolving and building their imports first.
// The returned vertex is not finalized.
func (l *Loader) buildFiles(ctx context.Context, pkgID string, files []*ast.File) (*adt.Vertex, error) {
	p := &Package{loader: l}
	for i, f := range files {
		name := f.Filename
		if name == "" {
			name = fmt.Sprintf("file%d.cue", i)
		}
		p.files = append(p.files, &SourceFile{Name: name, Syntax: f})
	}
	if err := l.resolveFileImports(ctx, p); err != nil {
		return nil, err
	}
	l.buildMu.Lock()
	defer l.buildMu.Unlock()
	if err := l.buildImportsLocked(ctx, p); err != nil {
		return nil, err
	}
	cfg := &compile.Config{KnownImport: p.knownImport}
	v, cerr := compile.Files(cfg, l.rt, pkgID, files...)
	if cerr != nil {
		return nil, cerr
	}
	return v, nil
}

// buildLocked builds package p: it recursively builds and registers its
// imports, compiles its files, and finalizes and registers the result.
// The result is cached; a cancelled build is not, so that a later call
// can start afresh. buildMu must be held.
func (l *Loader) buildLocked(ctx context.Context, p *Package) error {
	switch p.buildState {
	case buildDone:
		return p.buildErr
	case buildInProgress:
		return fmt.Errorf("import cycle through %s", p.ip)
	}
	p.buildState = buildInProgress
	v, err := l.buildPackageLocked(ctx, p)
	if err != nil && ctx.Err() != nil {
		// Do not cache a cancelled build.
		p.buildState = buildNotStarted
		return err
	}
	p.buildState = buildDone
	p.buildErr = err
	if err != nil {
		return err
	}
	p.vertex = v
	p.value = l.newValue(v)
	// Register the package root so that eval-time import resolution
	// (LoadBuiltin) finds it under its canonical path, and index it for
	// PackageOf.
	l.rt.RegisterPackage(p.ip.String(), v)
	l.mu.Lock()
	l.pkgByVertex[v] = p
	l.mu.Unlock()
	return nil
}

// buildPackageLocked produces the finalized root vertex of p.
func (l *Loader) buildPackageLocked(ctx context.Context, p *Package) (*adt.Vertex, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.isStd {
		v := l.rt.LoadBuiltin(p.ip.String())
		if v == nil {
			return nil, fmt.Errorf("cannot find standard library package %q", p.ip)
		}
		return v, nil
	}
	if p.fixed != nil {
		w, verr, fatal := l.forceVertex(ctx, *p.fixed)
		if fatal != nil {
			return nil, fatal
		}
		_ = verr // an error value is a valid package value; it surfaces on use
		return w, nil
	}
	if len(p.files) == 0 {
		return nil, fmt.Errorf("no files in package %q", p.ip)
	}
	if err := l.buildImportsLocked(ctx, p); err != nil {
		return nil, err
	}
	files := make([]*ast.File, len(p.files))
	for i, sf := range p.files {
		files[i] = sf.Syntax
	}
	cfg := &compile.Config{KnownImport: p.knownImport}
	v, cerr := compile.Files(cfg, l.rt, p.ip.String(), files...)
	if cerr != nil {
		return nil, cerr
	}
	opCtx := l.newOpContext(ctx, v)
	v.Finalize(opCtx)
	opCtx.FlushStats()
	if b := opCtx.Canceled(); b != nil {
		err := ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		return nil, err
	}
	return v, nil
}

// buildImportsLocked builds each import of p and registers it on the
// runtime under the exact spelling used in p's files, so that
// eval-time resolution finds it. Import failures are registered as
// error values so that they surface where the import is used;
// cancellation aborts the build.
func (l *Loader) buildImportsLocked(ctx context.Context, p *Package) error {
	for _, spelling := range slices.Sorted(maps.Keys(p.importSpellings)) {
		dep := p.importSpellings[spelling]
		var w *adt.Vertex
		switch {
		case dep == nil:
			w = errVertexf("cannot resolve import %q", spelling)
		default:
			if err := l.buildLocked(ctx, dep); err != nil {
				if ctx.Err() != nil {
					return err
				}
				w = errVertex(err)
			} else {
				w = dep.vertex
			}
		}
		if err := l.registerSpelling(spelling, dep, w); err != nil {
			return err
		}
	}
	return nil
}

// registerSpelling registers vertex w under an import spelling.
// Because registration is keyed globally on the runtime by the literal
// spelling, two packages whose identical spellings resolve to
// different canonical packages cannot coexist; this is detected and
// reported.
//
// TODO(cueload): scope import spellings per importing package once the
// runtime seam supports it.
func (l *Loader) registerSpelling(spelling string, dep *Package, w *adt.Vertex) error {
	canonical := spelling
	if dep != nil {
		canonical = dep.ip.String()
	}
	l.mu.Lock()
	prev, ok := l.spellings[spelling]
	if !ok {
		l.spellings[spelling] = canonical
	}
	l.mu.Unlock()
	if ok && prev != canonical {
		return fmt.Errorf("conflicting resolutions for import %q: %s and %s", spelling, prev, canonical)
	}
	l.rt.RegisterPackage(spelling, w)
	return nil
}

// knownImport reports whether the compiler may treat an import path in
// p's files as known even though it has no build.Instance: true for
// every import spelling the loader has resolved for p.
func (p *Package) knownImport(importPath string) bool {
	_, ok := p.importSpellings[importPath]
	return ok
}
