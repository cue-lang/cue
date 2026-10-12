// Copyright 2026 CUE Authors
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

package filetypes_test

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/filetypes"
	"github.com/go-quicktest/qt"
)

// TestDriverMatchesCUE is the differential guard that the earlier
// prototype parity test was meant to be: it evaluates types.cue with
// the real CUE evaluator and, over a broad cross-product of inputs,
// asserts the open-data driver accepts/rejects exactly what CUE does
// and resolves the same encoding, interpretation, and form. Because the
// oracle uses CUE's own unification, it captures disjunction-domain
// semantics the tri-state driver must reproduce; it would have caught
// the driver silently accepting form values outside a field's domain.
//
// The toFile oracle is a faithful evaluator replication of the
// reference algorithm the earlier tables were generated from (toFile1
// in the pre-driver generate.go): mode FileInfo unified with the tag
// values, the extension entry participating exactly when no tag
// declares the encoding as a regular field, and CUE's own disjunction
// defaults discharged at the end. It deliberately does not transcribe
// the driver's resolution structure, so it also guards the driver's
// decision of which entries participate — including forcing a
// struct-level disjunction such as tagInfo.pb onto a non-default
// branch when the extension conflicts with the default one.
func TestDriverMatchesCUE(t *testing.T) {
	t.Parallel()
	root := compileTypes(t)

	modes := []filetypes.Mode{filetypes.Input, filetypes.Export, filetypes.Def, filetypes.Eval}
	encodings := fieldNames(t, root.LookupPath(cue.ParsePath("modes.input.encodings")))
	forms := append([]string{""}, fieldNames(t, root.LookupPath(cue.ParsePath("forms")))...)
	interps := append([]string{""}, fieldNames(t, root.LookupPath(cue.ParsePath("interpretations")))...)
	tags := fieldNames(t, root.LookupPath(cue.ParsePath("tagInfo")))

	// filenames covers every known extension (which are the same in all
	// modes), stdin/stdout, an unknown extension, and no extension.
	var filenames []string
	for _, ext := range fieldNames(t, root.LookupPath(cue.ParsePath("modes.input.extensions"))) {
		if ext == "-" {
			filenames = append(filenames, "-")
		} else {
			filenames = append(filenames, "x"+ext)
		}
	}
	filenames = append(filenames, "x.unknown", "withoutextension")

	// Each mode has one oracle, shared by all subtests of that mode
	// so that they reuse each other's cached values.
	oracles := make(map[filetypes.Mode]*oracle)
	for _, mode := range modes {
		oracles[mode] = newOracle(compileTypes(t), mode)
	}

	// run runs body for every mode as parallel subtests of name.
	// The subtests of each name run one after another,
	// as a mode's oracle is not safe for concurrent use.
	run := func(name string, body func(t *testing.T, o *oracle)) {
		t.Run(name, func(t *testing.T) {
			for _, mode := range modes {
				t.Run(mode.String(), func(t *testing.T) {
					t.Parallel()
					body(t, oracles[mode])
				})
			}
		})
	}

	run("fromfile", func(t *testing.T, o *oracle) {
		mode := o.mode
		for _, enc := range encodings {
			for _, interp := range interps {
				for _, form := range forms {
					b := &build.File{Filename: "x", Encoding: build.Encoding(enc), Interpretation: build.Interpretation(interp), Form: build.Form(form)}
					fi, derr := filetypes.FromFile(b, mode)
					ok, want := o.fromFile(enc, interp, form)
					if (derr == nil) != ok {
						t.Errorf("[%s enc=%s interp=%q form=%q] driver ok=%v, CUE ok=%v", mode, enc, interp, form, derr == nil, ok)
						continue
					}
					if derr == nil {
						want.Filename = b.Filename
						qt.Check(t, qt.DeepEquals(fi, want),
							qt.Commentf("[%s enc=%s interp=%q form=%q] FileInfo mismatch", mode, enc, interp, form))
					}
				}
			}
		}
	})

	run("qualifier-pairs", func(t *testing.T, o *oracle) {
		for _, a := range tags {
			for _, b := range tags {
				scope := a
				want := []string{a}
				if b != a {
					scope = a + "+" + b
					want = []string{a, b}
				}
				compare(t, o, scope, want, "x")
			}
		}
	})

	// extension-axis: every single tag (and the empty scope) against
	// every known extension, stdin/stdout, an unknown extension, and a
	// filename without extension, in all modes. This is where a tag
	// that does not pin the encoding (a form tag, or pb with its
	// struct-level disjunction) must let the extension participate in
	// disjunct selection.
	run("extension-axis", func(t *testing.T, o *oracle) {
		scopes := append([]string{""}, tags...)
		for _, scope := range scopes {
			var tagList []string
			if scope != "" {
				tagList = []string{scope}
			}
			for _, filename := range filenames {
				compare(t, o, scope, tagList, filename)
			}
		}
	})

	// tag-pairs-with-extensions: all tag pairs against a representative
	// set of filenames. The pb pairs with .json/.proto/.textproto are
	// the regression surface for extension-driven disjunct selection.
	run("tag-pairs-with-extensions", func(t *testing.T, o *oracle) {
		representative := []string{"x.json", "x.proto", "x.textproto", "-"}
		for _, a := range tags {
			for _, b := range tags {
				if b == a {
					continue
				}
				scope := a + "+" + b
				for _, filename := range representative {
					compare(t, o, scope, []string{a, b}, filename)
				}
			}
		}
	})

	run("subsidiary-tags", func(t *testing.T, o *oracle) {
		mode := o.mode
		cases := []struct {
			scope    string
			top      []string
			stringly map[string]string
			boolean  map[string]bool
		}{
			{scope: "code+lang=js", top: []string{"code"}, stringly: map[string]string{"lang": "js"}},
			{scope: "ini+flavor=git", top: []string{"ini"}, stringly: map[string]string{"flavor": "git"}},
			{scope: "ini+flavor=bogus", top: []string{"ini"}, stringly: map[string]string{"flavor": "bogus"}},
			{scope: "json+compact", top: []string{"json"}, boolean: map[string]bool{"compact": true}},
			{scope: "yaml+indentSequences=false", top: []string{"yaml"}, boolean: map[string]bool{"indentSequences": false}},
			{scope: "jsonschema+strict", top: []string{"jsonschema"}, boolean: map[string]bool{"strict": true}},
			{
				scope:   "jsonschema+strict=false+strictKeywords=true",
				top:     []string{"jsonschema"},
				boolean: map[string]bool{"strict": false, "strictKeywords": true},
			},
		}
		for _, tc := range cases {
			got, derr := filetypes.ParseFileAndType("x", tc.scope, mode)
			ok, want := o.toFile(tc.top, tc.stringly, tc.boolean, "x")
			if (derr == nil) != ok {
				t.Errorf("[%s %s] driver ok=%v (%v), CUE ok=%v", mode, tc.scope, derr == nil, derr, ok)
				continue
			}
			if derr == nil {
				qt.Check(t, qt.DeepEquals(emptyAsNil(got), emptyAsNil(want)),
					qt.Commentf("[%s %s] build.File mismatch", mode, tc.scope))
			}
		}
	})
}

