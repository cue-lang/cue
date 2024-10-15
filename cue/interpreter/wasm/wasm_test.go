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
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/interpreter/wasm"
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
