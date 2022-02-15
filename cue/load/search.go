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
	// TODO: remove this usage

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"io/fs"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
)

// A match represents the result of matching a single package pattern.
type match struct {
	Pattern string // the pattern itself
	Literal bool   // whether it is a literal (no wildcards)
	Pkgs    []*build.Instance
	Err     errors.Error
}

// TODO: should be matched from module file only.
// The pattern is either "all" (all packages), "std" (standard packages),
// "cmd" (standard commands), or a path including "...".
func (l *loader) matchPackages(pattern, pkgName string) *match {

	m := &match{
		Pattern: pattern,
		Literal: false,
	}

	return m
}

// matchPackagesInFS is like allPackages but is passed a pattern
// beginning ./ or ../, meaning it should scan the tree rooted
// at the given directory. There are ... in the pattern too.
// (See go help packages for pattern syntax.)
func (l *loader) matchPackagesInFS(pattern, pkgName string) *match {
	c := l.cfg
	m := &match{
		Pattern: pattern,
		Literal: false,
	}

	fsys := c.FileSystem

	// Find directory to begin the scan.
	// Could be smarter but this one optimization
	// is enough for now, since ... is usually at the
	// end of a path.
	i := strings.Index(pattern, "...")
	dir, _ := pathpkg.Split(pattern[:i])

	root := l.abs(dir)

	// Find new module root from here or check there are no additional
	// cue.mod files between here and the next module.

	if !hasFilepathPrefix(root, c.ModuleRoot) {
		m.Err = errors.Newf(token.NoPos,
			"cue: pattern %s refers to dir %s, outside module root %s",
			pattern, root, c.ModuleRoot)
		return m
	}

	pkgDir := pathpkg.Join(root, modDir)
	// TODO(legacy): remove
	pkgDir2 := pathpkg.Join(root, "pkg")

	_ = fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path == pkgDir || path == pkgDir2 {
			return fs.SkipDir
		}

		top := path == root

		// Avoid .foo, _foo, and testdata directory trees, but do not avoid "." or "..".
		_, elem := pathpkg.Split(path)
		dot := strings.HasPrefix(elem, ".") && elem != "." && elem != ".."
		if dot || strings.HasPrefix(elem, "_") || (elem == "testdata" && !top) {
			return fs.SkipDir
		}

		if !top {
			// Ignore other modules found in subdirectories.
			if _, err := fs.Stat(fsys, pathpkg.Join(path, modDir)); err == nil {
				return fs.SkipDir
			}
		}

		// name := prefix + pathpkg.ToSlash(path)
		// if !match(name) {
		// 	return nil
		// }

		// We keep the directory if we can import it, or if we can't import it
		// due to invalid CUE source files. This means that directories
		// containing parse errors will be built (and fail) instead of being
		// silently skipped as not matching the pattern.
		// Do not take root, as we want to stay relative
		// to one dir only.
		dir, e := filepath.Rel(c.Dir, path)
		dir = filepath.ToSlash(dir)

		if e != nil {
			panic(err)
		} else {
			dir = "./" + dir
		}
		// TODO: consider not doing these checks here.
		inst := c.newRelInstance(token.NoPos, dir, pkgName)
		pkgs := l.importPkg(token.NoPos, inst)
		for _, p := range pkgs {
			if err := p.Err; err != nil && (p == nil || len(p.InvalidFiles) == 0) {
				switch err.(type) {
				case nil:
					break
				case *NoFilesError:
					if c.DataFiles && len(p.OrphanedFiles) > 0 {
						break
					}
					return nil
				default:
					m.Err = errors.Append(m.Err, err)
				}
			}
		}

		m.Pkgs = append(m.Pkgs, pkgs...)
		return nil
	})
	return m
}

// treeCanMatchPattern(pattern)(name) reports whether
// name or children of name can possibly match pattern.
// Pattern is the same limited glob accepted by matchPattern.
func treeCanMatchPattern(pattern string) func(name string) bool {
	wildCard := false
	if i := strings.Index(pattern, "..."); i >= 0 {
		wildCard = true
		pattern = pattern[:i]
	}
	return func(name string) bool {
		return len(name) <= len(pattern) && hasPathPrefix(pattern, name) ||
			wildCard && strings.HasPrefix(name, pattern)
	}
}