// compare resolves one (scope, filename, mode) with the driver and
// with the oracle and asserts they agree.
func compare(t *testing.T, o *oracle, scope string, tagList []string, filename string) {
	t.Helper()
	f, derr := filetypes.ParseFileAndType(filename, scope, o.mode)
	ok, want := o.toFile(tagList, nil, nil, filename)
	if (derr == nil) != ok {
		t.Errorf("[%s %q %s] driver ok=%v (%v), CUE ok=%v", o.mode, scope, filename, derr == nil, derr, ok)
		return
	}
	if derr == nil {
		qt.Check(t, qt.DeepEquals(emptyAsNil(f), emptyAsNil(want)),
			qt.Commentf("[%s %q %s] build.File mismatch", o.mode, scope, filename))
	}
}

// compileTypes compiles types.cue in a new context.
func compileTypes(t *testing.T) cue.Value {
	t.Helper()
	root := cuecontext.New().CompileBytes(filetypes.TypesCUESource(), cue.Filename("types.cue"))
	if err := root.Err(); err != nil {
		t.Fatalf("compiling types.cue: %v", err)
	}
	return root
}

func fieldNames(t *testing.T, v cue.Value) []string {
	t.Helper()
	it, err := v.Fields()
	if err != nil {
		t.Fatalf("iterating %v: %v", v.Path(), err)
	}
	var out []string
	for it.Next() {
		out = append(out, it.Selector().Unquoted())
	}
	return out
}

