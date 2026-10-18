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

package cmd

import (
	"bytes"
	goerrors "errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/mod/modimports"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/tools/fix"
)

// selectedFile is a file of the module as the enumerator yields it, with
// the package clause it parsed, or the reason the file is left alone.
type selectedFile struct {
	modimports.ModuleFile

	// skip, when set, is why the entry is reported as skipped and not
	// read: it cannot be read, or it is a symbolic link that does not
	// resolve. The module could not evaluate it before either, so the
	// skip does not block the bump.
	skip error

	// fail, when set, is why the entry is reported as failed: a symbolic
	// link to a directory, which is not followed, or to a file outside
	// the module or inside a nested module, which is not written through.
	// The loader reads such files, so leaving them unconverted would
	// break the module at the new version; the failure blocks the bump
	// unless --force is given.
	fail error
}

// moduleFiles returns the CUE files of the module rooted at root that
// cue fix and cue mod upgrade rewrite, with paths slash-separated and
// relative to root, each with its package clause parsed or the error that
// parse produced. They are found by the enumerator behind the loader's
// ./... expansion, without loading a package, so a package that does not
// load, because an import cannot be resolved or a directory mixes
// packages, contributes its files all the same.
//
// Selected are the files of the main tree, leaving out only directories
// whose name starts with ".", nested modules (a directory holding its own
// cue.mod) and cue.mod itself, and the files of the cue.mod/gen,
// cue.mod/pkg and cue.mod/usr trees, where a cue.mod inside a vendored copy
// of a module is no boundary. Directories named "_"-something or testdata
// are taken too, unlike by the ./... pattern: their packages can still be
// imported, so the module evaluates them. Files are those ending in .cue
// whose name does not start with "." or "_", _test.cue and _tool.cue files
// included. A directory's files come before its subdirectories, each in
// name order.
//
// A symbolic link to a file is read through, as the loader reads it: when
// its target is a file of the module, the target is converted once, under
// its own path, and the link is dropped here; when the target lies
// outside the module or inside a nested module, the link is returned as
// failed, and nothing is written through it. A link to a directory is not
// followed and returned as failed as well. A link that does not resolve,
// or an entry that cannot be read, is returned as skipped. The root itself
// may be reached through a link, as may cue.mod or any directory: a file
// below a linked directory is a file of the module like any other.
//
// A file the enumerator cannot parse is returned without syntax; the
// rewrite parses it again at the module's language version and reports
// that error, since the enumerator's parse, up to the imports, has no
// version.
func moduleFiles(root string) ([]selectedFile, error) {
	// The working directory may be a path through a symbolic link, which
	// the enumerator would report as a link rather than walk. The files
	// are the same either way.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	var files []selectedFile
	for f, err := range modimports.AllModuleFiles(os.DirFS(root), ".", modimports.WalkOptions{
		SkipDir: func(p string) bool {
			return strings.HasPrefix(path.Base(p), ".")
		},
		IncludeCueModTrees: true,
		ReportLinkedDirs:   true,
	}) {
		if err != nil {
			entry := selectedFile{ModuleFile: modimports.ModuleFile{FilePath: f.FilePath}}
			var cueErr errors.Error
			switch {
			case goerrors.Is(err, modimports.ErrLinkedDir):
				entry.fail = modimports.ErrLinkedDir
			case strings.HasSuffix(f.FilePath, ".cue") && goerrors.As(err, &cueErr):
				// A CUE file that does not parse is yielded as an error
				// naming it, and is still a file to rewrite and report.
			default:
				// The file system's: an entry that cannot be read, a
				// link that does not resolve.
				entry.skip = unwrapPathError(err)
			}
			files = append(files, entry)
			continue
		}
		files = append(files, selectedFile{ModuleFile: f})
	}
	return resolveLinks(root, files)
}

// unwrapPathError strips the path from a file system error, which the
// report prints in front of the message already.
func unwrapPathError(err error) error {
	var pe *fs.PathError
	if goerrors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return err
}

