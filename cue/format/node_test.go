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

package format

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// TestInvalidAST verifies behavior for various invalid AST inputs. In some
// cases it is okay to be permissive, as long as the output is correct.
func TestInvalidAST(t *testing.T) {
	ident := func(s string) *ast.Ident {
		return &ast.Ident{NamePos: token.NoSpace.Pos(), Name: s}
	}
	testCases := []struct {
		desc string
		node ast.Node
		out  string
	}{{
		desc: "label sequence for definition",
		node: &ast.Field{Label: ident("foo"), Value: &ast.StructLit{
			Elts: []ast.Decl{&ast.Field{
				Label: ident("bar"),
				Token: token.ISA,
				Value: &ast.StructLit{},
			}},
		}},
		// Force a new struct.
		out: `
foo: bar :: {
}`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			b, err := Node(tc.node)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			want := tc.out
			if got != want {
				t.Errorf("\ngot  %v;\nwant %v", got, want)
			}
		})
	}
}
