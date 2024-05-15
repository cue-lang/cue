// Copyright 2018 The CUE Authors
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

package load

// Files in this package are to a large extent based on Go files from the following
// Go packages:
//    - cmd/go/internal/load
//    - go/build

import (
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/mod/modpkgload"

	// Trigger the unconditional loading of all core builtin packages if load
	// is used. This was deemed the simplest way to avoid having to import
	// this line explicitly, and thus breaking existing code, for the majority
	// of cases, while not introducing an import cycle.
	_ "cuelang.org/go/pkg"
)

type loader struct {
	cfg    *Config
	tagger *tagger
	stk    importStack
	pkgs   *modpkgload.Packages

	// syntaxCache caches the work involved when decoding a file into an *ast.File.
	// This can happen multiple times for the same file, for example when it is present in
	// multiple different build instances in the same directory hierarchy.
	syntaxCache *syntaxCache

	// dirCachedBuildFiles caches the work involved when reading a directory
	// and determining what build files it contains.
	// It is keyed by directory name.
	// When we descend into subdirectories to load patterns such as ./...
	// we often end up loading parent directories many times over;
	// this cache amortizes that work.
	dirCachedBuildFiles map[string]cachedFileFiles
}

type cachedFileFiles struct {
	err          errors.Error
	buildFiles   []*build.File
	unknownFiles []*build.File
}

func newLoader(c *Config, tg *tagger, syntaxCache *syntaxCache, pkgs *modpkgload.Packages) *loader {
	return &loader{
		cfg:                 c,
		tagger:              tg,
		pkgs:                pkgs,
		dirCachedBuildFiles: map[string]cachedFileFiles{},
		syntaxCache:         syntaxCache,
	}
}

func (l *loader) abs(filename string) string {
	if !isLocalImport(filename) {
		return filename
	}
	return filepath.Join(l.cfg.Dir, filename)
}

func (l *loader) errPkgf(importPos []token.Pos, format string, args ...interface{}) *PackageError {
	err := &PackageError{
		ImportStack: l.stk.Copy(),
		Message:     errors.NewMessagef(format, args...),
	}
	err.fillPos(l.cfg.Dir, importPos)
	return err
}

// cueFilesPackage creates a package for building a collection of CUE files
// (typically named on the command line).
func (l *loader) cueFilesPackage(files []*build.File) *build.Instance {
	// ModInit() // TODO: support modules
	pkg := l.cfg.Context.NewInstance(l.cfg.Dir, l.loadFunc)

	for _, bf := range files {
		f := bf.Filename
		if f == "-" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(l.cfg.Dir, f)
		}
		fi, err := l.cfg.fileSystem.stat(f)
		if err != nil {
			return l.cfg.newErrInstance(errors.Wrapf(err, token.NoPos, "could not find file %v", f))
		}
		if fi.IsDir() {
			return l.cfg.newErrInstance(errors.Newf(token.NoPos, "file is a directory %v", f))
		}
	}

	fp := newFileProcessor(l.cfg, pkg, l.tagger)
	if l.cfg.Package == "*" {
		fp.allPackages = true
		pkg.PkgName = "_"
	}
	for _, bf := range files {
		fp.add(l.cfg.Dir, bf, allowAnonymous|allowExcludedFiles)
	}

	// TODO: ModImportFromFiles(files)
	pkg.Dir = l.cfg.Dir
	rewriteFiles(pkg, pkg.Dir, true)
	for _, err := range errors.Errors(fp.finalize(pkg)) { // ImportDir(&ctxt, dir, 0)
		var x *NoFilesError
		if len(pkg.OrphanedFiles) == 0 || !errors.As(err, &x) {
			pkg.ReportError(err)
		}
	}
	// TODO: Support module importing.
	// if ModDirImportPath != nil {
	// 	// Use the effective import path of the directory
	// 	// for deciding visibility during pkg.load.
	// 	bp.ImportPath = ModDirImportPath(dir)
	// }

	pkg.User = true
	l.addFiles(pkg)

	_ = pkg.Complete()
	pkg.DisplayPath = "command-line-arguments"

	return pkg
}

// addFiles populates p.Files by reading CUE syntax from p.BuildFiles.
func (l *loader) addFiles(p *build.Instance) {
	for _, bf := range p.BuildFiles {
		files, err := l.syntaxCache.getSyntax(bf)
		if err != nil {
			p.ReportError(errors.Promote(err, "load"))
		}
		for _, f := range files {
			_ = p.AddSyntax(f)
		}
	}
}

type syntaxCache struct {
	config encoding.Config
	ctx    *cue.Context
	cache  map[string]syntaxCacheEntry
}

type syntaxCacheEntry struct {
	err   error
	files []*ast.File
}

func newSyntaxCache(cfg *Config) *syntaxCache {
	return &syntaxCache{
		config: encoding.Config{
			Stdin:     cfg.stdin(),
			ParseFile: cfg.ParseFile,
		},
		ctx:   cuecontext.New(),
		cache: make(map[string]syntaxCacheEntry),
	}
}

// getSyntax returns the CUE syntax corresponding to the file argument f.
func (c *syntaxCache) getSyntax(bf *build.File) ([]*ast.File, error) {
	syntax, ok := c.cache[bf.Filename]
	if ok {
		return syntax.files, syntax.err
	}
	d := encoding.NewDecoder(c.ctx, bf, &c.config)
	for ; !d.Done(); d.Next() {
		syntax.files = append(syntax.files, d.File())
	}
	d.Close()
	syntax.err = d.Err()
	c.cache[bf.Filename] = syntax
	return syntax.files, syntax.err
}
