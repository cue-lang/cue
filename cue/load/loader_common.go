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
	"cmp"
	pathpkg "path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

// An importMode controls the behavior of the Import method.
type importMode uint

const (
	// If importComment is set, parse import comments on package statements.
	// Import returns an error if it finds a comment it cannot understand
	// or finds conflicting comments in multiple source files.
	// See golang.org/s/go14customimport for more information.
	importComment importMode = 1 << iota

	allowAnonymous
	allowExcludedFiles
)

func rewriteFiles(p *build.Instance, root string, isLocal bool) {
	p.Root = root

	normalizeFiles(p.BuildFiles)
	normalizeFiles(p.IgnoredFiles)
	normalizeFiles(p.OrphanedFiles)
	normalizeFiles(p.InvalidFiles)
	normalizeFiles(p.UnknownFiles)
}

// normalizeFiles sorts the files so that files contained by a parent directory
// always come before files contained in sub-directories, and that filenames in
// the same directory are sorted lexically byte-wise, like Go's `<` operator.
func normalizeFiles(files []*build.File) {
	slices.SortFunc(files, func(a, b *build.File) int {
		fa := a.Filename
		fb := b.Filename
		ca := strings.Count(fa, string(filepath.Separator))
		cb := strings.Count(fb, string(filepath.Separator))
		if c := cmp.Compare(ca, cb); c != 0 {
			return c
		}
		return cmp.Compare(fa, fb)
	})
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

type fileProcessor struct {
	firstFile        string
	firstCommentFile string
	imported         map[string][]token.Pos
	allTags          map[string]bool
	ignoreOther      bool // ignore files from other packages
	allPackages      bool

	c      *fileProcessorConfig
	tagger *tagger
	pkgs   map[string]*build.Instance
	pkg    *build.Instance

	err errors.Error
}

type fileProcessorConfig = Config

func newFileProcessor(c *fileProcessorConfig, p *build.Instance, tg *tagger) *fileProcessor {
	return &fileProcessor{
		imported: make(map[string][]token.Pos),
		allTags:  make(map[string]bool),
		c:        c,
		pkgs:     map[string]*build.Instance{"_": p},
		pkg:      p,
		tagger:   tg,
	}
}

func countCUEFiles(c *fileProcessorConfig, p *build.Instance) int {
	count := len(p.BuildFiles)
	for _, f := range p.IgnoredFiles {
		if c.Tools && strings.HasSuffix(f.Filename, "_tool.cue") {
			count++
		}
		if c.Tests && strings.HasSuffix(f.Filename, "_test.cue") {
			count++
		}
	}
	return count
}

func (fp *fileProcessor) finalize(p *build.Instance) errors.Error {
	if fp.err != nil {
		return fp.err
	}
	if countCUEFiles(fp.c, p) == 0 &&
		!fp.c.DataFiles &&
		(p.PkgName != "_" || !fp.allPackages) {
		fp.err = errors.Append(fp.err, &NoFilesError{Package: p, ignored: len(p.IgnoredFiles) > 0})
		return fp.err
	}

	for tag := range fp.allTags {
		p.AllTags = append(p.AllTags, tag)
	}
	sort.Strings(p.AllTags)

	p.ImportPaths, _ = cleanImports(fp.imported)

	return nil
}

// add adds the given file to the appropriate package in fp.
func (fp *fileProcessor) add(root string, file *build.File, mode importMode) (added bool) {
	fullPath := file.Filename
	if fullPath != "-" {
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(root, fullPath)
		}
		file.Filename = fullPath
	}

	base := filepath.Base(fullPath)

	// special * and _
	p := fp.pkg // default package

	// badFile := func(p *build.Instance, err errors.Error) bool {
	badFile := func(err errors.Error) bool {
		fp.err = errors.Append(fp.err, err)
		file.ExcludeReason = fp.err
		p.InvalidFiles = append(p.InvalidFiles, file)
		return true
	}
	if err := setFileSource(fp.c, file); err != nil {
		return badFile(errors.Promote(err, ""))
	}

	match, data, err := matchFile(fp.c, file, true, fp.allTags, mode)
	switch {
	case match:

	case err == nil:
		// Not a CUE file.
		p.OrphanedFiles = append(p.OrphanedFiles, file)
		return false

	case !errors.Is(err, errExclude):
		return badFile(err)

	default:
		file.ExcludeReason = err
		if file.Interpretation == "" {
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		} else {
			p.OrphanedFiles = append(p.OrphanedFiles, file)
		}
		return false
	}

	pf, perr := parser.ParseFile(fullPath, data, parser.ImportsOnly)
	if perr != nil {
		badFile(errors.Promote(perr, "add failed"))
		return true
	}

	pkg := pf.PackageName()
	if pkg == "" {
		pkg = "_"
	}
	pos := pf.Pos()

	switch {
	case pkg == p.PkgName, mode&allowAnonymous != 0:
	case fp.allPackages && pkg != "_":
		q := fp.pkgs[pkg]
		if q == nil {
			q = &build.Instance{
				PkgName: pkg,

				Dir:         p.Dir,
				DisplayPath: p.DisplayPath,
				ImportPath:  p.ImportPath + ":" + pkg,
				Root:        p.Root,
				Module:      p.Module,
			}
			fp.pkgs[pkg] = q
		}
		p = q

	case pkg != "_":

	default:
		file.ExcludeReason = excludeError{errors.Newf(pos, "no package name")}
		p.IgnoredFiles = append(p.IgnoredFiles, file)
		return false // don't mark as added
	}

	if !fp.c.AllCUEFiles {
		if err := shouldBuildFile(pf, fp.tagger); err != nil {
			if !errors.Is(err, errExclude) {
				fp.err = errors.Append(fp.err, err)
			}
			file.ExcludeReason = err
			p.IgnoredFiles = append(p.IgnoredFiles, file)
			return false
		}
	}

	if pkg != "" && pkg != "_" {
		if p.PkgName == "" {
			p.PkgName = pkg
			fp.firstFile = base
		} else if pkg != p.PkgName {
			if fp.ignoreOther {
				file.ExcludeReason = excludeError{errors.Newf(pos,
					"package is %s, want %s", pkg, p.PkgName)}
				p.IgnoredFiles = append(p.IgnoredFiles, file)
				return false
			}
			if !fp.allPackages {
				return badFile(&MultiplePackageError{
					Dir:      p.Dir,
					Packages: []string{p.PkgName, pkg},
					Files:    []string{fp.firstFile, base},
				})
			}
		}
	}

	isTest := strings.HasSuffix(base, "_test"+cueSuffix)
	isTool := strings.HasSuffix(base, "_tool"+cueSuffix)

	if mode&importComment != 0 {
		qcom, line := findImportComment(data)
		if line != 0 {
			com, err := strconv.Unquote(qcom)
			if err != nil {
				badFile(errors.Newf(pos, "%s:%d: cannot parse import comment", fullPath, line))
			} else if p.ImportComment == "" {
				p.ImportComment = com
				fp.firstCommentFile = base
			} else if p.ImportComment != com {
				badFile(errors.Newf(pos, "found import comments %q (%s) and %q (%s) in %s", p.ImportComment, fp.firstCommentFile, com, base, p.Dir))
			}
		}
	}

	for _, spec := range pf.Imports {
		quoted := spec.Path.Value
		path, err := strconv.Unquote(quoted)
		if err != nil {
			badFile(errors.Newf(
				spec.Path.Pos(),
				"%s: parser returned invalid quoted string: <%s>", fullPath, quoted,
			))
		}
		if !isTest || fp.c.Tests {
			fp.imported[path] = append(fp.imported[path], spec.Pos())
		}
	}
	switch {
	case isTest:
		if fp.c.Tests {
			p.BuildFiles = append(p.BuildFiles, file)
		} else {
			file.ExcludeReason = excludeError{errors.Newf(pos,
				"_test.cue files excluded in non-test mode")}
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		}
	case isTool:
		if fp.c.Tools {
			p.BuildFiles = append(p.BuildFiles, file)
		} else {
			file.ExcludeReason = excludeError{errors.Newf(pos,
				"_tool.cue files excluded in non-cmd mode")}
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		}
	default:
		p.BuildFiles = append(p.BuildFiles, file)
	}
	return true
}

