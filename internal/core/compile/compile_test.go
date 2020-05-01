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
	"flag"
	"fmt"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
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

func TestCompile(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "../../../cue/testdata/",
		Name:   "compile",
		Update: *update,
		Skip:   alwaysSkip,
		ToDo:   needFix,
	}

	if *todo {
		test.ToDo = nil
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		// TODO: use high-level API.

		a := t.ValidInstances()

		v, err := compile.Files(nil, r, a[0].Files...)

		// Write the results.
		t.WriteErrors(err)

		if v == nil {
			return
		}

		for i, f := range a[0].Files {
			if i > 0 {
				fmt.Fprintln(t)
			}
			fmt.Fprintln(t, "---", t.Rel(f.Filename))
			debug.WriteNode(t, r, v.Conjuncts[i].Expr(), &debug.Config{
				Cwd: t.Dir,
			})
		}
		fmt.Fprintln(t)
	})
}

var alwaysSkip = map[string]string{
	"fulleval/031_comparison against bottom": "fix bin op binding in test",
}

var needFix = map[string]string{
	"export/020":                           "builtin",
	"fulleval/027_len_of_incomplete_types": "builtin",
	"fulleval/032_or_builtin_should_not_fail_on_non-concrete_empty_list": "builtin",
	"fulleval/053_issue312":       "builtin",
	"resolve/034_closing_structs": "builtin",
	"resolve/048_builtins":        "builtin",

	"fulleval/026_dont_convert_incomplete_errors_to_non-incomplete": "import",
	"fulleval/044_Issue_#178":                              "import",
	"fulleval/048_dont_pass_incomplete_values_to_builtins": "import",
	"fulleval/049_alias_reuse_in_nested_scope":             "import",
	"fulleval/050_json_Marshaling_detects_incomplete":      "import",
	"fulleval/051_detectIncompleteYAML":                    "import",
	"fulleval/052_detectIncompleteJSON":                    "import",
	"fulleval/056_issue314":                                "import",
	"resolve/013_custom_validators":                        "import",
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

	arc, err := compile.Files(nil, r, file)
	if err != nil {
		t.Error(errors.Details(err, nil))
	}
	t.Error(debug.NodeString(r, arc.Conjuncts[0].Expr(), nil))
}
