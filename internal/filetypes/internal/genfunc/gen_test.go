// Copyright 2025 CUE Authors
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

package genfunc

import (
	"bytes"
	_ "embed"
	"encoding/json"
	goformat "go/format"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/cuecontext"
)

//go:embed testharness.go.tmpl
var testHarnessTmplData string

var testHarnessTmpl = template.Must(template.New("testharness.go.tmpl").Parse(testHarnessTmplData))

type tmplParams struct {
	TypeName   string
	StructName string
	FuncName   string
}

type subtest struct {
	testName string
	data     any
	want     any
}

var generateGoFuncForCUEStructTests = []struct {
	testName   string
	funcName   string
	structName string
	keys       []string
	typeName   string
	cue        string
	wantError  bool
	subtests   []subtest
}{{
	testName:   "StringTags",
	structName: "tags",
	funcName:   "unify",
	keys:       []string{"lang"},
	typeName:   "string",
	cue: `
{
	{
		[string]: string
	}
	lang: (*"" | string) & {
		"go"
	}
}`,
	subtests: []subtest{{
		testName: "Empty",
		data:     map[string]string{},
		want: subtestData(map[string]string{
			"lang": "go",
		}),
	}, {
		testName: "Conflict",
		data: map[string]string{
			"lang": "other",
		},
		want: result[string]{
			Error: `unify: conflict on lang; "other" provided but need "go"`,
		},
	}},
}, {
	testName:   "BoolTags",
	funcName:   "unify",
	structName: "boolTags",
	keys: []string{
		"a",
		"b",
		"c",
	},
	typeName: "bool",
	cue: `
{
	_#def
	_#def: {
		{
			[string]: bool
		}
		c:         *false | bool
		b: *c | bool
		a: *c | bool
	}
}`,
	subtests: []subtest{{
		testName: "Empty",
		data:     map[string]bool{},
		want: subtestData(map[string]bool{
			"a": false,
			"b": false,
			"c": false,
		}),
	}, {
		testName: "DependencyPropagation",
		data: map[string]bool{
			"c": true,
		},
		want: subtestData(map[string]bool{
			"a": true,
			"b": true,
			"c": true,
		}),
	}, {
		testName: "DependencyPropagationOverride",
		data: map[string]bool{
			"c": true,
			"a": false,
		},
		want: subtestData(map[string]bool{
			"a": false,
			"b": true,
			"c": true,
		}),
	}},
}}

// Note: duplicated in testharness.go.tmpl
type result[T any] struct {
	Error string       `json:"error,omitempty"`
	Data  map[string]T `json:"data,omitempty"`
}

func subtestData[T any](m map[string]T) result[T] {
	return result[T]{
		Data: m,
	}
}

func TestGenerateGoFuncForCUEStruct(t *testing.T) {
	ctx := cuecontext.New()
	for _, test := range generateGoFuncForCUEStructTests {
		t.Run(test.testName, func(t *testing.T) {
			v := ctx.CompileString(test.cue)
			qt.Assert(t, qt.IsNil(v.Err()))
			var buf bytes.Buffer
			err := testHarnessTmpl.Execute(&buf, tmplParams{
				StructName: test.structName,
				TypeName:   test.typeName,
				FuncName:   test.funcName,
			})
			qt.Assert(t, qt.IsNil(err))

			GenerateGoTypeForFields(&buf, test.structName, test.keys, test.typeName)
			GenerateGoFuncForCUEStruct(&buf, test.funcName, test.structName, v, test.keys, test.typeName)
			data, err := goformat.Source(buf.Bytes())
			if test.wantError {
				if err == nil {
					t.Errorf("expected invalid Go source, but it formats OK")
				}
			} else {
				qt.Check(t, qt.IsNil(err))
			}
			if err != nil {
				data = buf.Bytes()
			}

			if test.wantError {
				return
			}

			t.Logf("running code: {\n%s\n}", data)

			exe := compileGo(t, data)
			for _, test := range test.subtests {
				t.Run(test.testName, func(t *testing.T) {
					data, err := json.Marshal(test.data)
					qt.Assert(t, qt.IsNil(err))
					out, err := exec.Command(exe, string(data)).CombinedOutput()
					qt.Assert(t, qt.IsNil(err))
					qt.Assert(t, qt.JSONEquals(out, test.want))
				})
			}
		})
	}
}

// overlay represents the JSON expected by the go build -overlay flag.
type overlay struct {
	Replace map[string]string
}

func compileGo(t *testing.T, code []byte) string {
	d := t.TempDir()
	src := filepath.Join(d, "test.go")
	err := os.WriteFile(src, code, 0o666)
	qt.Assert(t, qt.IsNil(err))

	// Use `go build -overlay` to create a file that
	// pretends to be part of the current module, making
	// it possible to import internal packages.

	const overlayGoFile = "./generated_test_code.go"
	data, err := json.Marshal(overlay{
		Replace: map[string]string{
			overlayGoFile: src,
		},
	})
	qt.Assert(t, qt.IsNil(err))
	overlayFile := filepath.Join(d, "overlay.json")
	err = os.WriteFile(overlayFile, data, 0o666)
	qt.Assert(t, qt.IsNil(err))

	// Note: include .exe extension so it works under Windows.
	exe := filepath.Join(d, "genfunc_test.exe")
	cmd := exec.Command("go", "build",
		"-overlay", overlayFile,
		"-o", exe,
		overlayGoFile,
	)
	out, err := cmd.CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %s", out))
	return exe
}
