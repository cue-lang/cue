// Copyright 2020 CUE Authors
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

package filetypes

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
)

func check(t *testing.T, want, x interface{}, err error) {
	t.Helper()
	if err != nil {
		x = errors.String(err.(errors.Error))
	}
	qt.Assert(t, qt.CmpEquals(x, want, cmpopts.EquateEmpty()))
}

func TestAspectNames(t *testing.T) {
	want := [numAspects]string{
		aAttributes:   "attributes",
		aConstraints:  "constraints",
		aCycles:       "cycles",
		aData:         "data",
		aDefinitions:  "definitions",
		aDocs:         "docs",
		aIncomplete:   "incomplete",
		aImports:      "imports",
		aKeepDefaults: "keepDefaults",
		aOptional:     "optional",
		aReferences:   "references",
		aStream:       "stream",
	}
	qt.Assert(t, qt.DeepEquals(aspectNames, want))
}

func TestFromFile(t *testing.T) {
	testCases := []struct {
		name string
		in   build.File
		mode Mode
		out  interface{}
	}{{
		name: "must specify encoding",
		in:   build.File{},
		out:  `no encoding specified`,
	}, {
		// Default without any
		name: "cue",
		in: build.File{
			Filename: "",
			Encoding: build.CUE,
		},
		mode: Def,
		out: &FileInfo{
			Encoding:     build.CUE,
			Form:         build.Schema,
			Definitions:  true,
			Data:         true,
			Optional:     true,
			Constraints:  true,
			References:   true,
			Cycles:       true,
			KeepDefaults: true,
			Incomplete:   true,
			Imports:      true,
			Docs:         true,
			Attributes:   true,
		},
	}, {
		// CUE encoding in data form.
		name: "data: cue",
		in: build.File{
			Filename: "",
			Form:     build.Data,
			Encoding: build.CUE,
		},
		mode: Input,
		out: &FileInfo{
			Filename:   "",
			Encoding:   build.CUE,
			Form:       build.Data,
			Data:       true,
			Docs:       true,
			Attributes: true,
		},
	}, {
		// Filename starting with a . but no other extension.
		name: "filename-with-dot",
		in: build.File{
			Filename: ".json",
		},
		mode: Def,
		out:  `no encoding specified`,
	}, {
		name: "yaml",
		mode: Def,
		in: build.File{
			Filename: "foo.yaml",
			Encoding: build.YAML,
		},
		out: &FileInfo{
			Filename:   "foo.yaml",
			Encoding:   build.YAML,
			Form:       build.Graph,
			Data:       true,
			References: true,
			Cycles:     true,
			Stream:     true,
			Docs:       true,
			Attributes: true,
		},
	}, {
		name: "yaml+openapi",
		in: build.File{
			Filename:       "foo.yaml",
			Encoding:       build.YAML,
			Interpretation: build.OpenAPI,
		},
		out: &FileInfo{
			Filename:       "foo.yaml",
			Encoding:       build.YAML,
			Interpretation: build.OpenAPI,
			Form:           build.Schema,
			Definitions:    true,
			Data:           true,
			Optional:       true,
			Constraints:    true,
			References:     true,
			Cycles:         true,
			KeepDefaults:   true,
			Incomplete:     true,
			Imports:        true,
			Docs:           true,
			Attributes:     true,
		},
	}, {
		name: "JSONDefault",
		mode: Input,
		in: build.File{
			Filename: "data.json",
			Encoding: build.JSON,
		},
		out: &FileInfo{
			Filename:     "data.json",
			Encoding:     build.JSON,
			Form:         build.Data,
			Definitions:  false,
			Data:         true,
			Optional:     false,
			Constraints:  false,
			References:   false,
			Cycles:       false,
			KeepDefaults: false,
			Incomplete:   false,
			Imports:      false,
			Docs:         false,
			Attributes:   false,
		},
	}, {
		name: "JSONSchema",
		in: build.File{
			Filename:       "foo.json",
			Interpretation: "jsonschema",
			Encoding:       build.JSON,
		},
		out: &FileInfo{
			Filename:       "foo.json",
			Encoding:       build.JSON,
			Interpretation: "jsonschema",
			Form:           build.Schema,
			Definitions:    true,
			Data:           true,
			Optional:       true,
			Constraints:    true,
			References:     true,
			Cycles:         true,
			KeepDefaults:   true,
			Incomplete:     true,
			Imports:        true,
			Docs:           true,
			Attributes:     true,
		},
	}, {
		name: "JSONOpenAPI",
		in: build.File{
			Filename:       "foo.json",
			Encoding:       build.JSON,
			Interpretation: build.OpenAPI,
		},
		mode: Def,
		out: &FileInfo{
			Filename:       "foo.json",
			Encoding:       build.JSON,
			Interpretation: build.OpenAPI,
			Form:           build.Schema,
			Definitions:    true,
			Data:           true,
			Optional:       true,
			Constraints:    true,
			References:     true,
			Cycles:         true,
			KeepDefaults:   true,
			Incomplete:     true,
			Imports:        true,
			Docs:           true,
			Attributes:     true,
		},
	}, {
		name: "KoalaXML",
		in: build.File{
			Filename: "foo.xml",
			Encoding: "xml",
			BoolTags: map[string]bool{
				"koala": true,
			},
		},
		mode: Def,
		out: &FileInfo{
			Filename:     "foo.xml",
			Encoding:     "xml",
			Form:         "data",
			Definitions:  false,
			Data:         true,
			Optional:     false,
			Constraints:  false,
			References:   false,
			Cycles:       false,
			KeepDefaults: false,
			Incomplete:   false,
			Imports:      false,
			Docs:         true,
			Attributes:   true,
		},
	}, {
		name: "OpenAPIDefaults",
		in: build.File{
			Filename:       "-",
			Encoding:       build.JSON,
			Interpretation: build.OpenAPI,
		},
		mode: Def,
		out: &FileInfo{
			Filename:       "-",
			Encoding:       build.JSON,
			Interpretation: build.OpenAPI,
			Form:           build.Schema,
			Definitions:    true,
			Data:           true,
			Optional:       true,
			Constraints:    true,
			References:     true,
			Cycles:         true,
			KeepDefaults:   true,
			Incomplete:     true,
			Imports:        true,
			Docs:           true,
			Attributes:     true,
		},
	}, {
		name: "Go",
		in: build.File{
			Encoding: build.Code,
			Filename: "foo.go",
		},
		mode: Def,
		out: &FileInfo{
			Filename:     "foo.go",
			Encoding:     build.Code,
			Form:         build.Schema,
			Definitions:  true,
			Data:         true,
			Optional:     true,
			Constraints:  true,
			References:   true,
			Cycles:       true,
			KeepDefaults: true,
			Incomplete:   true,
			Imports:      true,
			Docs:         true,
			Attributes:   true,
		},
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := FromFile(&tc.in, tc.mode)
			check(t, tc.out, info, err)
		})
	}
}

