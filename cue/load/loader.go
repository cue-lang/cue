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
	pathpkg "path"
	"path/filepath"
	"strings"
	"unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"golang.org/x/xerrors"
)

// Instances returns the instances named by the command line arguments 'args'.
// If errors occur trying to load an instance it is returned with Incomplete
// set. Errors directly related to loading the instance are recorded in this
// instance, but errors that occur loading dependencies are recorded in these
// dependencies.
func Instances(args []string, c *Config) []*build.Instance {
	if c == nil {
		c = &Config{}
	}
	newC, err := c.complete()
	if err != nil {
		return []*build.Instance{c.newErrInstance(token.NoPos, "", err)}
	}
	c = newC

	l := c.loader

	// TODO: require packages to be placed before files. At some point this
	// could be relaxed.
	i := 0
	for ; i < len(args) && isPkg(args[i]); i++ {
	}

	a := []*build.Instance{}

	if len(args) == 0 || i > 0 {
		for _, m := range l.importPaths(args[:i]) {
			if m.Err != nil {
				inst := c.newErrInstance(token.NoPos, "", m.Err)
				a = append(a, inst)
				continue
			}
			a = append(a, m.Pkgs...)
		}
	}

	if args = args[i:]; len(args) > 0 {
		files, err := filetypes.ParseArgs(args[i:])
		if err != nil {
			return []*build.Instance{c.newErrInstance(token.NoPos, "", err)}
		}
		a = append(a, l.cueFilesPackage(files))
	}

	return a
}

