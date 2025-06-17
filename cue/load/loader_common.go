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
	"cmp"
	"maps"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// An importMode controls the behavior of the Import method.
type importMode uint

const (
	allowAnonymous = 1 << iota
	allowExcludedFiles
)

var errExclude = errors.New("file rejected")

type cueError = errors.Error
type excludeError struct {
	cueError
}

func (e excludeError) Is(err error) bool { return err == errExclude }

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
	return slices.Clone(*s)
}

type fileProcessor struct {
	firstFile   string
	imported    map[string][]token.Pos
	ignoreOther bool // ignore files from other packages
	allPackages bool

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

	p.ImportPaths = slices.Sorted(maps.Keys(fp.imported))

	return nil
}

// add adds the given file to the appropriate package in fp.
func (fp *fileProcessor) add(root string, file *build.File, mode importMode) {
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

	// sameDir holds whether the file should be considered to be
	// part of the same directory as the default package. This is
	// true when the file is part of the original package directory
	// or when allowExcludedFiles is specified, signifying that the
	// file is part of an explicit set of files provided on the
	// command line.
	sameDir := filepath.Dir(fullPath) == p.Dir || (mode&allowExcludedFiles) != 0

	// badFile := func(p *build.Instance, err errors.Error) bool {
	badFile := func(err errors.Error) {
		fp.err = errors.Append(fp.err, err)
		file.ExcludeReason = fp.err
		p.InvalidFiles = append(p.InvalidFiles, file)
	}
	if err := setFileSource(fp.c, file); err != nil {
		badFile(errors.Promote(err, ""))
		return
	}

	if file.Encoding != build.CUE {
		// Not a CUE file.
		if sameDir {
			p.OrphanedFiles = append(p.OrphanedFiles, file)
		}
		return
	}
	if (mode & allowExcludedFiles) == 0 {
		var badPrefix string
		for _, prefix := range []string{".", "_"} {
			if strings.HasPrefix(base, prefix) {
				badPrefix = prefix
			}
		}
		if badPrefix != "" {
			if !sameDir {
				return
			}
			file.ExcludeReason = errors.Newf(token.NoPos, "filename starts with a '%s'", badPrefix)
			if file.Interpretation == "" {
				p.IgnoredFiles = append(p.IgnoredFiles, file)
			} else {
				p.OrphanedFiles = append(p.OrphanedFiles, file)
			}
			return
		}
	}
	// Note: when path is "-" (stdin), it will already have
	// been read and file.Source set to the resulting data
	// by setFileSource.
	pf, perr := fp.c.fileSystem.getCUESyntax(file, fp.c.parserConfig)
	if perr != nil {
		badFile(errors.Promote(perr, "add failed"))
		return
	}

	pkg := pf.PackageName()
	if pkg == "" {
		pkg = "_"
	}
	pos := pf.Pos()

	switch {
	case pkg == p.PkgName && (sameDir || pkg != "_"):
		// We've got the exact package that's being looked for.
		// It will already be present in fp.pkgs.
	case mode&allowAnonymous != 0 && sameDir:
		// It's an anonymous file that's not in a parent directory.
	case fp.allPackages && pkg != "_":
		q := fp.pkgs[pkg]
		if q == nil && !sameDir {
			// It's a file in a parent directory that doesn't correspond
			// to a package in the original directory.
			return
		}
		if q == nil {
			q = fp.c.Context.NewInstance(p.Dir, nil)
			q.PkgName = pkg
			q.DisplayPath = p.DisplayPath
			q.ImportPath = p.ImportPath + ":" + pkg
			q.Root = p.Root
			q.Module = p.Module
			q.ModuleFile = p.ModuleFile
			fp.pkgs[pkg] = q
		}
		p = q

	case pkg != "_":
		// We're loading a single package and we either haven't matched
		// the earlier selected package or we haven't selected a package
		// yet. In either case, the default package is the one we want to use.
	default:
		if sameDir {
			file.ExcludeReason = excludeError{errors.Newf(pos, "no package name")}
			p.IgnoredFiles = append(p.IgnoredFiles, file)
		}
		return
	}

	if !fp.c.AllCUEFiles {
		tagIsSet := fp.tagger.tagIsSet
		if p.Module != "" && p.Module != fp.c.Module {
			// The file is outside the main module so treat all build tag keys as unset.
			// Note that if there's no module, we don't consider it to be outside
			// the main module, because otherwise @if tags in non-package files
			// explicitly specified on the command line will not work.
			tagIsSet = func(string) bool {
				return false
			}
		}
		if err := shouldBuildFile(pf, tagIsSet); err != nil {
			if !errors.Is(err, errExclude) {
				fp.err = errors.Append(fp.err, err)
			}
			file.ExcludeReason = err
			p.IgnoredFiles = append(p.IgnoredFiles, file)
			return
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
				return
			}
			if !fp.allPackages {
				badFile(&MultiplePackageError{
					Dir:      p.Dir,
					Packages: []string{p.PkgName, pkg},
					Files:    []string{fp.firstFile, base},
				})
				return
			}
		}
	}

	isTest := strings.HasSuffix(base, "_test"+cueSuffix)
	isTool := strings.HasSuffix(base, "_tool"+cueSuffix)

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