// matchPattern(pattern)(name) reports whether
// name matches pattern. Pattern is a limited glob
// pattern in which '...' means 'any string' and there
// is no other special syntax.
// Unfortunately, there are two special cases. Quoting "go help packages":
//
// First, /... at the end of the pattern can match an empty string,
// so that net/... matches both net and packages in its subdirectories, like net/http.
// Second, any slash-separted pattern element containing a wildcard never
// participates in a match of the "vendor" element in the path of a vendored
// package, so that ./... does not match packages in subdirectories of
// ./vendor or ./mycode/vendor, but ./vendor/... and ./mycode/vendor/... do.
// Note, however, that a directory named vendor that itself contains code
// is not a vendored package: cmd/vendor would be a command named vendor,
// and the pattern cmd/... matches it.
func matchPattern(pattern string) func(name string) bool {
	// Convert pattern to regular expression.
	// The strategy for the trailing /... is to nest it in an explicit ? expression.
	// The strategy for the vendor exclusion is to change the unmatchable
	// vendor strings to a disallowed code point (vendorChar) and to use
	// "(anything but that codepoint)*" as the implementation of the ... wildcard.
	// This is a bit complicated but the obvious alternative,
	// namely a hand-written search like in most shell glob matchers,
	// is too easy to make accidentally exponential.
	// Using package regexp guarantees linear-time matching.

	const vendorChar = "\x00"

	if strings.Contains(pattern, vendorChar) {
		return func(name string) bool { return false }
	}

	re := regexp.QuoteMeta(pattern)
	re = replaceVendor(re, vendorChar)
	switch {
	case strings.HasSuffix(re, `/`+vendorChar+`/\.\.\.`):
		re = strings.TrimSuffix(re, `/`+vendorChar+`/\.\.\.`) + `(/vendor|/` + vendorChar + `/\.\.\.)`
	case re == vendorChar+`/\.\.\.`:
		re = `(/vendor|/` + vendorChar + `/\.\.\.)`
	case strings.HasSuffix(re, `/\.\.\.`):
		re = strings.TrimSuffix(re, `/\.\.\.`) + `(/\.\.\.)?`
	}
	re = strings.Replace(re, `\.\.\.`, `[^`+vendorChar+`]*`, -1)

	reg := regexp.MustCompile(`^` + re + `$`)

	return func(name string) bool {
		if strings.Contains(name, vendorChar) {
			return false
		}
		return reg.MatchString(replaceVendor(name, vendorChar))
	}
}

// replaceVendor returns the result of replacing
// non-trailing vendor path elements in x with repl.
func replaceVendor(x, repl string) string {
	if !strings.Contains(x, "vendor") {
		return x
	}
	elem := strings.Split(x, "/")
	for i := 0; i < len(elem)-1; i++ {
		if elem[i] == "vendor" {
			elem[i] = repl
		}
	}
	return strings.Join(elem, "/")
}

// warnUnmatched warns about patterns that didn't match any packages.
func warnUnmatched(matches []*match) {
	for _, m := range matches {
		if len(m.Pkgs) == 0 {
			m.Err =
				errors.Newf(token.NoPos, "cue: %q matched no packages\n", m.Pattern)
		}
	}
}

// importPaths returns the matching paths to use for the given command line.
// It calls ImportPathsQuiet and then WarnUnmatched.
func (l *loader) importPaths(patterns []string) []*match {
	matches := l.importPathsQuiet(patterns)
	warnUnmatched(matches)
	return matches
}

// importPathsQuiet is like ImportPaths but does not warn about patterns with no matches.
func (l *loader) importPathsQuiet(patterns []string) []*match {
	var out []*match
	for _, a := range cleanPatterns(patterns) {
		if isMetaPackage(a) {
			out = append(out, l.matchPackages(a, l.cfg.Package))
			continue
		}

		orig := a
		pkgName := l.cfg.Package
		switch p := strings.IndexByte(a, ':'); {
		case p < 0:
		case p == 0:
			pkgName = a[1:]
			a = "."
		default:
			pkgName = a[p+1:]
			a = a[:p]
		}
		if pkgName == "*" {
			pkgName = ""
		}

		if strings.Contains(a, "...") {
			if isLocalImport(a) {
				out = append(out, l.matchPackagesInFS(a, pkgName))
			} else {
				out = append(out, l.matchPackages(a, pkgName))
			}
			continue
		}

		var p *build.Instance
		if isLocalImport(a) {
			p = l.cfg.newRelInstance(token.NoPos, a, pkgName)
		} else {
			p = l.cfg.newInstance(token.NoPos, importPath(orig))
		}

		pkgs := l.importPkg(token.NoPos, p)
		out = append(out, &match{Pattern: a, Literal: true, Pkgs: pkgs})
	}
	return out
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

		// Put argument in canonical form, but preserve leading ./.
		if strings.HasPrefix(a, "./") {
			a = "./" + pathpkg.Clean(a)
			if a == "./." {
				a = "."
			}
		} else {
			a = pathpkg.Clean(a)
		}
		out = append(out, a)
	}
	return out
}

// isMetaPackage checks if name is a reserved package name that expands to multiple packages.
func isMetaPackage(name string) bool {
	return name == "std" || name == "cmd" || name == "all"
}

// hasPathPrefix reports whether the path s begins with the
// elements in prefix.
func hasPathPrefix(s, prefix string) bool {
	switch {
	default:
		return false
	case len(s) == len(prefix):
		return s == prefix
	case len(s) > len(prefix):
		if prefix != "" && prefix[len(prefix)-1] == '/' {
			return strings.HasPrefix(s, prefix)
		}
		return s[len(prefix)] == '/' && s[:len(prefix)] == prefix
	}
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
		if prefix != "" && prefix[len(prefix)-1] == '/' {
			return strings.HasPrefix(s, prefix)
		}
		return s[len(prefix)] == '/' && s[:len(prefix)] == prefix
	}
}