// resolveStr mirrors sval.resolve: a field's default value if it has
// one, else its concrete value, else "".
func resolveStr(v cue.Value, name string) string {
	f := v.LookupPath(cue.ParsePath(name))
	if !f.Exists() {
		return ""
	}
	if d, ok := f.Default(); ok {
		if s, err := d.String(); err == nil {
			return s
		}
	}
	if s, err := f.String(); err == nil {
		return s
	}
	return ""
}

func hasEncoding(v cue.Value) bool {
	return resolveStr(v, "encoding") != ""
}

// oracle evaluates types.cue for one mode. The evaluator is far slower
// than the driver under test, so it caches the values shared by many cases.
type oracle struct {
	root cue.Value
	mode filetypes.Mode

	// tagValues caches [oracle.tagValue] by its tags joined with "+".
	tagValues map[string]cue.Value

	// toFiles caches [oracle.toFile] results without subsidiary tags,
	// keyed by the sorted tags and the extension when it participates.
	toFiles map[string]*build.File
}

func newOracle(root cue.Value, mode filetypes.Mode) *oracle {
	return &oracle{
		root:      root,
		mode:      mode,
		tagValues: make(map[string]cue.Value),
		toFiles:   make(map[string]*build.File),
	}
}

// tagValue returns the mode's FileInfo unified with the tagInfo entries
// of tags, or a value which does not exist if any tag is unknown.
func (o *oracle) tagValue(tags []string) cue.Value {
	key := strings.Join(tags, "+")
	if v, ok := o.tagValues[key]; ok {
		return v
	}
	var v cue.Value
	if len(tags) == 0 {
		v = o.root.LookupPath(cue.ParsePath(fmt.Sprintf("modes.%s.FileInfo", o.mode)))
	} else if prev := o.tagValue(tags[:len(tags)-1]); prev.Exists() {
		if ti := o.root.LookupPath(cue.ParsePath("tagInfo." + tags[len(tags)-1])); ti.Exists() {
			v = prev.Unify(ti)
		}
	}
	o.tagValues[key] = v
	return v
}

// fromFile is the evaluator oracle for the FromFile resolution.
func (o *oracle) fromFile(enc, interp, form string) (bool, *filetypes.FileInfo) {
	if enc == "" {
		return false, nil
	}
	// Build a single conjunction rather than unifying each part in turn,
	// which evaluates every intermediate value.
	conjuncts := []string{
		fmt.Sprintf("modes.%s.FileInfo", o.mode),
		fmt.Sprintf("{encoding: %q}", enc),
	}
	// add adds the value at path, reporting whether it exists.
	add := func(path string) bool {
		conjuncts = append(conjuncts, path)
		return o.root.LookupPath(cue.ParsePath(path)).Exists()
	}
	if interp != "" {
		conjuncts = append(conjuncts, fmt.Sprintf("{interpretation: %q}", interp))
	}
	if form != "" {
		conjuncts = append(conjuncts, fmt.Sprintf("{form: %q}", form))
		if !add("forms." + form) {
			return false, nil
		}
	} else if interp != "" {
		if !add("interpretations." + interp) {
			return false, nil
		}
	}
	if interp == "" {
		if !add(fmt.Sprintf("modes.%s.encodings.%s", o.mode, enc)) {
			return false, nil
		}
	}
	v := o.root.Context().CompileString(strings.Join(conjuncts, " & "), cue.Scope(o.root))
	// Collapse struct-level disjunctions (tagInfo.pb, encodings.cue) to
	// the CUE default — the branch the driver's alts backtracking
	// selects — then reject a bottom result.
	v, _ = v.Default()
	if v.Validate() != nil {
		return false, nil
	}
	var fi filetypes.FileInfo
	if err := v.Decode(&fi); err != nil {
		return false, nil
	}
	return true, &fi
}