// resolveLinks settles the files that are symbolic links: one whose
// target is among the files is dropped, since the target is converted
// under its own path; one whose target lies outside the module or inside
// a nested module is marked as failed; one that does not resolve is marked
// as skipped; any other is kept and converted through the link, as the
// loader would read it. Only an entry that is itself a link counts: a file
// below a linked directory is a file of the module like any other.
func resolveLinks(root string, files []selectedFile) ([]selectedFile, error) {
	selected := make(map[string]bool, len(files))
	for _, f := range files {
		selected[f.FilePath] = true
	}
	kept := files[:0]
	for _, f := range files {
		if f.skip != nil || f.fail != nil {
			kept = append(kept, f)
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(f.FilePath))
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			kept = append(kept, f)
			continue
		}
		target, err := filepath.EvalSymlinks(p)
		if err != nil {
			f.skip = fmt.Errorf("symbolic link does not resolve: %w", unwrapPathError(err))
			kept = append(kept, f)
			continue
		}
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			f.fail = fmt.Errorf("symbolic link to a file outside the module is not written through")
			kept = append(kept, f)
			continue
		}
		if selected[filepath.ToSlash(rel)] {
			continue
		}
		// A target inside a nested module belongs to that module, which
		// has a version of its own; writing through the link would
		// convert it for this one.
		nested, err := inNestedModule(root, rel)
		if err != nil {
			return nil, err
		}
		if nested {
			f.fail = fmt.Errorf("symbolic link into a nested module is not written through")
			kept = append(kept, f)
			continue
		}
		kept = append(kept, f)
	}
	return kept, nil
}

// inNestedModule reports whether rel, relative to root, lies below a
// directory of the main tree that holds its own cue.mod. The cue.mod
// trees have no such boundaries (see modimports.WalkOptions).
func inNestedModule(root, rel string) (bool, error) {
	if rel == "cue.mod" || strings.HasPrefix(rel, "cue.mod"+string(filepath.Separator)) {
		return false, nil
	}
	for dir := filepath.Dir(rel); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(root, dir, "cue.mod"))
		if err == nil {
			return true, nil
		}
		if !goerrors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

// parseModuleFile reads and parses root's cue.mod/module.cue, returning it
// with its raw contents and the language version it declares.
func parseModuleFile(root string) (mf *modfile.File, data []byte, version string, err error) {
	path := filepath.Join(root, "cue.mod", "module.cue")
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	mf, err = modfile.Parse(data, path)
	if err != nil {
		return nil, nil, "", suggestModCommand(err)
	}
	if mf.Language == nil || mf.Language.Version == "" {
		return nil, nil, "", suggestModCommand(modfile.ErrNoLanguageVersion)
	}
	return mf, data, mf.Language.Version, nil
}

// rewriteOutcome is what became of one file of the module.
type rewriteOutcome int

const (
	// outcomePending: the file's package has not been processed; a run
	// that stops early leaves files in this state, which the report
	// passes over.
	outcomePending rewriteOutcome = iota
	// outcomeSkipped: the parser rejects the file at the module's language
	// version, or it is a symbolic link that must not be written through,
	// so it is left as it is. It is reported, and never blocks.
	outcomeSkipped
	// outcomeFailed: the file parsed but could not be rewritten, or its
	// rewritten form does not parse at the target version.
	outcomeFailed
	// outcomeUnchanged: the rewrite produced the file's own bytes.
	outcomeUnchanged
	// outcomeConverted: the rewrite produced new bytes to write.
	outcomeConverted
)

// rewrittenFile is one file of the module and what the rewrite made of it.
type rewrittenFile struct {
	rel  string // slash-separated, relative to the module root
	path string // absolute
	pkg  string // the package clause, or "" for none

	// out holds the converted form; it is kept only when it differs from
	// the source. The syntax tree is let go as soon as it is formatted, so
	// that a module of many large files is held one package at a time.
	out []byte

	outcome rewriteOutcome
	errs    errors.Error // the parse error when skipped, the rewrite errors when failed

	// marked reports that the rewrite added TODO(cue-fix) comments: places
	// whose meaning changed, for the author to review.
	marked bool
	// written reports that the converted form reached disk.
	written bool
}

// rewriteInstances is what rewrites the files of the module's packages. It
// is a variable so that tests can observe the command's behavior on a
// rewrite that fails, which no file that parses produces today.
var rewriteInstances = fix.Instances

