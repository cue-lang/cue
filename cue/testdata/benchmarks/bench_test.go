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

package benchmarks

import (
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"golang.org/x/tools/txtar"
)

func Benchmark(b *testing.B) {
	entries, err := os.ReadDir(".")
	if err != nil {
		b.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".txtar" {
			continue
		}

		a, err := txtar.ParseFile(name)
		if err != nil {
			b.Fatal(err)
		}

		inst := cuetxtar.Load(a, b.TempDir())[0]
		if inst.Err != nil {
			b.Fatal(inst.Err)
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
			os.WriteFile(name, txtar.Format(a), info.Mode())
		}

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				inst := cue.Build(cuetxtar.Load(a, b.TempDir()))[0]
				if inst.Err != nil {
					b.Fatal(inst.Err)
				}

				inst.Value().Validate()
			}
		})
	}
}
