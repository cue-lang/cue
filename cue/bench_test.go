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

package cue_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"golang.org/x/tools/txtar"
)

var (
	matrix = cuetdtest.FullMatrix
)

func Benchmark(b *testing.B) {
	root := "testdata/benchmarks"
	err := filepath.WalkDir(root, func(fullpath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || filepath.Ext(fullpath) != ".txtar" {
			return nil
		}

		a, err := txtar.ParseFile(fullpath)
		if err != nil {
			return err
		}

		inst := cuetxtar.Load(a, b.TempDir())[0]
		if inst.Err != nil {
			return inst.Err
		}

		r := runtime.New()
		v, err := r.Build(nil, inst)
		if err != nil {
			b.Fatal(err)
		}
		e := eval.New(r)
		ctx := e.NewContext(v)
		v.Finalize(ctx)

		if cuetest.UpdateGoldenFiles {
			const statsFile = "stats.txt"
			var stats txtar.File
			var statsPos int
			for i, f := range a.Files {
				if f.Name == statsFile {
					stats = f
					statsPos = i
					break
				}
			}
			if stats.Name == "" {
				// At stats.txt as the first file.
				a.Files = append([]txtar.File{{
					Name: statsFile,
				}}, a.Files...)
			}

			a.Files[statsPos].Data = []byte(ctx.Stats().String() + "\n\n")

			info, err := entry.Info()
			if err != nil {
				b.Fatal(err)
			}
			os.WriteFile(fullpath, txtar.Format(a), info.Mode())
		}

		b.Run(entry.Name(), func(b *testing.B) {
			for _, m := range matrix {
				b.Run(m.Name(), func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						ctx := m.Context()
						value := ctx.BuildInstance(cuetxtar.Load(a, b.TempDir())[0])
						value.Validate()
					}
				})
			}
		})
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
}
