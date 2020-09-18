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
	"io/ioutil"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/rogpeppe/go-internal/txtar"
)

func Benchmark(b *testing.B) {
	files, err := ioutil.ReadDir(".")
	if err != nil {
		b.Fatal(err)
	}

	for _, fi := range files {
		name := fi.Name()
		if fi.IsDir() || filepath.Ext(name) != ".txtar" {
			continue
		}

		a, err := txtar.ParseFile(name)
		if err != nil {
			b.Fatal(err)
		}

		inst := cue.Build(cuetxtar.Load(a, "/cuetest"))[0]
		if inst.Err != nil {
			b.Fatal(inst.Err)
		}

		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				inst := cue.Build(cuetxtar.Load(a, "/cuetest"))[0]
				if inst.Err != nil {
					b.Fatal(inst.Err)
				}

				inst.Value().Validate()
			}
		})
	}
}