func TestParseFile(t *testing.T) {
	// TODO(errors): wrong path?
	testCases := []struct {
		in   string
		mode Mode
		out  interface{}
	}{{
		in:   "file.json",
		mode: Input,
		out: &build.File{
			Filename:       "file.json",
			Encoding:       build.JSON,
			Interpretation: build.Auto,
			BoolTags:       map[string]bool{"compact": false},
		},
	}, {
		in:   ".json",
		mode: Input,
		out:  `no encoding specified for file ".json"`,
	}, {
		in:   ".json.yaml",
		mode: Input,
		out: &build.File{
			Filename:       ".json.yaml",
			Encoding:       build.YAML,
			Interpretation: build.Auto,
			BoolTags:       map[string]bool{"indentSequences": true},
		},
	}, {
		in:   "file.json",
		mode: Def,
		out: &build.File{
			Filename: "file.json",
			Encoding: build.JSON,
			BoolTags: map[string]bool{"compact": false},
		},
	}, {
		in: "schema:file.json",
		out: &build.File{
			Filename:       "file.json",
			Encoding:       build.JSON,
			Interpretation: build.Auto,
			Form:           build.Schema,
			BoolTags:       map[string]bool{"compact": false},
		},
	}, {
		in: "openapi:-",
		out: &build.File{
			Filename:       "-",
			Encoding:       build.JSON,
			Interpretation: build.OpenAPI,
			Form:           build.Schema,
			BoolTags: map[string]bool{
				"allSchemas":     false,
				"strict":         false,
				"strictFeatures": false,
				"strictKeywords": false,
			},
		},
	}, {
		in: "cue:file.json",
		out: &build.File{
			Filename: "file.json",
			Encoding: build.CUE,
			BoolTags: map[string]bool{"compact": false},
		},
	}, {
		in: "cue+schema:-",
		out: &build.File{
			Filename: "-",
			Encoding: build.CUE,
			Form:     build.Schema,
			BoolTags: map[string]bool{"compact": false},
		},
	}, {
		in: "json+compact:-",
		out: &build.File{
			Filename: "-",
			Encoding: build.JSON,
			BoolTags: map[string]bool{"compact": true},
		},
	}, {
		in: "cue+compact:-",
		out: &build.File{
			Filename: "-",
			Encoding: build.CUE,
			BoolTags: map[string]bool{"compact": true},
		},
	}, {
		// compact is not (yet) valid for encodings other than json and cue.
		in:  "yaml+compact:-",
		out: `field "compact" not allowed`,
	}, {
		in: "code+lang=js:foo.x",
		out: &build.File{
			Filename: "foo.x",
			Encoding: build.Code,
			Tags:     map[string]string{"lang": "js"},
		},
	}, {
		in:  "json+lang=js:foo.x",
		out: `tag lang is not allowed in this context`,
	}, {
		in:  "foo:file.bar",
		out: `unknown filetype foo`,
	}, {
		in:  "file.bar",
		out: `unknown file extension .bar`,
	}, {
		in: `D:\foo.json`,
		out: &build.File{
			Filename:       `D:\foo.json`,
			Encoding:       build.JSON,
			Interpretation: build.Auto,
			BoolTags:       map[string]bool{"compact": false},
		},
	}, {
		in: `json:D:\foo.json`,
		out: &build.File{
			Filename: `D:\foo.json`,
			Encoding: build.JSON,
			BoolTags: map[string]bool{"compact": false},
		},
	}, {
		in: `D:\foo:colons.json`,
		out: &build.File{
			Filename:       `D:\foo:colons.json`,
			Encoding:       build.JSON,
			Interpretation: build.Auto,
			BoolTags:       map[string]bool{"compact": false},
		},
	}, {
		in: `/foo:colons.json`,
		out: &build.File{
			Filename:       `/foo:colons.json`,
			Encoding:       build.JSON,
			Interpretation: build.Auto,
			BoolTags:       map[string]bool{"compact": false},
		},
	}, {
		in:  "json:",
		out: `empty file name in "json:"`,
	}, {
		in:  ":file.json",
		out: `empty filetype prefix in ":file.json"`,
	}, {
		in:   "-",
		mode: Input,
		out: &build.File{
			Filename: "-",
			Encoding: build.CUE,
		},
	}, {
		in:   "-",
		mode: Export,
		out: &build.File{
			Filename: "-",
			Encoding: build.JSON,
		},
	}}
	for _, tc := range testCases {
		t.Run(tc.in, func(t *testing.T) {
			f, err := ParseFile(tc.in, tc.mode)
			check(t, tc.out, f, err)
		})
	}
}

