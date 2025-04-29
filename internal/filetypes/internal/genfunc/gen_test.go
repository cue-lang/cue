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

func compileGo(t *testing.T, code []byte) string {
	d := t.TempDir()
	src := filepath.Join(d, "test.go")
	err := os.WriteFile(src, code, 0o666)
	qt.Assert(t, qt.IsNil(err))

	// It's not easy for the generated code to import
	// the internal opt package, so put the code in a module
	// and add that package.
	err = os.WriteFile(filepath.Join(d, "go.mod"), []byte("module example.com/test\ngo 1.23.0\n"), 0o6666)
	qt.Assert(t, qt.IsNil(err))
	err = os.Mkdir(filepath.Join(d, "opt"), 0o777)
	qt.Assert(t, qt.IsNil(err))
	optImpl, err := os.ReadFile("../opt/opt.go")
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(filepath.Join(d, "opt", "opt.go"), optImpl, 0o666)
	qt.Assert(t, qt.IsNil(err))

	exe := filepath.Join(d, "genfunc_test")
	cmd := exec.Command("go", "build", "-o", exe, src)
	cmd.Dir = d
	out, err := cmd.CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("output: %s", out))
	return exe
}
