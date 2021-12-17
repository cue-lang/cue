// Copyright 2021 CUE Authors
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

package cuecontext

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
)

func TestAPI(t *testing.T) {
	testCases := []struct {
		name string
		fun  func() cue.Value
		want string
	}{{
		name: "issue1204",
		fun: func() cue.Value {
			ctx := New()
			expr := ast.NewCall(ast.NewIdent("close"), ast.NewStruct())
			return ctx.BuildExpr(expr)
		},
		want: `close({})`,
	}, {
		name: "issue1131",
		fun: func() cue.Value {
			m := make(map[string]interface{})
			ctx := New()
			cv := ctx.Encode(m)
			return cv
		},
		want: "", // empty file.
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := fmt.Sprintf("%#v", tc.fun())
			if got != tc.want {
				t.Errorf("got:\n%v;\nwant:\n%v", got, tc.want)
			}
		})
	}
}

// TestConcurrency tests whether concurrent use of an index is allowed.
// This test only functions well with the --race flag.
func TestConcurrency(t *testing.T) {
	c := New()
	go func() {
		c.CompileString(`
		package foo
		a: 1
		`)
	}()
	go func() {
		c.CompileString(`
		package bar
		a: 2
		`)
	}()
}
