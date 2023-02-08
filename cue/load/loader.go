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

// Files in package are to a large extent based on Go files from the following
// Go packages:
//    - cmd/go/internal/load
//    - go/build

import (
	"path/filepath"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"

	// Trigger the unconditional loading of all core builtin packages if load
	// is used. This was deemed the simplest way to avoid having to import
	// this line explicitly, and thus breaking existing code, for the majority
	// of cases, while not introducing an import cycle.
	_ "cuelang.org/go/pkg"
)

// Mode flags for loadImport and download (in get.go).
const (
	// resolveImport means that loadImport should do import path expansion.
	// That is, resolveImport means that the import path came from
	// a source file and has not been expanded yet to account for
	// vendoring or possible module adjustment.
	// Every import path should be loaded initially with resolveImport,
	// and then the expanded version (for example with the /vendor/ in it)
	// gets recorded as the canonical import path. At that point, future loads
	// of that package must not pass resolveImport, because
	// disallowVendor will reject direct use of paths containing /vendor/.
	resolveImport = 1 << iota
)

type loader struct {
	cfg          *Config
	stk          importStack
	tags         []*tag // tags found in files
	buildTags    map[string]bool
	replacements map[ast.Node]ast.Node
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
		Message:     errors.NewMessage(format, args),
	}
	err.fillPos(l.cfg.Dir, importPos)
	return err
}

// cueFilesPackage creates a package for building a collection of CUE files
// (typically named on the command line).
func (l *loader) cueFilesPackage(files []*build.File) *build.Instance {
	pos := token.NoPos
	cfg := l.cfg
	cfg.filesMode = true
	// ModInit() // TODO: support modules
	pkg := l.cfg.Context.NewInstance(cfg.Dir, l.loadFunc())

	for _, bf := range files {
		f := bf.Filename
		if f == "-" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(cfg.Dir, f)
		}
		fi, err := cfg.fileSystem.stat(f)
		if err != nil {
			return cfg.newErrInstance(pos, toImportPath(f),
				errors.Wrapf(err, pos, "could not find file"))
		}
		if fi.IsDir() {
			return cfg.newErrInstance(token.NoPos, toImportPath(f),
				errors.Newf(pos, "file is a directory %v", f))
		}
	}

	fp := newFileProcessor(cfg, pkg)
	for _, file := range files {
		fp.add(pos, cfg.Dir, file, allowAnonymous)
	}

	// TODO: ModImportFromFiles(files)
	pkg.Dir = cfg.Dir
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

	l.addFiles(cfg.Dir, pkg)

	pkg.User = true
	l.stk.Push("user")
	_ = pkg.Complete()
	l.stk.Pop()
	pkg.User = true
	//pkg.LocalPrefix = dirToImportPath(dir)
	pkg.DisplayPath = "command-line-arguments"

	return pkg
}

func (l *loader) addFiles(dir string, p *build.Instance) {
	for _, f := range p.BuildFiles {
		d := encoding.NewDecoder(f, &encoding.Config{
			Stdin:     l.cfg.stdin(),
			ParseFile: l.cfg.ParseFile,
		})
		for ; !d.Done(); d.Next() {
			_ = p.AddSyntax(d.File())
		}
		if err := d.Err(); err != nil {
			p.ReportError(errors.Promote(err, "load"))
		}
		d.Close()
	}
}