func isPkg(s string) bool {
	if s == "." || s == ".." {
		return true
	}
	if s == "-" {
		return false
	}

	// This goes of the assumption that file names may not have a `:` in their
	// name in cue.
	// A filename must have an extension or be preceded by a qualifier argument.
	// So strings of the form foo/bar:baz, where bar is a valid identifier and
	// absolute package
	if p := strings.LastIndexByte(s, ':'); p > 0 {
		if !ast.IsValidIdent(s[p+1:]) {
			return false
		}
		// For a non-pkg, the part before : may only be lowercase and '+'.
		// In addition, a package necessarily must have a slash of some form.
		return strings.ContainsAny(s[:p], `/.\`)
	}

	// Assuming we terminate search for packages once a scoped qualifier is
	// found, we know that any file without an extension (except maybe '-')
	// is invalid. We can therefore assume it is a package.
	// The section may still contain a dot, for instance ./foo/. or ./foo/...
	return strings.TrimLeft(filepath.Ext(s), ".") == ""

	// NOTE/TODO: we have not needed to check whether it is an absolute package
	// or whether the package starts with a dot. Potentially we could thus relax
	// the requirement that packages be dots if it is clear that the package
	// name will not interfere with command names in all circumstances.
}

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
	cfg *Config
	stk importStack
}

func (l *loader) abs(filename string) string {
	if !isLocalImport(filename) {
		return filename
	}
	return filepath.Join(l.cfg.Dir, filename)
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
		if cfg.isDir(f) {
			return cfg.newErrInstance(token.NoPos, toImportPath(f),
				errors.Newf(pos, "file is a directory %v", f))
		}
	}

	// TODO: add fields directly?
	fp := newFileProcessor(cfg, pkg)
	for _, file := range files {
		path := file.Filename
		if !filepath.IsAbs(path) {
			path = filepath.Join(cfg.Dir, path)
		}
		fi, err := cfg.fileSystem.stat(path)
		if err != nil {
			return cfg.newErrInstance(pos, toImportPath(path),
				errors.Wrapf(err, pos, "could not find dir %s", path))
		}
		if fi.IsDir() {
			return cfg.newErrInstance(pos, toImportPath(path),
				errors.Newf(pos, "%s is a directory, should be a CUE file", file.Filename))
		}
		fp.add(pos, cfg.Dir, file, allowAnonymous)
	}

	// TODO: ModImportFromFiles(files)
	_, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return cfg.newErrInstance(pos, toImportPath(cfg.Dir),
			errors.Wrapf(err, pos, "could convert '%s' to absolute path", cfg.Dir))
	}
	pkg.Dir = cfg.Dir
	rewriteFiles(pkg, pkg.Dir, true)
	for _, err := range errors.Errors(fp.finalize()) { // ImportDir(&ctxt, dir, 0)
		var x *NoFilesError
		if len(pkg.OrphanedFiles) > 0 && !xerrors.As(err, &x) {
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
	files := p.CUEFiles
	fs := &l.cfg.fileSystem

	for _, f := range files {
		if !fs.isAbsPath(f) {
			f = fs.joinPath(dir, f)
		}
		r, err := fs.openFile(f)
		if err != nil {
			p.ReportError(err)
		}

		_ = p.AddFile(f, r)
	}
}

func cleanImport(path string) string {
	orig := path
	path = pathpkg.Clean(path)
	if strings.HasPrefix(orig, "./") && path != ".." && !strings.HasPrefix(path, "../") {
		path = "./" + path
	}
	return path
}

// An importStack is a stack of import paths, possibly with the suffix " (test)" appended.
// The import path of a test package is the import path of the corresponding
// non-test package with the suffix "_test" added.
type importStack []string

func (s *importStack) Push(p string) {
	*s = append(*s, p)
}

func (s *importStack) Pop() {
	*s = (*s)[0 : len(*s)-1]
}

func (s *importStack) Copy() []string {
	return append([]string{}, *s...)
}

// shorterThan reports whether sp is shorter than t.
// We use this to record the shortest import sequences
// that leads to a particular package.
func (sp *importStack) shorterThan(t []string) bool {
	s := *sp
	if len(s) != len(t) {
		return len(s) < len(t)
	}
	// If they are the same length, settle ties using string ordering.
	for i := range s {
		if s[i] != t[i] {
			return s[i] < t[i]
		}
	}
	return false // they are equal
}

// reusePackage reuses package p to satisfy the import at the top
// of the import stack stk. If this use causes an import loop,
// reusePackage updates p's error information to record the loop.
func (l *loader) reusePackage(p *build.Instance) *build.Instance {
	// We use p.Internal.Imports==nil to detect a package that
	// is in the midst of its own loadPackage call
	// (all the recursion below happens before p.Internal.Imports gets set).
	if p.ImportPaths == nil {
		if err := lastError(p); err == nil {
			err = l.errPkgf(nil, "import cycle not allowed")
			err.IsImportCycle = true
			report(p, err)
		}
		p.Incomplete = true
	}
	// Don't rewrite the import stack in the error if we have an import cycle.
	// If we do, we'll lose the path that describes the cycle.
	if err := lastError(p); err != nil && !err.IsImportCycle && l.stk.shorterThan(err.ImportStack) {
		err.ImportStack = l.stk.Copy()
	}
	return p
}

// dirToImportPath returns the pseudo-import path we use for a package
// outside the CUE path. It begins with _/ and then contains the full path
// to the directory. If the package lives in c:\home\gopher\my\pkg then
// the pseudo-import path is _/c_/home/gopher/my/pkg.
// Using a pseudo-import path like this makes the ./ imports no longer
// a special case, so that all the code to deal with ordinary imports works
// automatically.
func dirToImportPath(dir string) string {
	return pathpkg.Join("_", strings.Map(makeImportValid, filepath.ToSlash(dir)))
}

func makeImportValid(r rune) rune {
	// Should match Go spec, compilers, and ../../go/parser/parser.go:/isValidImport.
	const illegalChars = `!"#$%&'()*,:;<=>?[\]^{|}` + "`\uFFFD"
	if !unicode.IsGraphic(r) || unicode.IsSpace(r) || strings.ContainsRune(illegalChars, r) {
		return '_'
	}
	return r
}
