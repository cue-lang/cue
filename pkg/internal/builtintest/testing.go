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

package builtintest

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/validate"
	"cuelang.org/go/internal/cuetxtar"
)

func Run(name string, t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: name,
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, errs := r.Build(nil, a[0])
		if errs != nil {
			t.Fatal(errs)
		}

		e := eval.New(r)
		ctx := e.NewContext(v)
		v.Finalize(ctx)

		if b := validate.Validate(ctx, v, &validate.Config{
			AllErrors: true,
		}); b != nil {
			fmt.Fprintln(t, "Errors:")
			t.WriteErrors(b.Err)
			fmt.Fprintln(t, "")
			fmt.Fprintln(t, "Result:")
		}

		p := export.All
		p.ShowErrors = true

		files, errs := p.Vertex(r, test.Name, v)
		if errs != nil {
			t.Fatal(errs)
		}

		b, err := format.Node(files)
		if err != nil {
			t.Fatal(err)
		}

		fmt.Fprintln(t, string(b))
	})
}
