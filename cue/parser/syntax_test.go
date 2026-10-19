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

package parser_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue/ast"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modfile"
)

// headerPrefix starts each line of a corpus file's header.
const headerPrefix = "// versions: "

// TestSyntaxCorpus checks each file in testdata/syntax against the parser. A
// file starts with a header stating the parser's outcome for each minor
// language version from the earliest a module can declare to the current
// one, one line per run of versions with the same outcome, as in
//
//	// versions: v0.8.0..v0.17.0; outcome: ok
//	// versions: v0.18.0..; outcome: error 3:2: old-style alias syntax (=) is not allowed
//
// A range without an end extends to the current version and beyond. A file
// that enables experiments with @experiment(...) is parsed both with them
// blanked out and, at the versions where they exist, as written; lines for
// the latter name them, as in
//
//	// versions: v0.16.0..; experiments: try; outcome: ok
//
// An error outcome gives the position and message of the parser's first
// error, counting the header's lines. The test fails if a header differs from
// the one the parser's outcomes give; CUE_UPDATE=1 rewrites headers to match.
// See testdata/syntax/README.md.
func TestSyntaxCorpus(t *testing.T) {
	versions := languageVersions(t)
	root := filepath.Join("testdata", "syntax")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && name == "README.md" {
			continue
		}
		if e.IsDir() || filepath.Ext(name) != ".cue" {
			t.Errorf("unexpected %s in %s; want only .cue files and README.md", name, root)
			continue
		}
		files++
		t.Run(name, func(t *testing.T) {
			checkCorpusFile(t, filepath.Join(root, name), versions)
		})
	}
	if files == 0 {
		t.Errorf("no corpus files in %s", root)
	}
}

// checkCorpusFile checks the header of the corpus file at path, given the
// minor language versions to check, oldest first.
func checkCorpusFile(t *testing.T, path string, versions []string) {
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header, body := splitHeader(src)
	want := corpusHeader(filepath.Base(path), body, versions)
	if string(header) == want {
		return
	}
	if cuetest.UpdateOrDiffGoldenFiles() {
		cuetest.WriteGoldenFile(t, path, append([]byte(want), body...))
		return
	}
	t.Fatalf("header does not match the parser's outcomes; rerun with CUE_UPDATE=1 to rewrite it (-have +want):\n%s",
		cmp.Diff(string(header), want))
}

// splitHeader splits src into its header lines and the body after them.
func splitHeader(src []byte) (header, body []byte) {
	rest := src
	for bytes.HasPrefix(rest, []byte(headerPrefix)) {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			i = len(rest) - 1
		}
		rest = rest[i+1:]
	}
	return src[:len(src)-len(rest)], rest
}

// outcome is the result of parsing a file's body at one language version:
// no error, or the position, within the body, and message of the first one.
type outcome struct {
	ok        bool
	line, col int
	msg       string
}

// format returns o as it appears in a header that has the given number of
// lines.
func (o outcome) format(headerLines int) string {
	if o.ok {
		return "ok"
	}
	return fmt.Sprintf("error %d:%d: %s", o.line+headerLines, o.col, o.msg)
}

// parseOutcome parses src at the given language version.
func parseOutcome(name string, src []byte, version string) outcome {
	_, err := parser.ParseFile(name, src, parser.Version(version))
	if err == nil {
		return outcome{ok: true}
	}
	e := cueerrors.Errors(err)[0]
	format, args := e.Msg()
	pos := e.Position()
	return outcome{line: pos.Line(), col: pos.Column(), msg: fmt.Sprintf(format, args...)}
}

// corpusHeader returns the header that the parser's outcomes give for body,
// at the given minor language versions, oldest first.
func corpusHeader(name string, body []byte, versions []string) string {
	type run struct {
		from, to    string
		experiments []string
		outcome     outcome
	}
	var runs []run
	// add appends the runs of equal outcomes over vs, parsing src.
	add := func(vs []string, src []byte, experiments []string) {
		start := len(runs)
		for _, v := range vs {
			o := parseOutcome(name, src, v)
			if len(runs) > start && runs[len(runs)-1].outcome == o {
				runs[len(runs)-1].to = v
				continue
			}
			runs = append(runs, run{from: v, to: v, experiments: experiments, outcome: o})
		}
	}
	add(versions, withoutExperiments(name, body), nil)
	if exps := experimentNames(name, body); len(exps) > 0 {
		var available []string
		for _, v := range versions {
			if _, err := cueexperiment.NewFile(v, exps...); err == nil {
				available = append(available, v)
			}
		}
		add(available, body, exps)
	}
	current := versions[len(versions)-1]
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(headerPrefix)
		switch {
		case r.to == current:
			fmt.Fprintf(&b, "%s..", r.from)
		case r.to == r.from:
			b.WriteString(r.from)
		default:
			fmt.Fprintf(&b, "%s..%s", r.from, r.to)
		}
		if len(r.experiments) > 0 {
			fmt.Fprintf(&b, "; experiments: %s", strings.Join(r.experiments, ", "))
		}
		fmt.Fprintf(&b, "; outcome: %s\n", r.outcome.format(len(runs)))
	}
	return b.String()
}

// experimentAttrs returns the @experiment file attributes of src.
func experimentAttrs(name string, src []byte) []*ast.Attribute {
	f, _ := parser.ParseFile(name, src, parser.PackageClauseOnly)
	if f == nil {
		return nil
	}
	var attrs []*ast.Attribute
	for _, decl := range f.Decls {
		if a, ok := decl.(*ast.Attribute); ok {
			if name, _ := a.Split(); name == "experiment" {
				attrs = append(attrs, a)
			}
		}
	}
	return attrs
}

// experimentNames returns the names of the experiments that src enables,
// sorted.
func experimentNames(name string, src []byte) []string {
	var names []string
	for _, a := range experimentAttrs(name, src) {
		_, args := a.Split()
		for _, n := range strings.Split(args, ",") {
			names = append(names, strings.TrimSpace(n))
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// withoutExperiments returns src with its @experiment file attributes
// replaced by spaces, so that positions in it are unchanged.
func withoutExperiments(name string, src []byte) []byte {
	out := slices.Clone(src)
	for _, a := range experimentAttrs(name, src) {
		for i := a.Pos().Offset(); i < a.End().Offset(); i++ {
			out[i] = ' '
		}
	}
	return out
}

// languageVersions returns the minor language versions from the earliest a
// module can declare to the current one, oldest first, as in v0.8.0.
func languageVersions(t *testing.T) []string {
	earliest := semver.MajorMinor(modfile.EarliestClosedSchemaVersion())
	current := semver.MajorMinor(cueversion.LanguageVersion())
	major := semver.Major(current)
	if semver.Major(earliest) != major {
		t.Fatalf("earliest version %s and current version %s differ in major version", earliest, current)
	}
	minor := func(v string) int {
		n, err := strconv.Atoi(strings.TrimPrefix(v, major+"."))
		if err != nil {
			t.Fatalf("cannot read the minor version of %s: %v", v, err)
		}
		return n
	}
	var versions []string
	for m := minor(earliest); m <= minor(current); m++ {
		versions = append(versions, fmt.Sprintf("%s.%d.0", major, m))
	}
	return versions
}
