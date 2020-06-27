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

package eval

import (
	"flag"
	"fmt"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/pkg/strings"
)

var (
	update = flag.Bool("update", false, "update the test files")
	todo   = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

func TestEval(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "../../../cue/testdata",
		Name:   "eval",
		Update: *update,
		Skip:   alwaysSkip,
		ToDo:   needFix,
	}

	if *todo {
		test.ToDo = nil
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, err := compile.Files(nil, r, a[0].Files...)
		if err != nil {
			t.Fatal(err)
		}

		e := Evaluator{
			r:     r,
			index: r,
		}

		err = e.Eval(v)
		t.WriteErrors(err)

		if v == nil {
			return
		}

		debug.WriteNode(t, r, v, &debug.Config{Cwd: t.Dir})
		fmt.Fprintln(t)
	})
}

var alwaysSkip = map[string]string{
	"compile/erralias": "compile error",
}

var needFix = map[string]string{
	"fulleval/048_dont_pass_incomplete_values_to_builtins": "import",
	"fulleval/050_json_Marshaling_detects_incomplete":      "import",
	"fulleval/051_detectIncompleteYAML":                    "import",
	"fulleval/052_detectIncompleteJSON":                    "import",
	"fulleval/056_issue314":                                "import",
	"resolve/013_custom_validators":                        "import",

	"export/027": "cycle",
	"export/028": "cycle",
	"export/030": "cycle",

	"cycle/025_cannot_resolve_references_that_would_be_ambiguous": "cycle",

	"resolve/034_closing_structs": "close()",
	"fulleval/053_issue312":       "close()",
}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	t.Skip()
	in := `
	// max: >99 | *((5|*1) & 5)
	// *( 5 | *_|_ )
	// 1 | *((5|*1) & 5)


	max: >= (num+0) | * (num+0)
	res: !=4 | * 1
	num:  *(1+(res+0)) | >(res+0)

    // (1 | *2 | 3) & (1 | 2 | *3)

	// m1: (*1 | (*2 | 3)) & (>=2 & <=3)
	// m2: (*1 | (*2 | 3)) & (2 | 3)
	// m3: (*1 | *(*2 | 3)) & (2 | 3)
	// b: (*"a" | "b") | "c"
	// {a: 1} | {b: 2}
	`

	if strings.TrimSpace(in) == "" {
		t.Skip()
	}

	file, err := parser.ParseFile("TestX", in)
	if err != nil {
		t.Fatal(err)
	}
	r := runtime.New()

	b, err := format.Node(file)
	_, _ = b, err
	// fmt.Println(string(b), err)

	v, err := compile.Files(nil, r, file)
	if err != nil {
		t.Fatal(err)
	}

	ctx := NewContext(r, v)

	ctx.Unify(ctx, v, adt.Finalized)
	// if err != nil {
	// 	t.Fatal(err)
	// }

	t.Error(debug.NodeString(r, v, nil))
}