// rewriteModule parses every file of the module rooted at root at language
// version version, rewrites each package's files with opts, and checks
// that every rewritten file parses at version target. It writes nothing.
//
// Files are grouped into packages by package clause across the module,
// which is a superset of every instance the loader forms (a package
// inherits its parent directories' files of the same name, and one import
// path merges its cue.mod/gen, pkg and usr directories), so that the
// sanitizer sees every file that can share a package. The packages are
// rewritten one at a time, and a file's syntax tree is let go once its
// converted form is known, so that what is held at once is one package's
// trees and the converted bytes: a generated module can hold thousands of
// large files. An error the rewrite reports is attributed to the file its
// position names; one without a position, or naming no file of the
// module, concerns the run and is returned as the error, with the results
// collected so far, so that the caller can still report them.
func rewriteModule(root string, mf *modfile.File, version, target string, opts ...fix.Option) ([]*rewrittenFile, error) {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	mfiles, err := moduleFiles(root)
	if err != nil {
		return nil, err
	}

	// Pass 1: group the files by the package clause the enumerator parsed;
	// the full parse waits for the file's package's turn.
	var results []*rewrittenFile
	byPath := map[string]*rewrittenFile{}
	byPkg := map[string][]*rewrittenFile{}
	var pkgOrder []string
	for _, mfile := range mfiles {
		r := &rewrittenFile{rel: mfile.FilePath, path: filepath.Join(root, filepath.FromSlash(mfile.FilePath)), outcome: outcomePending}
		results = append(results, r)
		byPath[r.path] = r
		if mfile.skip != nil {
			r.outcome = outcomeSkipped
			r.errs = errors.Promote(mfile.skip, "")
			continue
		}
		if mfile.fail != nil {
			r.outcome = outcomeFailed
			r.errs = errors.Promote(mfile.fail, "")
			continue
		}
		var key string
		switch {
		case mfile.Syntax == nil:
			// A file that did not parse is a package of its own.
			key = "\x00\x00" + r.rel
		case mfile.Syntax.PackageName() == "":
			// Files without a package clause form the anonymous package
			// of their directory, as the loader combines them.
			key = "\x00" + path.Dir(r.rel)
		default:
			r.pkg = mfile.Syntax.PackageName()
			key = r.pkg
		}
		if _, ok := byPkg[key]; !ok {
			pkgOrder = append(pkgOrder, key)
		}
		byPkg[key] = append(byPkg[key], r)
	}

	rewrite := func(insts []*build.Instance) errors.Error {
		var runErrs errors.Error
		for _, e := range errors.Errors(rewriteInstances(insts, opts...)) {
			pos := e.Position()
			if r := byPath[pos.Filename()]; r != nil && pos.IsValid() {
				r.outcome = outcomeFailed
				r.errs = errors.Append(r.errs, e)
				continue
			}
			runErrs = errors.Append(runErrs, e)
		}
		return runErrs
	}

	// Pass 2: one package at a time.
	rewritten := false
	for _, key := range pkgOrder {
		group := byPkg[key]
		var files []*ast.File
		srcs := map[*rewrittenFile][]byte{}
		trees := map[*rewrittenFile]*ast.File{}
		for _, r := range group {
			src, err := os.ReadFile(r.path)
			if err != nil {
				return nil, err
			}
			f, err := parser.ParseFile(r.path, src, parser.ParseComments, parser.Version(version))
			if err != nil {
				// A file an earlier run already wrote in the target's
				// form, before a failed write left the version as it
				// was, parses at the target. It joins its package as it
				// is, so that sanitizing sees the fields it declares;
				// the rewrite leaves a file in the target's own state
				// alone, and it is reported as nothing.
				if converted, err2 := parser.ParseFile(r.path, src, parser.ParseComments, parser.Version(target)); err2 == nil {
					files = append(files, converted)
					srcs[r] = src
					trees[r] = converted
					continue
				}
				r.outcome = outcomeSkipped
				r.errs = errors.Promote(err, "")
				continue
			}
			files = append(files, f)
			srcs[r] = src
			trees[r] = f
		}
		if len(files) == 0 {
			continue
		}
		rewritten = true
		if err := rewrite([]*build.Instance{{ModuleFile: mf, Files: files}}); err != nil {
			return results, err
		}
		for _, r := range group {
			f := trees[r]
			if f == nil || r.outcome == outcomeFailed {
				continue
			}
			out, err := format.Node(f)
			if err != nil {
				r.outcome = outcomeFailed
				r.errs = errors.Promote(err, "format")
				continue
			}
			// The whole point of the bump is that the module evaluates at
			// the target; check that, rather than assume the rewrite got it
			// right.
			if _, err := parser.ParseFile(r.path, out, parser.ParseComments, parser.Version(target)); err != nil {
				r.outcome = outcomeFailed
				r.errs = errors.Promote(err, "")
				continue
			}
			src := srcs[r]
			r.marked = bytes.Count(out, []byte("TODO(cue-fix)")) > bytes.Count(src, []byte("TODO(cue-fix)"))
			if bytes.Equal(out, src) {
				r.outcome = outcomeUnchanged
			} else {
				r.outcome = outcomeConverted
				r.out = out
			}
		}
	}
	if !rewritten {
		// Still validate the options against the module's version, so that
		// an empty or wholly unparseable module reports a bad --exp.
		if err := rewrite([]*build.Instance{{ModuleFile: mf}}); err != nil {
			return results, err
		}
	}
	return results, nil
}

