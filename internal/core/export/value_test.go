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

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/rogpeppe/go-internal/txtar"
)

var exclude = map[string]string{
	"scalardef": "incomplete",
}

func TestValue(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "value",
		Update: cuetest.UpdateGoldenFiles,
		Skip:   exclude,
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		pkgID := a[0].ID()

		v, err := r.Build(nil, a[0])
		if err != nil {
			t.Fatal(err)
		}

		ctx := eval.NewContext(r, v)
		v.Finalize(ctx)

		all := export.All
		all.ShowErrors = true

		evalWithOptions := export.Profile{
			TakeDefaults:    true,
			ShowOptional:    true,
			ShowDefinitions: true,
			ShowAttributes:  true,
		}

		for _, tc := range []struct {
			name string
			fn   func(r adt.Runtime, id string, v adt.Value) (ast.Expr, errors.Error)
		}{
			{"Simplified", export.Simplified.Value},
			{"Raw", export.Raw.Value},
			{"Final", export.Final.Value},
			{"All", all.Value},
			{"Eval", evalWithOptions.Value},
		} {
			fmt.Fprintln(t, "==", tc.name)
			x, errs := tc.fn(r, pkgID, v)
			errors.Print(t, errs, nil)
			_, _ = t.Write(formatNode(t.T, x))
			fmt.Fprintln(t)
		}
	})
}

// For debugging purposes. Do not delete.
func TestValueX(t *testing.T) {
	t.Skip()

	in := `
-- in.cue --
	`

	archive := txtar.Parse([]byte(in))
	a := cuetxtar.Load(archive, "/tmp/test")

	r := runtime.New()
	v, errs := compile.Files(nil, r, "", a[0].Files...)
	if errs != nil {
		t.Fatal(errs)
	}

	ctx := eval.NewContext(r, v)
	v.Finalize(ctx)

	p := export.All
	p.ShowErrors = true

	p = &export.Profile{
		TakeDefaults:    true,
		ShowOptional:    true,
		ShowDefinitions: true,
		ShowAttributes:  true,
	}

	x, errs := p.Value(r, "main", v)
	if errs != nil {
		t.Fatal(errs)
	}

	t.Error(string(formatNode(t, x)))
}
