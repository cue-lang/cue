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
	"fmt"
	"log"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	build "cuelang.org/go/cue/build"
	"cuelang.org/go/cue/encoding"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
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
func (l *loader) importPkg(path, srcDir string) *build.Instance {
	l.stk.Push(path)
	defer l.stk.Pop()

	cfg := l.cfg
	ctxt := &cfg.fileSystem

	parentPath := path
	if isLocalImport(path) {
		parentPath = filepath.Join(srcDir, path)
	}
	p := cfg.Context.NewInstance(path, l.loadFunc(parentPath))
	p.DisplayPath = path

	isLocal := isLocalImport(path)
	var modDir string
	// var modErr error
	if !isLocal {
		// TODO(mpvl): support module lookup
	}

	p.Local = isLocal

	if err := updateDirs(cfg, p, path, srcDir, 0); err != nil {
		p.ReportError(err)
		return p
	}

	if modDir == "" && path != cleanImport(path) {
		report(p, l.errPkgf(nil,
			"non-canonical import path: %q should be %q", path, pathpkg.Clean(path)))
		p.Incomplete = true
		return p
	}

	fp := newFileProcessor(cfg, p)

	root := p.Dir

	for dir := p.Dir; ctxt.isDir(dir); {
		files, err := ctxt.readDir(dir)
		if err != nil {
			p.ReportError(err)
			return p
		}
		rootFound := false
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if fp.add(dir, f.Name(), importComment) {
				root = dir
			}
			if f.Name() == "cue.mod" {
				root = dir
				rootFound = true
			}
		}

		if rootFound || dir == p.Root || fp.pkg.PkgName == "" {
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

	rewriteFiles(p, root, false)
	if err := fp.finalize(); err != nil {
		p.ReportError(err)
		return p
	}

	for _, f := range p.CUEFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(root, f)
		}
		p.AddFile(f, nil)
	}
	p.Complete()
	return p
}

// loadFunc creates a LoadFunc that can be used to create new build.Instances.
func (l *loader) loadFunc(parentPath string) build.LoadFunc {

	return func(path string) *build.Instance {
		cfg := l.cfg

		// TODO: HACK: for now we don't handle any imports that are not
		// relative paths.
		if !isLocalImport(path) {
			return nil
		}

		if strings.Contains(path, "@") {
			i := cfg.newInstance(path)
			report(i, l.errPkgf(nil,
				"can only use path@version syntax with 'cue get'"))
			return i
		}

		return l.importPkg(path, parentPath)
	}
}

func updateDirs(c *Config, p *build.Instance, path, srcDir string, mode importMode) error {
	ctxt := &c.fileSystem
	// path := p.ImportPath
	if path == "" {
		return fmt.Errorf("import %q: invalid import path", path)
	}

	if isLocalImport(path) {
		if srcDir == "" {
			return fmt.Errorf("import %q: import relative to unknown directory", path)
		}
		if !ctxt.isAbsPath(path) {
			p.Dir = ctxt.joinPath(srcDir, path)
		}
		return nil
	}

	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("import %q: cannot import absolute path", path)
	}

	// TODO: Lookup the import in dir "pkg" at the module root.

	// package was not found
	return fmt.Errorf("cannot find package %q", path)
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
	imported         map[string][]token.Position
	allTags          map[string]bool
	allFiles         bool
	ignoreOther      bool // ignore files from other packages

	c   *Config
	pkg *build.Instance

	err error
}

func newFileProcessor(c *Config, p *build.Instance) *fileProcessor {
	return &fileProcessor{
		imported: make(map[string][]token.Position),
		allTags:  make(map[string]bool),
		c:        c,
		pkg:      p,
	}
}

func (fp *fileProcessor) finalize() error {
	p := fp.pkg
	if fp.err != nil {
		return fp.err
	}
	if len(p.CUEFiles) == 0 && !fp.c.DataFiles {
		return &noCUEError{Package: p, Dir: p.Dir, Ignored: len(p.IgnoredCUEFiles) > 0}
	}

	for tag := range fp.allTags {
		p.AllTags = append(p.AllTags, tag)
	}
	sort.Strings(p.AllTags)

	p.ImportPaths, _ = cleanImports(fp.imported)

	return nil
}

func (fp *fileProcessor) add(root, path string, mode importMode) (added bool) {
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(root, path)
	}
	name := filepath.Base(fullPath)
	dir := filepath.Dir(fullPath)

	fset := token.NewFileSet()
	ext := nameExt(name)
	p := fp.pkg

	badFile := func(err error) bool {
		if fp.err == nil {
			fp.err = err
		}
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

	pf, err := parser.ParseFile(fset, filename, data, parser.ImportsOnly, parser.ParseComments)
	if err != nil {
		return badFile(err)
	}

	pkg := ""
	if pf.Name != nil {
		pkg = pf.Name.Name
	}
	if pkg == "" && mode&allowAnonymous == 0 {
		p.IgnoredCUEFiles = append(p.IgnoredCUEFiles, fullPath)
		return false // don't mark as added
	}

	if fp.c.Package != "" {
		if pkg != fp.c.Package {
			if fp.ignoreOther {
				p.IgnoredCUEFiles = append(p.IgnoredCUEFiles, fullPath)
				return false
			}
			// TODO: package does not conform with requested.
			return badFile(fmt.Errorf("%s: found package %q; want %q", filename, pkg, fp.c.Package))
		}
	} else if fp.firstFile == "" {
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
				badFile(fmt.Errorf("%s:%d: cannot parse import comment", filename, line))
			} else if p.ImportComment == "" {
				p.ImportComment = com
				fp.firstCommentFile = name
			} else if p.ImportComment != com {
				badFile(fmt.Errorf("found import comments %q (%s) and %q (%s) in %s", p.ImportComment, fp.firstCommentFile, com, name, p.Dir))
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
				fp.imported[path] = append(fp.imported[path], fset.Position(spec.Pos()))
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

	// now ready for import comment, a // or /* */ comment
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
	case bytes.HasPrefix(data, slashStar):
		data = data[2:]
		i := bytes.Index(data, starSlash)
		if i < 0 {
			// malformed comment
			return "", 0
		}
		comment = data[:i]
		if bytes.Contains(comment, newline) {
			return "", 0
		}
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
	slashStar  = []byte("/*")
	starSlash  = []byte("*/")
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
			if bytes.HasPrefix(data, slashStar) {
				data = data[2:]
				i := bytes.Index(data, starSlash)
				if i < 0 {
					return nil
				}
				data = data[i+2:]
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

func cleanImports(m map[string][]token.Position) ([]string, map[string][]token.Position) {
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