// reportRewrites prints one line per skipped or failed file, in walk
// order, and returns how many were skipped and how many failed.
func reportRewrites(w io.Writer, results []*rewrittenFile) (skipped, failed int) {
	for _, r := range results {
		switch r.outcome {
		case outcomeSkipped:
			skipped++
			fmt.Fprintf(w, "%s: skipped: %s\n", positionOf(r, r.errs), errorText(firstError(r.errs)))
		case outcomeFailed:
			failed++
			for _, e := range errors.Errors(r.errs) {
				fmt.Fprintf(w, "%s: %s\n", positionOf(r, e), errorText(e))
			}
		}
	}
	return skipped, failed
}

// positionOf renders r's path, relative to the module root, with the line
// and column of err's first position when it has one.
func positionOf(r *rewrittenFile, err errors.Error) string {
	if p := firstError(err).Position(); p.IsValid() && p.Line() > 0 {
		return fmt.Sprintf("%s:%d:%d", r.rel, p.Line(), p.Column())
	}
	return r.rel
}

func firstError(err errors.Error) errors.Error {
	if errs := errors.Errors(err); len(errs) > 0 {
		return errs[0]
	}
	return err
}

// errorText renders an error's message on one line: a trailing newline
// would open a blank line in the report, and a line break inside it would
// leave a line without its file.
func errorText(err error) string {
	return strings.ReplaceAll(strings.TrimRight(err.Error(), "\r\n"), "\n", " ")
}

// writeRewrites writes every converted file in walk order, reporting each
// write that fails, and returns how many failed.
func writeRewrites(w io.Writer, results []*rewrittenFile) (failed int) {
	for _, r := range results {
		if r.outcome != outcomeConverted {
			continue
		}
		if err := writeFileAtomic(r.path, r.out); err != nil {
			failed++
			fmt.Fprintf(w, "%s: write: %s\n", r.rel, writeReason(err))
			continue
		}
		r.written = true
	}
	return failed
}

// writeFileAtomic replaces the file at path with data through a temporary
// file in the same directory, so that a write that fails part-way leaves
// the file as it was and a rerun still finds it to convert. The file's
// mode is kept.
func writeFileAtomic(path string, data []byte) error {
	// Write through a link to its target, as os.WriteFile would; the
	// rename below would otherwise replace the link itself.
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	mode := fs.FileMode(0o666)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	// A file that cannot be opened for writing is not replaced either: the
	// rename below would not need that permission, and a file made
	// read-only is meant to stay as it is.
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		f.Close()
	} else if !goerrors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if err := tmp.Close(); werr == nil {
		werr = err
	}
	if werr == nil {
		werr = os.Chmod(name, mode)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		os.Remove(name)
		return werr
	}
	return nil
}

// writeUpgradedModuleFile writes the module file's new contents when they
// differ from the old, reporting a failure like any other write. It is
// replaced as the source files are, so that a write failing part-way
// leaves the version it records, and a rerun can still read it.
func writeUpgradedModuleFile(w io.Writer, root string, old, data []byte) (ok bool) {
	if bytes.Equal(old, data) {
		return true
	}
	path := filepath.Join(root, "cue.mod", "module.cue")
	if err := writeFileAtomic(path, data); err != nil {
		fmt.Fprintf(w, "cue.mod/module.cue: write: %s\n", writeReason(err))
		return false
	}
	return true
}

// writeReason is what went wrong with a write, without the path, which
// the report names in its own form.
func writeReason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// markedFiles lists the files, now on disk, whose rewrite added
// TODO(cue-fix) comments.
func markedFiles(results []*rewrittenFile) []string {
	var marked []string
	for _, r := range results {
		if r.marked && r.written {
			marked = append(marked, r.rel)
		}
	}
	return marked
}
