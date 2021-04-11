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

package export_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
)

func TestExtract(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "doc",
		Update: cuetest.UpdateGoldenFiles,
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, err := compile.Files(nil, r, "", a[0].Files...)
		if err != nil {
			t.Fatal(err)
		}

		ctx := eval.NewContext(r, v)
		v.Finalize(ctx)

		writeDocs(t, r, v, nil)
	})
}

func writeDocs(t *cuetxtar.Test, r adt.Runtime, v *adt.Vertex, path []string) {
	fmt.Fprintln(t, path)
	for _, c := range export.ExtractDoc(v) {
		fmt.Fprintln(t, "-", c.Text())
	}

	for _, a := range v.Arcs {
		writeDocs(t, r, a, append(path, a.Label.SelectorString(r)))
	}
}
