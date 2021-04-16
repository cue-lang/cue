// Copyright 2019 CUE Authors
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

package os

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/task"
	"cuelang.org/go/internal/value"
)

func TestGetenv(t *testing.T) {

	for _, p := range [][2]string{
		{"CUEOSTESTMOOD", "yippie"},
		{"CUEOSTESTTRUE", "True"},
		{"CUEOSTESTFALSE", "0"},
		{"CUEOSTESTBI", "1"},
		{"CUEOSTESTNUM", "34K"},
		{"CUEOSTESTNUMD", "not a num"},
		{"CUEOSTESTMULTI", "10"},
	} {
		os.Setenv(p[0], p[1])
	}

	config := `{
		CUEOSTESTMOOD:  string
		CUEOSTESTTRUE:  bool
		CUEOSTESTFALSE: bool | string
		CUEOSTESTBI: 	*bool | int,
		CUEOSTESTNUM:   int
		CUEOSTESTNUMD:  *int | *bool | string
		CUEOSTESTMULTI: *<10 | string
		CUEOSTESTNULL:  int | null
	}`

	want := map[string]interface{}{
		"CUEOSTESTMOOD": ast.NewString("yippie"),
		"CUEOSTESTTRUE": ast.NewBool(true),
		"CUEOSTESTFALSE": &ast.BinaryExpr{
			Op: token.OR,
			X:  ast.NewBool(false),
			Y:  ast.NewString("0"),
		},
		"CUEOSTESTBI": &ast.BinaryExpr{
			Op: token.OR,
			X:  ast.NewLit(token.INT, "1"),
			Y:  ast.NewBool(true),
		},
		"CUEOSTESTNUM":  &ast.BasicLit{Kind: token.INT, Value: "34K"},
		"CUEOSTESTNUMD": ast.NewString("not a num"),
		"CUEOSTESTMULTI": &ast.BinaryExpr{
			Op: token.OR,
			X:  ast.NewLit(token.INT, "10"),
			Y:  ast.NewString("10"),
		},
		"CUEOSTESTNULL": nil,
	}

	for _, tc := range []struct {
		pkg    string
		runner task.Runner
	}{
		{"tool/os.Getenv", &getenvCmd{}},
		{"tool/os.Environ", &environCmd{}},
	} {
		v := parse(t, tc.pkg, config)
		got, err := tc.runner.Run(&task.Context{Obj: v})
		if err != nil {
			t.Fatal(err)
		}

		var opts = []cmp.Option{
			cmpopts.IgnoreFields(ast.BinaryExpr{}, "OpPos"),
			cmpopts.IgnoreFields(ast.BasicLit{}, "ValuePos"),
			cmpopts.IgnoreUnexported(ast.BasicLit{}, ast.BinaryExpr{}),
			// For ignoring addinonal entries from os.Environ:
			cmpopts.IgnoreMapEntries(func(s string, x interface{}) bool {
				_, ok := want[s]
				return !ok
			}),
		}

		if !cmp.Equal(got, want, opts...) {
			t.Error(cmp.Diff(got, want, opts...))
		}

		// Errors:
		for _, etc := range []struct{ config, err string }{{
			config: `{ CUEOSTESTNULL:  [...string] }`,
			err:    "expected unsupported type error",
		}, {
			config: `{ CUEOSTESTNUMD: int }`,
			err:    "expected invalid number error",
		}, {
			config: `{ CUEOSTESTNUMD: null }`,
			err:    "expected invalid type",
		}} {
			t.Run(etc.err, func(t *testing.T) {
				v = parse(t, tc.pkg, etc.config)
				if _, err = tc.runner.Run(&task.Context{Obj: v}); err == nil {
					t.Error(etc.err)
				}
			})
		}
	}
}

func parse(t *testing.T, kind, expr string) cue.Value {
	t.Helper()

	x, err := parser.ParseExpr("test", expr)
	if err != nil {
		errors.Print(os.Stderr, err, nil)
		t.Fatal(err)
	}
	var r cue.Runtime
	i, err := r.CompileExpr(x)
	if err != nil {
		t.Fatal(err)
	}
	return value.UnifyBuiltin(i.Value(), kind)
}
