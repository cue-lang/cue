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
	"io/fs"
	"maps"
	"os"
	pathpkg "path"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	pkgpath "cuelang.org/go/pkg/path"
)

// separator returns the path separator byte for the given OS.
func separator(os pkgpath.OS) byte {
	if os == pkgpath.Windows {
		return '\\'
	}
	return '/'
}

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

func rewriteFiles(p *build.Instance, root string, isLocal bool, os pkgpath.OS) {
	p.Root = root

	normalizeFiles(p.BuildFiles, os)
	normalizeFiles(p.IgnoredFiles, os)
	normalizeFiles(p.OrphanedFiles, os)
	normalizeFiles(p.InvalidFiles, os)
	normalizeFiles(p.UnknownFiles, os)
}

// setFSLoc records the FS location info in DirLoc/RootLoc
// and, when loading from an [fs.FS] with [Config.FromFSPath],
// maps Dir/Root to display paths.
//
// Dir/Root must hold loader-internal paths when this is called.
// DirLoc is only set on the first call (Dir is set once and not
// overwritten). RootLoc is always updated because rewriteFiles
// may reset Root between calls.
func setFSLoc(c *Config, p *build.Instance) {
	if c.FS != nil {
		if !p.DirLoc.IsSet() {
			p.DirLoc = makeFSLoc(c.FS, p.Dir, c.FromFSPath)
			if c.FromFSPath != nil {
				p.Dir = c.FromFSPath(p.Dir)
			}
		}
		p.RootLoc = makeFSLoc(c.FS, p.Root, c.FromFSPath)
		if c.FromFSPath != nil {
			p.Root = c.FromFSPath(p.Root)
		}
	} else {
		if !p.DirLoc.IsSet() && p.Dir != "" {
			p.DirLoc = makeOSFSLoc(p.Dir, c.pathOS)
		}
		if p.Root != "" {
			p.RootLoc = makeOSFSLoc(p.Root, c.pathOS)
		}
	}
}

// makeFSLoc creates an FSLoc for a loader-internal path in FS mode.
// The loader-internal path is absolute within the FS namespace (e.g. "/foo/bar");
// it strips the leading "/" to produce a valid [fs.FS] path.
func makeFSLoc(fsys fs.FS, loaderPath string, cfgFromFSPath func(string) string) token.FSLoc {
	fsPath := strings.TrimPrefix(loaderPath, "/")
	var fromFSPath func(string) string
	if cfgFromFSPath != nil {
		fromFSPath = func(p string) string { return cfgFromFSPath("/" + p) }
	} else {
		fromFSPath = func(p string) string { return "/" + p }
	}
	return token.FSLoc{
		FS:         fsys,
		Path:       fsPath,
		FromFSPath: fromFSPath,
	}
}