func findImportComment(data []byte) (s string, line int) {
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

// isLocalImport reports whether the import path is
// a local import path, like ".", "..", "./foo", or "../foo".
func isLocalImport(path string) bool {
	return path == "." || path == ".." ||
		strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// warnUnmatched warns about patterns that didn't match any packages.
func warnUnmatched(matches []*match) {
	for _, m := range matches {
		if len(m.Pkgs) == 0 {
			m.Err = errors.Newf(token.NoPos, "cue: %q matched no packages", m.Pattern)
		}
	}
}

// cleanPatterns returns the patterns to use for the given
// command line. It canonicalizes the patterns but does not
// evaluate any matches.
func cleanPatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return []string{"."}
	}
	var out []string
	for _, a := range patterns {
		// Arguments are supposed to be import paths, but
		// as a courtesy to Windows developers, rewrite \ to /
		// in command-line arguments. Handles .\... and so on.
		if filepath.Separator == '\\' {
			a = strings.Replace(a, `\`, `/`, -1)
		}

		// Put argument in canonical form, but preserve leading "./".
		if strings.HasPrefix(a, "./") {
			a = "./" + pathpkg.Clean(a)
			if a == "./." {
				a = "."
			}
		} else if a != "" {
			a = pathpkg.Clean(a)
		}
		out = append(out, a)
	}
	return out
}

// isMetaPackage checks if name is a reserved package name that expands to multiple packages.
// TODO: none of these package names are actually recognized anywhere else
// and at least one (cmd) doesn't seem like it belongs in the CUE world.
func isMetaPackage(name string) bool {
	return name == "std" || name == "cmd" || name == "all"
}

// hasFilepathPrefix reports whether the path s begins with the
// elements in prefix.
func hasFilepathPrefix(s, prefix string) bool {
	switch {
	default:
		return false
	case len(s) == len(prefix):
		return s == prefix
	case len(s) > len(prefix):
		if prefix != "" && prefix[len(prefix)-1] == filepath.Separator {
			return strings.HasPrefix(s, prefix)
		}
		return s[len(prefix)] == filepath.Separator && s[:len(prefix)] == prefix
	}
}
