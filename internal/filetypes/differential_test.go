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
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/filetypes"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"
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

	// run runs body for every mode as parallel subtests of name, each
	// with its own compiled types.cue to keep the subtests independent.
	run := func(name string, body func(t *testing.T, root cue.Value, mode filetypes.Mode)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range modes {
				t.Run(mode.String(), func(t *testing.T) {
					t.Parallel()
					body(t, compileTypes(t), mode)
				})
			}
		})
	}

	run("fromfile", func(t *testing.T, root cue.Value, mode filetypes.Mode) {
		for _, enc := range encodings {
			for _, interp := range interps {
				for _, form := range forms {
					b := &build.File{Filename: "x", Encoding: build.Encoding(enc), Interpretation: build.Interpretation(interp), Form: build.Form(form)}
					fi, derr := filetypes.FromFile(b, mode)
					ok, want := cueFromFile(root, mode, enc, interp, form)
					if (derr == nil) != ok {
						t.Errorf("[%s enc=%s interp=%q form=%q] driver ok=%v, CUE ok=%v", mode, enc, interp, form, derr == nil, ok)
						continue
					}
					if derr == nil {
						want.Filename = b.Filename
						qt.Check(t, qt.CmpEquals(fi, want, cmpopts.EquateEmpty()),
							qt.Commentf("[%s enc=%s interp=%q form=%q] FileInfo mismatch", mode, enc, interp, form))
					}
				}
			}
		}
	})

	run("qualifier-pairs", func(t *testing.T, root cue.Value, mode filetypes.Mode) {
		for _, a := range tags {
			for _, b := range tags {
				scope := a
				want := []string{a}
				if b != a {
					scope = a + "+" + b
					want = []string{a, b}
				}
				compare(t, root, mode, scope, want, "x")
			}
		}
	})

	// extension-axis: every single tag (and the empty scope) against
	// every known extension, stdin/stdout, an unknown extension, and a
	// filename without extension, in all modes. This is where a tag
	// that does not pin the encoding (a form tag, or pb with its
	// struct-level disjunction) must let the extension participate in
	// disjunct selection.
	run("extension-axis", func(t *testing.T, root cue.Value, mode filetypes.Mode) {
		scopes := append([]string{""}, tags...)
		for _, scope := range scopes {
			var tagList []string
			if scope != "" {
				tagList = []string{scope}
			}
			for _, filename := range filenames {
				compare(t, root, mode, scope, tagList, filename)
			}
		}
	})

	// tag-pairs-with-extensions: all tag pairs against a representative
	// set of filenames. The pb pairs with .json/.proto/.textproto are
	// the regression surface for extension-driven disjunct selection.
	run("tag-pairs-with-extensions", func(t *testing.T, root cue.Value, mode filetypes.Mode) {
		representative := []string{"x.json", "x.proto", "x.textproto", "-"}
		for _, a := range tags {
			for _, b := range tags {
				if b == a {
					continue
				}
				scope := a + "+" + b
				for _, filename := range representative {
					compare(t, root, mode, scope, []string{a, b}, filename)
				}
			}
		}
	})

	run("subsidiary-tags", func(t *testing.T, root cue.Value, mode filetypes.Mode) {
		cases := []struct {
			scope    string
			top      []string
			stringly map[string]string
			boolean  map[string]bool
		}{
			{scope: "code+lang=js", top: []string{"code"}, stringly: map[string]string{"lang": "js"}},
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
			ok, want := cueToFile(root, mode, tc.top, tc.stringly, tc.boolean, "x")
			if (derr == nil) != ok {
				t.Errorf("[%s %s] driver ok=%v (%v), CUE ok=%v", mode, tc.scope, derr == nil, derr, ok)
				continue
			}
			if derr == nil {
				qt.Check(t, qt.CmpEquals(got, want, cmpopts.EquateEmpty()),
					qt.Commentf("[%s %s] build.File mismatch", mode, tc.scope))
			}
		}
	})
}

// compare resolves one (scope, filename, mode) with the driver and
// with the oracle and asserts they agree.
func compare(t *testing.T, root cue.Value, mode filetypes.Mode, scope string, tagList []string, filename string) {
	t.Helper()
	f, derr := filetypes.ParseFileAndType(filename, scope, mode)
	ok, oracle := cueToFile(root, mode, tagList, nil, nil, filename)
	if (derr == nil) != ok {
		t.Errorf("[%s %q %s] driver ok=%v (%v), CUE ok=%v", mode, scope, filename, derr == nil, derr, ok)
		return
	}
	if derr == nil {
		qt.Check(t, qt.CmpEquals(f, oracle, cmpopts.EquateEmpty()),
			qt.Commentf("[%s %q %s] build.File mismatch", mode, scope, filename))
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

// cueFromFile is the evaluator oracle for the FromFile resolution.
func cueFromFile(root cue.Value, mode filetypes.Mode, enc, interp, form string) (bool, *filetypes.FileInfo) {
	ctx := root.Context()
	if enc == "" {
		return false, nil
	}
	v := root.LookupPath(cue.ParsePath(fmt.Sprintf("modes.%s.FileInfo", mode)))
	v = v.Unify(ctx.CompileString(fmt.Sprintf("{encoding: %q}", enc)))
	if interp != "" {
		v = v.Unify(ctx.CompileString(fmt.Sprintf("{interpretation: %q}", interp)))
	}
	if form != "" {
		v = v.Unify(ctx.CompileString(fmt.Sprintf("{form: %q}", form)))
		fe := root.LookupPath(cue.ParsePath("forms." + form))
		if !fe.Exists() {
			return false, nil
		}
		v = v.Unify(fe)
	} else if interp != "" {
		ie := root.LookupPath(cue.ParsePath("interpretations." + interp))
		if !ie.Exists() {
			return false, nil
		}
		v = v.Unify(ie)
	}
	if interp == "" {
		ee := root.LookupPath(cue.ParsePath(fmt.Sprintf("modes.%s.encodings.%s", mode, enc)))
		if !ee.Exists() {
			return false, nil
		}
		v = v.Unify(ee)
	}
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

// cueToFile is the evaluator oracle for the qualifier-and-filename
// resolution. It replicates the reference algorithm the earlier
// lookup tables were generated from (toFile1 in the pre-driver
// generate.go): the file extension participates exactly when the
// unified tag value does not declare "encoding" as a regular field —
// a plain CUE field lookup, which sees a concrete or defaulted
// encoding but does not descend into an unresolved struct-level
// disjunction such as tagInfo.pb — and disjunction defaults are
// discharged only afterwards, so a conflicting extension can force a
// disjunction onto a non-default branch.
func cueToFile(root cue.Value, mode filetypes.Mode, tags []string, stringTags map[string]string, boolTags map[string]bool, filename string) (bool, *build.File) {
	v := root.LookupPath(cue.ParsePath(fmt.Sprintf("modes.%s.FileInfo", mode)))
	for _, tag := range tags {
		ti := root.LookupPath(cue.ParsePath("tagInfo." + tag))
		if !ti.Exists() {
			return false, nil
		}
		v = v.Unify(ti)
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