// makeOSFSLoc creates an FSLoc backed by [os.DirFS] for an absolute
// OS path. The FS is rooted at the filesystem root ("/" on Unix,
// the volume root on Windows). The returned FSLoc.Path is a valid
// [fs.FS] path (forward-slash separated, no leading separator).
func makeOSFSLoc(absPath string, pathOS pkgpath.OS) token.FSLoc {
	root := "/"
	if pathOS == pkgpath.Windows {
		vol := pkgpath.VolumeName(absPath, pathOS)
		root = vol + `\`
	}
	fsPath := absPath[len(root):]
	fsPath = strings.ReplaceAll(fsPath, `\`, "/")
	fromFSPath := func(p string) string { return root + p }
	if pathOS == pkgpath.Windows {
		fromFSPath = func(p string) string {
			return root + strings.ReplaceAll(p, "/", `\`)
		}
	}
	return token.FSLoc{
		FS:         os.DirFS(root),
		Path:       fsPath,
		FromFSPath: fromFSPath,
	}
}

// fsDir returns the loader-internal directory path for p.
// When loading from a [Config.FS], Dir may have been mapped
// to a display path, so this uses DirLoc to reconstruct
// the loader-internal path. In the OS case, Dir is unchanged.
func fsDir(c *Config, p *build.Instance) string {
	if c.FS != nil && p.DirLoc.IsSet() {
		return "/" + p.DirLoc.Path
	}
	return p.Dir
}

// normalizeFiles sorts the files so that files contained by a parent directory
// always come before files contained in sub-directories, and that filenames in
// the same directory are sorted lexically byte-wise, like Go's `<` operator.
func normalizeFiles(files []*build.File, os pkgpath.OS) {
	sep := separator(os)
	slices.SortFunc(files, func(a, b *build.File) int {
		fa := a.Filename
		fb := b.Filename
		ca := strings.Count(fa, string(sep))
		cb := strings.Count(fb, string(sep))
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
		fp.err = errors.Append(fp.err, &NoFilesError{Package: p, pathOS: fp.c.pathOS, ignored: len(p.IgnoredFiles) > 0})
		return fp.err
	}

	p.ImportPaths = slices.Sorted(maps.Keys(fp.imported))

	return nil
}

// add adds the given file to the appropriate package in fp.
// It reports whether the file might be considered part of the
// package being loaded, even if it ends up not added to
// the build files, for example because of an @if constraint or
// it's a tool file.
func (fp *fileProcessor) add(root string, file *build.File, mode importMode) bool {
	fullPath := file.Filename
	if fullPath != "-" {
		if !pkgpath.IsAbs(fullPath, fp.c.pathOS) {
			fullPath = pkgpath.Join([]string{root, fullPath}, fp.c.pathOS)
		}
		file.Filename = fullPath
	}

	base := pkgpath.Base(fullPath, fp.c.pathOS)

	// special * and _
	p := fp.pkg // default package

	// sameDir holds whether the file should be considered to be
	// part of the same directory as the default package. This is
	// true when the file is part of the original package directory
	// or when allowExcludedFiles is specified, signifying that the
	// file is part of an explicit set of files provided on the
	// command line.
	sameDir := pkgpath.Dir(fullPath, fp.c.pathOS) == fsDir(fp.c, p) || (mode&allowExcludedFiles) != 0

	// badFile := func(p *build.Instance, err errors.Error) bool {
	badFile := func(err errors.Error) {
		fp.err = errors.Append(fp.err, err)
		file.ExcludeReason = fp.err
		p.InvalidFiles = append(p.InvalidFiles, file)
	}
	if err := setFileSource(fp.c, file); err != nil {
		badFile(errors.Promote(err, ""))
		return false
	}
	// Set FilenameLoc before transforming the filename for display purposes.
	// This preserves the FS path for later FS operations.
	if fp.c.FS != nil {
		file.FilenameLoc = makeFSLoc(fp.c.FS, file.Filename, fp.c.FromFSPath)
	} else {
		file.FilenameLoc = makeOSFSLoc(file.Filename, fp.c.pathOS)
	}
	// Apply FromFSPath to transform the filename for display/position purposes.
	// This must be done after setFileSource (which uses the FS path to read
	// the file) but before getCUESyntax (which uses bf.Filename for positions).
	if fp.c.FromFSPath != nil {
		file.Filename = fp.c.FromFSPath(file.Filename)
	}

	if file.Encoding != build.CUE {
		// Not a CUE file.
		if sameDir {
			p.OrphanedFiles = append(p.OrphanedFiles, file)
		}
		return false
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
				return false
			}
			file.ExcludeReason = errors.Newf(token.NoPos, "filename starts with a '%s'", badPrefix)
			if file.Interpretation == "" {
				p.IgnoredFiles = append(p.IgnoredFiles, file)
			} else {
				p.OrphanedFiles = append(p.OrphanedFiles, file)
			}
			return false
		}
	}
	// Note: when path is "-" (stdin), it will already have
	// been read and file.Source set to the resulting data
	// by setFileSource.
	pf, perr := fp.c.fileSystem.getCUESyntax(file, fp.c.parserConfig)
	if perr != nil {
		badFile(errors.Promote(perr, "add failed"))
		return false
	}
	if file.FilenameLoc.IsSet() {
		if tokFile := pf.Pos().File(); tokFile != nil {
			tokFile.SetFSLoc(file.FilenameLoc)
		}
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
			return false
		}
		if q == nil {
			q = fp.c.Context.NewInstance(p.Dir, nil)
			q.PkgName = pkg
			q.DisplayPath = p.DisplayPath
			q.ImportPath = p.ImportPath + ":" + pkg
			q.Root = p.Root
			q.DirLoc = p.DirLoc
			q.RootLoc = p.RootLoc
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
		return false
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
			return true
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
				badFile(&MultiplePackageError{
					Dir:      p.Dir,
					Packages: []string{p.PkgName, pkg},
					Files:    []string{fp.firstFile, base},
				})
				return false
			}
		}
	}

	isTest := strings.HasSuffix(base, "_test"+cueSuffix)
	isTool := strings.HasSuffix(base, "_tool"+cueSuffix)

	for spec := range pf.ImportSpecs() {
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
func cleanPatterns(patterns []string, os pkgpath.OS) []string {
	if len(patterns) == 0 {
		return []string{"."}
	}
	var out []string
	for _, a := range patterns {
		// Arguments are supposed to be import paths, but
		// as a courtesy to Windows developers, rewrite \ to /
		// in command-line arguments. Handles .\... and so on.
		if separator(os) == '\\' {
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
func hasFilepathPrefix(s, prefix string, os pkgpath.OS) bool {
	sep := separator(os)
	switch {
	default:
		return false
	case len(s) == len(prefix):
		return s == prefix
	case len(s) > len(prefix):
		if prefix != "" && prefix[len(prefix)-1] == sep {
			return strings.HasPrefix(s, prefix)
		}
		return s[len(prefix)] == sep && s[:len(prefix)] == prefix
	}
}