// toFile is the evaluator oracle for the qualifier-and-filename
// resolution. It replicates the reference algorithm the earlier
// lookup tables were generated from (toFile1 in the pre-driver
// generate.go): the file extension participates exactly when the
// unified tag value does not declare "encoding" as a regular field —
// a plain CUE field lookup, which sees a concrete or defaulted
// encoding but does not descend into an unresolved struct-level
// disjunction such as tagInfo.pb — and disjunction defaults are
// discharged only afterwards, so a conflicting extension can force a
// disjunction onto a non-default branch.
func (o *oracle) toFile(tags []string, stringTags map[string]string, boolTags map[string]bool, filename string) (bool, *build.File) {
	if len(stringTags) > 0 || len(boolTags) > 0 {
		return o.toFileUncached(tags, stringTags, boolTags, filename)
	}
	// Unification is commutative, so the order of the tags does not
	// matter, and the filename only matters when its extension does.
	tags = slices.Sorted(slices.Values(tags))
	key := strings.Join(tags, "+")
	if v := o.tagValue(tags); v.Exists() && !v.LookupPath(cue.MakePath(cue.Str("encoding"))).Exists() {
		key += " " + testFileExt(filename)
	}
	f, ok := o.toFiles[key]
	if !ok {
		_, f = o.toFileUncached(tags, nil, nil, filename)
		o.toFiles[key] = f
	}
	if f == nil {
		return false, nil
	}
	f2 := *f
	f2.Filename = filename
	f2.Tags = maps.Clone(f.Tags)
	f2.BoolTags = maps.Clone(f.BoolTags)
	return true, &f2
}

// toFileUncached is [oracle.toFile] without the cache.
func (o *oracle) toFileUncached(tags []string, stringTags map[string]string, boolTags map[string]bool, filename string) (bool, *build.File) {
	root, mode := o.root, o.mode
	v := o.tagValue(tags)
	if !v.Exists() {
		return false, nil
	}
	if len(stringTags) > 0 {
		v = v.Unify(root.Context().Encode(map[string]any{"tags": stringTags}))
	}
	if len(boolTags) > 0 {
		v = v.Unify(root.Context().Encode(map[string]any{"boolTags": boolTags}))
	}
	if v.Validate() != nil {
		return false, nil // invalid tag combination
	}
	if !v.LookupPath(cue.MakePath(cue.Str("encoding"))).Exists() {
		ext := testFileExt(filename)
		if ext == "" {
			return false, nil // no encoding specified
		}
		ev := root.LookupPath(cue.MakePath(
			cue.Str("modes"), cue.Str(mode.String()), cue.Str("extensions"), cue.Str(ext)))
		if !ev.Exists() {
			return false, nil // unknown file extension
		}
		v = v.Unify(ev)
	}
	v, _ = v.Default()
	if v.Validate() != nil {
		return false, nil
	}
	if !hasEncoding(v) {
		return false, nil
	}
	var f build.File
	if err := v.Decode(&f); err != nil {
		return false, nil
	}
	f.Filename = filename
	return true, &f
}

// testFileExt mirrors the package's fileExt: "-" stands for
// stdin/stdout, and a leading dot alone does not make an extension.
func testFileExt(f string) string {
	if f == "-" {
		return "-"
	}
	e := filepath.Ext(f)
	if e == "" || e == filepath.Base(f) {
		return ""
	}
	return e
}

// emptyAsNil returns a copy of f with empty maps replaced by nil,
// so that go-cmp can compare files without any options.
func emptyAsNil(f *build.File) *build.File {
	f2 := *f
	if len(f2.Tags) == 0 {
		f2.Tags = nil
	}
	if len(f2.BoolTags) == 0 {
		f2.BoolTags = nil
	}
	return &f2
}