func TestParseArgs(t *testing.T) {
	testCases := []struct {
		in  string
		out interface{}
	}{{
		in: "foo.json baz.yaml",
		out: []*build.File{
			{
				Filename:       "foo.json",
				Encoding:       build.JSON,
				Interpretation: build.Auto,
				BoolTags:       map[string]bool{"compact": false},
			},
			{
				Filename:       "baz.yaml",
				Encoding:       build.YAML,
				Interpretation: build.Auto,
				BoolTags:       map[string]bool{"indentSequences": true},
			},
		},
	}, {
		in: "data: foo.cue",
		out: []*build.File{
			{Filename: "foo.cue", Encoding: build.CUE, Form: build.Data, BoolTags: map[string]bool{"compact": false}},
		},
	}, {
		in: "json: foo.json bar.data jsonschema: bar.schema",
		out: []*build.File{
			{Filename: "foo.json", Encoding: build.JSON, BoolTags: map[string]bool{"compact": false}}, // no auto!
			{Filename: "bar.data", Encoding: build.JSON, BoolTags: map[string]bool{"compact": false}},
			{
				Filename:       "bar.schema",
				Encoding:       build.JSON,
				Form:           build.Schema,
				Interpretation: "jsonschema",
				BoolTags: map[string]bool{
					"strict":               false,
					"strictFeatures":       false,
					"strictKeywords":       false,
					"openOnlyWhenExplicit": false,
				},
			},
		},
	}, {
		in: "ini+flavor=git: foo",
		out: []*build.File{
			{
				Filename: "foo",
				Encoding: build.INI,
				Tags:     map[string]string{"flavor": "git"},
			},
		},
	}, {
		// The .ini extension resolves the encoding, so a bare flavor tag is
		// enough, as "koala: x.xml" is for XML.
		in: "flavor=git: foo.ini",
		out: []*build.File{
			{
				Filename: "foo.ini",
				Encoding: build.INI,
				Tags:     map[string]string{"flavor": "git"},
			},
		},
	}, {
		in:  "ini+flavor=bogus: foo",
		out: `invalid value "bogus" for tag "flavor"; must be one of: git, python, systemd, windows`,
	}, {
		in:  "ini+flavor=git+flavor=python: foo",
		out: `conflicting values for tag "flavor": "git" and "python"`,
	}, {
		in:  "ini+flavor: foo",
		out: `tag "flavor" must have value (flavor=<value>)`,
	}, {
		in:  "flavor=git: foo.json",
		out: `tag flavor is not allowed in this context`,
	}, {
		in:  "json+flavor=git: foo",
		out: `tag flavor is not allowed in this context`,
	}, {
		in:  "ini+git: foo",
		out: `unknown filetype git`,
	}, {
		in: "foo.ini",
		out: []*build.File{
			{
				Filename: "foo.ini",
				Encoding: build.INI,
				Tags:     map[string]string{"flavor": ""},
			},
		},
	}, {
		in: "koala: bar.xml",
		out: []*build.File{
			{
				Filename:       "bar.xml",
				Encoding:       "xml",
				Interpretation: "auto",
				BoolTags: map[string]bool{
					"koala": true,
				},
			},
		},
	}, {
		in: "jsonschema+strict: bar.schema",
		out: []*build.File{
			{
				Filename:       "bar.schema",
				Encoding:       "json",
				Interpretation: "jsonschema",
				Form:           build.Schema,
				BoolTags: map[string]bool{
					"strict":               true,
					"strictFeatures":       true,
					"strictKeywords":       true,
					"openOnlyWhenExplicit": false,
				},
			},
		},
	}, {
		in: "jsonschema+strictFeatures=1: bar.schema",
		out: []*build.File{
			{
				Filename:       "bar.schema",
				Encoding:       "json",
				Interpretation: "jsonschema",
				Form:           build.Schema,
				BoolTags: map[string]bool{
					"openOnlyWhenExplicit": false,
					"strict":               false,
					"strictFeatures":       true,
					"strictKeywords":       false,
				},
			},
		},
	}, {
		in: "jsonschema+openOnlyWhenExplicit: bar.schema",
		out: []*build.File{
			{
				Filename:       "bar.schema",
				Encoding:       "json",
				Interpretation: "jsonschema",
				Form:           build.Schema,
				BoolTags: map[string]bool{
					"strict":               false,
					"strictFeatures":       false,
					"strictKeywords":       false,
					"openOnlyWhenExplicit": true,
				},
			},
		},
	}, {
		// "false" turns a bool tag off explicitly.
		in: "jsonschema+strict=false: bar.schema",
		out: []*build.File{
			{
				Filename:       "bar.schema",
				Encoding:       "json",
				Interpretation: "jsonschema",
				Form:           build.Schema,
				BoolTags: map[string]bool{
					"strict":               false,
					"strictFeatures":       false,
					"strictKeywords":       false,
					"openOnlyWhenExplicit": false,
				},
			},
		},
	}, {
		// turn off a cascaded default, leaving strictKeywords inherited from strict.
		// "0" is short for "false".
		in: "jsonschema+strict+strictFeatures=0: bar.schema",
		out: []*build.File{
			{
				Filename:       "bar.schema",
				Encoding:       "json",
				Interpretation: "jsonschema",
				Form:           build.Schema,
				BoolTags: map[string]bool{
					"strict":               true,
					"strictFeatures":       false,
					"strictKeywords":       true,
					"openOnlyWhenExplicit": false,
				},
			},
		},
	}, {
		in: `json: c:\foo.json c:\path\to\file.dat`,
		out: []*build.File{
			{Filename: `c:\foo.json`, Encoding: build.JSON, BoolTags: map[string]bool{"compact": false}},
			{Filename: `c:\path\to\file.dat`, Encoding: build.JSON, BoolTags: map[string]bool{"compact": false}},
		},
	}, {
		in: `D:\foo.json`,
		out: []*build.File{
			{Filename: `D:\foo.json`, Encoding: build.JSON, Interpretation: build.Auto, BoolTags: map[string]bool{"compact": false}},
		},
	}, {
		in: `D:\foo:colons.json`,
		out: []*build.File{
			{Filename: `D:\foo:colons.json`, Encoding: build.JSON, Interpretation: build.Auto, BoolTags: map[string]bool{"compact": false}},
		},
	}, {
		in: `/foo:colons.json`,
		out: []*build.File{
			{Filename: `/foo:colons.json`, Encoding: build.JSON, Interpretation: build.Auto, BoolTags: map[string]bool{"compact": false}},
		},
	}, {
		in:  `json:D:\foo.json`,
		out: `cannot combine file type and file name; did you mean "json: D:\\foo.json"?`,
	}, {
		in:  "json: json+schema: bar.schema",
		out: `scoped qualifier "json:" without file`,
	}, {
		in:  "json:",
		out: `scoped qualifier "json:" without file`,
	}, {
		in:  ":file.json",
		out: `empty filetype prefix in ":file.json"`,
	}, {
		in:  "json:foo:bar.yaml",
		out: `cannot combine file type and file name; did you mean "json: foo:bar.yaml"?`,
	}, {
		in:  "data:foo.cue",
		out: `cannot combine file type and file name; did you mean "data: foo.cue"?`,
	}, {
		// Regression test for https://cuelang.org/issue/3952: a missing
		// space after the file type qualifier should yield a helpful hint.
		in:  "json:-",
		out: `cannot combine file type and file name; did you mean "json: -"?`,
	}}
	for _, tc := range testCases {
		t.Run(tc.in, func(t *testing.T) {
			files, err := ParseArgs(strings.Split(tc.in, " "))
			check(t, tc.out, files, err)
		})
	}
}

