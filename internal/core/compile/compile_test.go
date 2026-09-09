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

package compile_test

import (
	"bytes"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"golang.org/x/tools/txtar"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

func TestCompile(t *testing.T) {
	if cuetest.UpdateOrDiffGoldenFiles() {
		syncTestdataInputsCUE(t)
	}

	test := cuetxtar.TxTarTest{
		Root: "testdata",
		Name: "compile",
	}

	if *todo {
		test.ToDo = nil
	}

	test.Run(t, func(t *cuetxtar.Test) {
		r := runtime.New()
		// TODO: use high-level API.

		a := t.Instance()

		v, err := compile.Instance(nil, r, a)

		// Write the results.
		t.WriteErrors(err)

		if v == nil {
			return
		}

		for i, f := range a.Files {
			if i > 0 {
				fmt.Fprintln(t)
			}
			fmt.Fprintln(t, "---", t.Rel(f.Filename))
			t.Write(debug.AppendNode(nil, r, v.ConjunctAt(i).Elem(), &debug.Config{
				Cwd: t.Dir,

				ExpandLetExpr: t.Bool("expandLetExpr"),
			}))
		}
	})
}

func TestOpenFunctionBodyRejected(t *testing.T) {
	file, err := parser.ParseFile("test.cue", `
@experiment(functions)
f: func(a: int) -> int: a
`)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.Func
	ast.Walk(file, func(n ast.Node) bool {
		if x, ok := n.(*ast.Func); ok {
			fn = x
			return false
		}
		return true
	}, nil)
	if fn == nil {
		t.Fatal("function literal not found")
	}
	// Mutate the parsed AST to exercise the compiler's defensive check without
	// asking the parser to accept the invalid source form.
	fn.Ellipsis = fn.Rparen

	r := runtime.New()
	_, err = compile.Files(nil, r, "", file)
	if err == nil || !strings.Contains(err.Error(), "open function signature cannot have a body") {
		t.Fatalf("compile error = %v; want open-body rejection", err)
	}
}

func syncTestdataInputsCUE(t *testing.T) {
	t.Helper()

	const srcRoot = "../../../cue/testdata"
	const dstRoot = "testdata/sync"

	// The mirrored archives by destination path.
	want := make(map[string][]byte)
	err := filepath.WalkDir(srcRoot, func(srcPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(srcPath) != ".txtar" {
			return nil
		}
		srcArchive, err := txtar.ParseFile(srcPath)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, srcPath)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dstRoot, rel)

		// A source archive may pin an older language version in its module
		// file, for syntax which the latest version no longer accepts;
		// parse its files under that version.
		langVersion := cuetxtar.ArchiveLanguageVersion(srcArchive)

		archive := &txtar.Archive{Comment: bytes.Clone(srcArchive.Comment)}
		for _, f := range srcArchive.Files {
			if !strings.HasSuffix(f.Name, ".cue") {
				continue
			}
			// Strip @test attributes from the source before mirroring it.
			// They are inline-test assertions for the evalalpha runner and
			// have no effect on compile output, but leaving them in causes
			// the compile goldens to churn whenever a @test directive is
			// added, updated, or removed.
			data, err := cuetxtar.StripTestAttrs(f.Data, langVersion)
			if err != nil {
				return err
			}
			archive.Files = append(archive.Files, txtar.File{Name: f.Name, Data: data})
		}
		want[dstPath] = txtar.Format(archive)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mirrors whose source archive is gone are removed when updating, and
	// reported under CUE_UPDATE=diff.
	err = filepath.WalkDir(dstRoot, func(dstPath string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if _, ok := want[dstPath]; ok {
			return nil
		}
		if cuetest.DiffGoldenFiles() {
			t.Errorf("%s is stale; CUE_UPDATE=1 would remove it", dstPath)
			return nil
		}
		return os.Remove(dstPath)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dstPath := range slices.Sorted(maps.Keys(want)) {
		cuetxtar.UpdateInputs(t, dstPath, want[dstPath])
	}
}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	in := `
	`

	if strings.TrimSpace(in) == "" {
		t.Skip()
	}

	file, err := parser.ParseFile("TestX", in)
	if err != nil {
		t.Fatal(err)
	}
	r := runtime.New()

	arc, err := compile.Files(nil, r, "", file)
	if err != nil {
		t.Error(errors.Details(err, nil))
	}
	t.Error(debug.NodeString(r, arc.ConjunctAt(0).Elem(), nil))
}
