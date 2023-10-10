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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/interpreter/wasm"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"

	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
)

// We are using TestMain because we want to ensure Wasm is enabled and
// works as expected with the command-line tool.
func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": cmd.MainTest,
	}))
}

// TestExe tests Wasm using the command-line tool.
func TestExe(t *testing.T) {
	root := must(filepath.Abs("testdata"))(t)
	p := testscript.Params{
		Dir:                 "testdata",
		UpdateScripts:       cuetest.UpdateGoldenFiles,
		RequireExplicitExec: true,
		Setup: func(e *testscript.Env) error {
			copyWasmFiles(t, e.WorkDir, root)
			return nil
		},
		Condition: cuetest.Condition,
	}
	if err := gotooltest.Setup(&p); err != nil {
		t.Fatal(err)
	}
	testscript.Run(t, p)
}

// TestWasm tests Wasm using the API.
func TestWasm(t *testing.T) {
	files := must(os.ReadDir("testdata"))(t)
	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		dir := f.Name()
		t.Run(dir, func(t *testing.T) {
			v := loadDir(t, filepath.Join("testdata", dir))
			got := must(format.Node(v.Syntax(
				cue.All(), cue.Concrete(true), cue.ErrorsAsValues(true),
			)))(t)
			check(t, dir, string(got))
		})
	}
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
	err := os.WriteFile(dst, buf, 0o664)
	if err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, dir, got string) {
	golden := filepath.Join("testdata", dir) + ".golden"

	if cuetest.UpdateGoldenFiles {
		os.WriteFile(golden, []byte(got), 0o644)
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
			_, file, line, _ := runtime.Caller(1)
			file = filepath.Base(file)
			t.Fatalf("unexpected error at %v:%v: %v", file, line, err)
		}
		return v
	}
}
