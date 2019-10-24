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

import (
	"bytes"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	build "cuelang.org/go/cue/build"
	"cuelang.org/go/cue/encoding"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// An importMode controls the behavior of the Import method.
type importMode uint

const (
	// If findOnly is set, Import stops after locating the directory
	// that should contain the sources for a package. It does not
	// read any files in the directory.
	findOnly importMode = 1 << iota

	// If importComment is set, parse import comments on package statements.
	// Import returns an error if it finds a comment it cannot understand
	// or finds conflicting comments in multiple source files.
	// See golang.org/s/go14customimport for more information.
	importComment

	allowAnonymous
)

// importPkg returns details about the CUE package named by the import path,
// interpreting local import paths relative to the srcDir directory.
// If the path is a local import path naming a package that can be imported
// using a standard import path, the returned package will set p.ImportPath
// to that path.
//
// In the directory and ancestor directories up to including one with a
// cue.mod file, all .cue files are considered part of the package except for:
//
//	- files starting with _ or . (likely editor temporary files)
//	- files with build constraints not satisfied by the context
//
// If an error occurs, importPkg sets the error in the returned instance,
// which then may contain partial information.
//
func (l *loader) importPkg(pos token.Pos, p *build.Instance) *build.Instance {
	l.stk.Push(p.ImportPath)
	defer l.stk.Pop()

	cfg := l.cfg
	ctxt := &cfg.fileSystem

	if p.Err != nil {
		return p
	}

	info, err := ctxt.stat(p.Dir)
	if err != nil || !info.IsDir() {
		// package was not found
		p.Err = errors.Newf(token.NoPos, "cannot find package %q", p.DisplayPath)
		return p
	}

	fp := newFileProcessor(cfg, p)

	// If we have an explicit package name, we can ignore other packages.
	if p.PkgName != "" {
		fp.ignoreOther = true
	}

	if !strings.HasPrefix(p.Dir, cfg.ModuleRoot) {
		panic("")
	}

	var dirs [][2]string
	genDir := GenPath(cfg.ModuleRoot)
	if strings.HasPrefix(p.Dir, genDir) {
		dirs = append(dirs, [2]string{genDir, p.Dir})
		// TODO(legacy): don't support "pkg"
		if filepath.Base(genDir) != "pkg" {
			for _, sub := range []string{"pkg", "src"} {
				rel, err := filepath.Rel(genDir, p.Dir)
				if err != nil {
					// should not happen
					p.Err = errors.Wrapf(err, token.NoPos, "invalid path")
					return p
				}
				base := filepath.Join(cfg.ModuleRoot, modDir, sub)
				dir := filepath.Join(base, rel)
				dirs = append(dirs, [2]string{base, dir})
			}
		}
	} else {
		dirs = append(dirs, [2]string{cfg.ModuleRoot, p.Dir})
	}

	for _, d := range dirs {
		for dir := d[1]; ctxt.isDir(dir); {
			files, err := ctxt.readDir(dir)
			if err != nil {
				p.ReportError(errors.Wrapf(err, pos, "import failed reading dir %v", dir))
				return p
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				fp.add(pos, dir, f.Name(), importComment)
			}

			if filepath.Clean(dir) == d[0] || fp.pkg.PkgName == "" {
				break
			}

			// From now on we just ignore files that do not belong to the same
			// package.
			fp.ignoreOther = true

			parent, _ := filepath.Split(filepath.Clean(dir))
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	impPath, err := addImportQualifier(importPath(p.ImportPath), p.PkgName)
	p.ImportPath = string(impPath)
	if err != nil {
		p.ReportError(err)
	}

	rewriteFiles(p, cfg.ModuleRoot, false)
	if errs := fp.finalize(); errs != nil {
		for _, e := range errors.Errors(errs) {
			p.ReportError(e)
		}
		return p
	}

	for _, f := range p.CUEFiles {
		if !ctxt.isAbsPath(f) {
			f = ctxt.joinPath(cfg.ModuleRoot, f)
		}
		r, err := ctxt.openFile(f)
		if err != nil {
			p.ReportError(err)
		}
		_ = p.AddFile(f, r)
	}
	p.Complete()
	return p
}

// loadFunc creates a LoadFunc that can be used to create new build.Instances.
func (l *loader) loadFunc() build.LoadFunc {

	return func(pos token.Pos, path string) *build.Instance {
		cfg := l.cfg

		impPath := importPath(path)
		if isLocalImport(path) {
			return cfg.newErrInstance(pos, impPath,
				errors.Newf(pos, "relative import paths not allowed (%q)", path))
		}

		// is it a builtin?
		if strings.IndexByte(strings.Split(path, "/")[0], '.') == -1 {
			if l.cfg.StdRoot != "" {
				p := cfg.newInstance(pos, impPath)
				return l.importPkg(pos, p)
			}
			return nil
		}

		p := cfg.newInstance(pos, impPath)
		return l.importPkg(pos, p)
	}
}

func normPrefix(root, path string, isLocal bool) string {
	root = filepath.Clean(root)
	prefix := ""
	if isLocal {
		prefix = "." + string(filepath.Separator)
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) &&
		strings.HasPrefix(path, root) {
		path = prefix + path[len(root)+1:]
	}
	return path
}

func rewriteFiles(p *build.Instance, root string, isLocal bool) {
	p.Root = root
	for i, path := range p.CUEFiles {
		p.CUEFiles[i] = normPrefix(root, path, isLocal)
		sortParentsFirst(p.CUEFiles)
	}
	for i, path := range p.TestCUEFiles {
		p.TestCUEFiles[i] = normPrefix(root, path, isLocal)
		sortParentsFirst(p.TestCUEFiles)
	}
	for i, path := range p.ToolCUEFiles {
		p.ToolCUEFiles[i] = normPrefix(root, path, isLocal)
		sortParentsFirst(p.ToolCUEFiles)
	}
	for i, path := range p.IgnoredCUEFiles {
		if strings.HasPrefix(path, root) {
			p.IgnoredCUEFiles[i] = normPrefix(root, path, isLocal)
		}
	}
	for i, path := range p.InvalidCUEFiles {
		p.InvalidCUEFiles[i] = normPrefix(root, path, isLocal)
		sortParentsFirst(p.InvalidCUEFiles)
	}
}

func sortParentsFirst(s []string) {
	sort.Slice(s, func(i, j int) bool {
		return len(filepath.Dir(s[i])) < len(filepath.Dir(s[j]))
	})
}

type fileProcessor struct {
	firstFile        string
	firstCommentFile string
	imported         map[string][]token.Pos
	allTags          map[string]bool
	allFiles         bool
	ignoreOther      bool // ignore files from other packages

	c   *Config
	pkg *build.Instance

	err errors.Error
}

func newFileProcessor(c *Config, p *build.Instance) *fileProcessor {
	return &fileProcessor{
		imported: make(map[string][]token.Pos),
		allTags:  make(map[string]bool),
		c:        c,
		pkg:      p,
	}
}

func countCUEFiles(c *Config, p *build.Instance) int {
	count := len(p.CUEFiles)
	if c.Tools {
		count += len(p.ToolCUEFiles)
	}
	if c.Tests {
		count += len(p.TestCUEFiles)
	}
	return count
}

func (fp *fileProcessor) finalize() errors.Error {
	p := fp.pkg
	if fp.err != nil {
		return fp.err
	}
	if countCUEFiles(fp.c, p) == 0 && !fp.c.DataFiles {
		fp.err = errors.Append(fp.err, &noCUEError{Package: p, Dir: p.Dir, Ignored: len(p.IgnoredCUEFiles) > 0})
		return fp.err
	}

	for tag := range fp.allTags {
		p.AllTags = append(p.AllTags, tag)
	}
	sort.Strings(p.AllTags)

	p.ImportPaths, _ = cleanImports(fp.imported)

	return nil
}

func (fp *fileProcessor) add(pos token.Pos, root, path string, mode importMode) (added bool) {
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(root, path)
	}
	name := filepath.Base(fullPath)
	dir := filepath.Dir(fullPath)

	ext := nameExt(name)
	p := fp.pkg

	badFile := func(err errors.Error) bool {
		fp.err = errors.Append(fp.err, err)
		p.InvalidCUEFiles = append(p.InvalidCUEFiles, fullPath)
		return true
	}

	match, data, filename, err := matchFile(fp.c, dir, name, true, fp.allFiles, fp.allTags)
	if err != nil {
		return badFile(err)
	}
	if !match {
		if ext == cueSuffix {
			p.IgnoredCUEFiles = append(p.IgnoredCUEFiles, fullPath)
		} else if encoding.MapExtension(ext) != nil {
			p.DataFiles = append(p.DataFiles, fullPath)
		}
		return false // don't mark as added
	}

	pf, perr := parser.ParseFile(filename, data, parser.ImportsOnly, parser.ParseComments)
	if perr != nil {
		badFile(errors.Promote(perr, "add failed"))
		return true
	}

	_, pkg, _ := internal.PackageInfo(pf)
	if pkg == "" && mode&allowAnonymous == 0 {
		p.IgnoredCUEFiles = append(p.IgnoredCUEFiles, fullPath)
		return false // don't mark as added
	}

	if p.PkgName == "" {
		p.PkgName = pkg
		fp.firstFile = name
	} else if pkg != p.PkgName {
		if fp.ignoreOther {
			p.IgnoredCUEFiles = append(p.IgnoredCUEFiles, fullPath)
			return false
		}
		return badFile(&multiplePackageError{
			Dir:      p.Dir,
			Packages: []string{p.PkgName, pkg},
			Files:    []string{fp.firstFile, name},
		})
	}

	isTest := strings.HasSuffix(name, "_test"+cueSuffix)
	isTool := strings.HasSuffix(name, "_tool"+cueSuffix)

	if mode&importComment != 0 {
		qcom, line := findimportComment(data)
		if line != 0 {
			com, err := strconv.Unquote(qcom)
			if err != nil {
				badFile(errors.Newf(pos, "%s:%d: cannot parse import comment", filename, line))
			} else if p.ImportComment == "" {
				p.ImportComment = com
				fp.firstCommentFile = name
			} else if p.ImportComment != com {
				badFile(errors.Newf(pos, "found import comments %q (%s) and %q (%s) in %s", p.ImportComment, fp.firstCommentFile, com, name, p.Dir))
			}
		}
	}

	for _, decl := range pf.Decls {
		d, ok := decl.(*ast.ImportDecl)
		if !ok {
			continue
		}
		for _, spec := range d.Specs {
			quoted := spec.Path.Value
			path, err := strconv.Unquote(quoted)
			if err != nil {
				log.Panicf("%s: parser returned invalid quoted string: <%s>", filename, quoted)
			}
			if !isTest || fp.c.Tests {
				fp.imported[path] = append(fp.imported[path], spec.Pos())
			}
			if !isTool && strings.HasPrefix(path, "tool/") {
				badFile(errors.Newf(spec.Pos(), "%s may only be imported in *_tool.cue files", quoted))
			}
		}
	}
	switch {
	case isTest:
		p.TestCUEFiles = append(p.TestCUEFiles, fullPath)
	case isTool:
		p.ToolCUEFiles = append(p.ToolCUEFiles, fullPath)
	default:
		p.CUEFiles = append(p.CUEFiles, fullPath)
	}
	return true
}

func nameExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[i:]
}

// hasCUEFiles reports whether dir contains any files with names ending in .go.
// For a vendor check we must exclude directories that contain no .go files.
// Otherwise it is not possible to vendor just a/b/c and still import the
// non-vendored a/b. See golang.org/issue/13832.
func hasCUEFiles(ctxt *fileSystem, dir string) bool {
	ents, _ := ctxt.readDir(dir)
	for _, ent := range ents {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), cueSuffix) {
			return true
		}
	}
	return false
}

