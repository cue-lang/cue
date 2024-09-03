// Copyright 2023 CUE Authors
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

package wasm_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/interpreter/wasm"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
)

// TestWasm tests Wasm using the API.
func TestWasm(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/cue",
		Name: "wasm",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		ctx := cuecontext.New(cuecontext.Interpreter(wasm.New()))

		if t.HasTag("cuecmd") {
			// if #cuecmd is set the test is only for the CUE command,
			// not the API, so skip it.
			return
		}

		bi := t.Instance()

		v := ctx.BuildInstance(bi)
		err := v.Validate()

		if t.HasTag("error") {
			// if #error is set we're checking for a specific error,
			// so only print the error then bail out.
			for _, err := range errors.Errors(err) {
				t.WriteErrors(err)
			}
			return
		}

		// we got an unexpected error. print both the error
		// and the CUE value, to aid debugging.
		if err != nil {
			fmt.Fprintln(t, "Errors:")
			for _, err := range errors.Errors(err) {
				t.WriteErrors(err)
			}
			fmt.Fprintln(t, "")
			fmt.Fprintln(t, "Result:")
		}

		syntax := v.Syntax(
			cue.Attributes(false), cue.Final(), cue.Definitions(true), cue.ErrorsAsValues(true),
		)
		file, err := astutil.ToFile(syntax.(ast.Expr))
		if err != nil {
			t.Fatal(err)
		}

		got, err := format.Node(file, format.UseSpaces(4), format.TabIndent(false))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(t, string(got))
	})
}

func copyWasmFiles(t *testing.T, dstDir, srcDir string) {
	filepath.WalkDir(dstDir, func(path string, d fs.DirEntry, err error) error {
		if filepath.Ext(path) != ".wasm" {
			return nil
		}
		relPath := must(filepath.Rel(dstDir, path))(t)
		from := filepath.Join(srcDir, relPath)
		copyFile(t, path, from)
		return nil
	})
}

func copyFile(t *testing.T, dst, src string) {
	buf := must(os.ReadFile(src))(t)
	err := os.WriteFile(dst, buf, 0664)
	if err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, dir string, got string) {
	golden := filepath.Join("testdata", dir) + ".golden"

	if cuetest.UpdateGoldenFiles {
		os.WriteFile(golden, []byte(got), 0666)
	}

	want := string(must(os.ReadFile(golden))(t))
	if got != want {
		t.Errorf("want %v, got %v", want, got)
	}
}

func loadDir(t *testing.T, name string) cue.Value {
	ctx := cuecontext.New(cuecontext.Interpreter(wasm.New()))
	bi := dirInstance(t, name)
	return ctx.BuildInstance(bi)
}

func dirInstance(t *testing.T, name string) *build.Instance {
	ctx := build.NewContext(build.ParseFile(loadFile))
	inst := ctx.NewInstance(name, nil)

	files := must(os.ReadDir(name))(t)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), "cue") {
			inst.AddFile(filepath.Join(name, f.Name()), nil)
		}
		if strings.HasSuffix(f.Name(), "wasm") {
			f := &build.File{
				Filename: f.Name(),
			}
			inst.UnknownFiles = append(inst.UnknownFiles, f)
		}
	}
	inst.Complete()
	return inst
}

func loadFile(filename string, src any) (*ast.File, error) {
	return parser.ParseFile(filename, src, parser.ParseFuncs)
}

func must[T any](v T, err error) func(t *testing.T) T {
	fail := false
	if err != nil {
		fail = true
	}
	return func(t *testing.T) T {
		if fail {
			_, file, line, _ := stdruntime.Caller(1)
			file = filepath.Base(file)
			t.Fatalf("unexpected error at %v:%v: %v", file, line, err)
		}
		return v
	}
}
