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

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func check(t *testing.T, want, x interface{}, err error) {
	t.Helper()
	if err != nil {
		x = errors.String(err.(errors.Error))
	}
	if !cmp.Equal(x, want, cmpopts.EquateEmpty()) {
		t.Error(cmp.Diff(want, x))
	}
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
		out:  `#FileInfo.encoding: non-concrete value string`,
	}, {
		// Default without any
		name: "cue",
		in: build.File{
			Filename: "",
			Encoding: build.CUE,
		},
		mode: Def,
		out: &FileInfo{
			File: &build.File{
				Filename: "",
				Encoding: "cue",
				Form:     "schema",
			},
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
		name: "yaml",
		mode: Def,
		in: build.File{
			Filename: "foo.yaml",
		},
		out: &FileInfo{
			File: &build.File{
				Filename: "foo.yaml",
				Encoding: "yaml",
				Form:     "graph",
			},
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
			Interpretation: "openapi",
		},
		out: &FileInfo{
			File: &build.File{
				Filename:       "foo.yaml",
				Encoding:       "yaml",
				Interpretation: "openapi",
				Form:           "schema",
			},
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
		name: "JSONDefault",
		mode: Input,
		in: build.File{
			Filename: "data.json",
		},
		out: &FileInfo{
			File: &build.File{
				Filename:       "data.json",
				Encoding:       "json",
				Interpretation: "auto",
				Form:           "schema",
			},
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
		name: "JSONSchema",
		in: build.File{
			Filename:       "foo.json",
			Interpretation: "jsonschema",
		},
		out: &FileInfo{
			File: &build.File{
				Filename:       "foo.json",
				Encoding:       "json",
				Interpretation: "jsonschema",
				Form:           "schema",
			},
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
		name: "JSONOpenAPI",
		in: build.File{
			Filename:       "foo.json",
			Interpretation: "openapi",
		},
		mode: Def,
		out: &FileInfo{
			File: &build.File{
				Filename:       "foo.json",
				Encoding:       "json",
				Interpretation: "openapi",
				Form:           "schema",
			},
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
		name: "OpenAPIDefaults",
		in: build.File{
			Filename:       "-",
			Interpretation: "openapi",
		},
		mode: Def,
		out: &FileInfo{
			File: &build.File{
				Filename:       "-",
				Encoding:       "json",
				Interpretation: "openapi",
				Form:           "schema",
			},
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
		name: "Go",
		in: build.File{
			Filename: "foo.go",
		},
		mode: Def,
		out: &FileInfo{
			File: &build.File{
				Filename: "foo.go",
				Encoding: "code",
				Form:     "schema",
				Tags:     map[string]string{"lang": "go"},
			},
			Definitions:  true,
			Data:         true,
			Optional:     true,
			Constraints:  true,
			References:   true,
			Cycles:       true,
			KeepDefaults: true,
			Incomplete:   true,
			Imports:      true,
			Stream:       false,
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
	t.Skip("fix error messages")
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
			Encoding:       "json",
			Interpretation: "auto",
		},
	}, {
		in:   "file.json",
		mode: Def,
		out: &build.File{
			Filename: "file.json",
			Encoding: "json",
		},
	}, {
		in: "schema:file.json",
		out: &build.File{
			Filename:       "file.json",
			Encoding:       "json",
			Interpretation: "auto",
			Form:           "schema",
		},
	}, {
		in: "openapi:-",
		out: &build.File{
			Filename:       "-",
			Encoding:       "json",
			Interpretation: "openapi",
		},
	}, {
		in: "cue:file.json",
		out: &build.File{
			Filename: "file.json",
			Encoding: "cue",
		},
	}, {
		in: "cue+schema:-",
		out: &build.File{
			Filename: "-",
			Encoding: "cue",
			Form:     "schema",
		},
	}, {
		in: "code+lang=js:foo.x",
		out: &build.File{
			Filename: "foo.x",
			Encoding: "code",
			Tags:     map[string]string{"lang": "js"},
		},
	}, {
		in:  "foo:file.bar",
		out: `cue: marshal error: tags: value "foo" not found`,
	}, {
		in:  "file.bar",
		out: `cue: marshal error: extensions: value ".bar" not found`,
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
				Encoding:       "json",
				Interpretation: "auto",
			},
			{
				Filename:       "baz.yaml",
				Encoding:       "yaml",
				Interpretation: "auto",
			},
		},
	}, {
		in: "data: foo.cue",
		out: []*build.File{
			{Filename: "foo.cue", Encoding: "cue", Form: "data"},
		},
	}, {
		in: "json: foo.json bar.data jsonschema: bar.schema",
		out: []*build.File{
			{Filename: "foo.json", Encoding: "json"}, // no auto!
			{Filename: "bar.data", Encoding: "json"},
			{
				Filename:       "bar.schema",
				Encoding:       "json",
				Interpretation: "jsonschema",
			},
		},
	}, {
		in: `json: c:\foo.json c:\path\to\file.dat`,
		out: []*build.File{
			{Filename: `c:\foo.json`, Encoding: "json"},
			{Filename: `c:\path\to\file.dat`, Encoding: "json"},
		},
	}, {
		in:  "json: json+schema: bar.schema",
		out: `scoped qualifier "json:" without file`,
	}, {
		in:  "json:",
		out: `scoped qualifier "json:" without file`,
	}, {
		in:  "json:foo:bar.yaml",
		out: `unsupported file name "json:foo:bar.yaml": may not have ':'`,
	}}
	for _, tc := range testCases {
		t.Run(tc.in, func(t *testing.T) {
			files, err := ParseArgs(strings.Split(tc.in, " "))
			check(t, tc.out, files, err)
		})
	}
}