func findimportComment(data []byte) (s string, line int) {
	// expect keyword package
	word, data := parseWord(data)
	if string(word) != "package" {
		return "", 0
	}

	// expect package name
	_, data = parseWord(data)

	// now ready for import comment, a // comment
	// beginning and ending on the current line.
	for len(data) > 0 && (data[0] == ' ' || data[0] == '\t' || data[0] == '\r') {
		data = data[1:]
	}

	var comment []byte
	switch {
	case bytes.HasPrefix(data, slashSlash):
		i := bytes.Index(data, newline)
		if i < 0 {
			i = len(data)
		}
		comment = data[2:i]
	}
	comment = bytes.TrimSpace(comment)

	// split comment into `import`, `"pkg"`
	word, arg := parseWord(comment)
	if string(word) != "import" {
		return "", 0
	}

	line = 1 + bytes.Count(data[:cap(data)-cap(arg)], newline)
	return strings.TrimSpace(string(arg)), line
}

var (
	slashSlash = []byte("//")
	newline    = []byte("\n")
)

// skipSpaceOrComment returns data with any leading spaces or comments removed.
func skipSpaceOrComment(data []byte) []byte {
	for len(data) > 0 {
		switch data[0] {
		case ' ', '\t', '\r', '\n':
			data = data[1:]
			continue
		case '/':
			if bytes.HasPrefix(data, slashSlash) {
				i := bytes.Index(data, newline)
				if i < 0 {
					return nil
				}
				data = data[i+1:]
				continue
			}
		}
		break
	}
	return data
}