// TestRegisteredEncodingModeMatrix (T024, DYNFT-REG-002) checks that a
// dynamically registered encoding resolves exactly like an
// equivalently-configured built-in in all four modes, modulo the
// encoding name itself, and that subsidiary-tag misuse on it yields
// the built-in error phrasing.
//
// The reference built-in is jsonl: tagInfo.jsonl contributes only the
// encoding, and encodings.jsonl is forms.data plus stream: true, with
// no mode-specific extension overlays — a shape a dynamic registration
// can declare exactly.
func TestRegisteredEncodingModeMatrix(t *testing.T) {
	ResetDynamicRegistryForTesting()
	t.Cleanup(ResetDynamicRegistryForTesting)

	streamTrue := true
	err := RegisterEncoding(DynamicEncoding{
		Name:       "kvjsonl",
		Extensions: []string{".kvjsonl"},
		Info: DynamicFileInfo{
			Form:    "data",
			Aspects: map[string]bool{"stream": streamTrue},
		},
	})
	qt.Assert(t, qt.IsNil(err))

	// mirror maps the registered encoding's names back to the
	// built-in's so results can be compared field-by-field.
	mirrorFile := func(f *build.File) *build.File {
		c := *f
		c.Filename = strings.ReplaceAll(c.Filename, "kvjsonl", "jsonl")
		if c.Encoding == "kvjsonl" {
			c.Encoding = build.JSONL
		}
		return &c
	}

	for mode := Input; mode < NumModes; mode++ {
		t.Run(mode.String(), func(t *testing.T) {
			// Resolution by extension.
			got, err1 := ParseFileAndType("x.kvjsonl", "", mode)
			want, err2 := ParseFileAndType("x.jsonl", "", mode)
			qt.Assert(t, qt.IsNil(err1))
			qt.Assert(t, qt.IsNil(err2))
			qt.Assert(t, qt.CmpEquals(mirrorFile(got), want, cmpopts.EquateEmpty()))

			// Resolution by explicit qualifier.
			got, err1 = ParseFileAndType("x.data", "kvjsonl", mode)
			want, err2 = ParseFileAndType("x.data", "jsonl", mode)
			qt.Assert(t, qt.IsNil(err1))
			qt.Assert(t, qt.IsNil(err2))
			qt.Assert(t, qt.CmpEquals(mirrorFile(got), want, cmpopts.EquateEmpty()))

			// Qualifier combined with a top-level form tag.
			got, err1 = ParseFileAndType("x.data", "kvjsonl+schema", mode)
			want, err2 = ParseFileAndType("x.data", "jsonl+schema", mode)
			qt.Assert(t, qt.IsNil(err1))
			qt.Assert(t, qt.IsNil(err2))
			qt.Assert(t, qt.CmpEquals(mirrorFile(got), want, cmpopts.EquateEmpty()))

			// FromFile reports the same detailed file info.
			gotFI, err1 := FromFile(&build.File{Filename: "x.kvjsonl", Encoding: "kvjsonl"}, mode)
			wantFI, err2 := FromFile(&build.File{Filename: "x.jsonl", Encoding: build.JSONL}, mode)
			qt.Assert(t, qt.IsNil(err1))
			qt.Assert(t, qt.IsNil(err2))
			gotFI.Filename, gotFI.Encoding = wantFI.Filename, wantFI.Encoding
			qt.Assert(t, qt.CmpEquals(gotFI, wantFI, cmpopts.EquateEmpty()))
		})
	}

	// Subsidiary-tag misuse yields the built-in error phrasing, for a
	// string tag and a bool tag alike.
	_, err = ParseFileAndType("x.data", "kvjsonl+lang=js", Input)
	qt.Assert(t, qt.ErrorMatches(err, `tag lang is not allowed in this context`))
	_, err = ParseFileAndType("x.data", "kvjsonl+strict", Input)
	qt.Assert(t, qt.ErrorMatches(err, `tag strict is not allowed in this context`))
}

func TestDefaultTagsForInterpretation(t *testing.T) {
	tags := DefaultTagsForInterpretation(build.JSONSchema, Input)
	qt.Assert(t, qt.DeepEquals(tags, map[string]bool{
		"strict":               false,
		"strictFeatures":       false,
		"strictKeywords":       false,
		"openOnlyWhenExplicit": false,
	}))
}
