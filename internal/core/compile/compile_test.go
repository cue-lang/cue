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
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if cuetest.UpdateGoldenFiles() {
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

func syncTestdataInputsCUE(t *testing.T) {
	t.Helper()

	const srcRoot = "../../../cue/testdata"
	const dstRoot = "testdata/sync"

	if err := os.RemoveAll(dstRoot); err != nil {
		t.Fatal(err)
	}

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
			data, err := cuetxtar.StripTestAttrs(f.Data)
			if err != nil {
				return err
			}
			archive.Files = append(archive.Files, txtar.File{Name: f.Name, Data: data})
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0777); err != nil {
			return err
		}
		return os.WriteFile(dstPath, txtar.Format(archive), 0666)
	})
	if err != nil {
		t.Fatal(err)
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