// parseWord skips any leading spaces or comments in data
// and then parses the beginning of data as an identifier or keyword,
// returning that word and what remains after the word.
func parseWord(data []byte) (word, rest []byte) {
	data = skipSpaceOrComment(data)

	// Parse past leading word characters.
	rest = data
	for {
		r, size := utf8.DecodeRune(rest)
		if unicode.IsLetter(r) || '0' <= r && r <= '9' || r == '_' {
			rest = rest[size:]
			continue
		}
		break
	}

	word = data[:len(data)-len(rest)]
	if len(word) == 0 {
		return nil, nil
	}

	return word, rest
}

func cleanImports(m map[string][]token.Pos) ([]string, map[string][]token.Pos) {
	all := make([]string, 0, len(m))
	for path := range m {
		all = append(all, path)
	}
	sort.Strings(all)
	return all, m
}

// // Import is shorthand for Default.Import.
// func Import(path, srcDir string, mode ImportMode) (*Package, error) {
// 	return Default.Import(path, srcDir, mode)
// }

// // ImportDir is shorthand for Default.ImportDir.
// func ImportDir(dir string, mode ImportMode) (*Package, error) {
// 	return Default.ImportDir(dir, mode)
// }

var slashslash = []byte("//")

// isLocalImport reports whether the import path is
// a local import path, like ".", "..", "./foo", or "../foo".
func isLocalImport(path string) bool {
	return path == "." || path == ".." ||
		strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}
