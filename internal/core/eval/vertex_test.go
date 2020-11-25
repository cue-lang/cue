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

package eval_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
)

// TestVertex tests the use of programmatically generated Vertex values that
// may have different properties than the one generated.
func TestVertex(t *testing.T) {
	r := cue.NewRuntime()
	ctx := eval.NewContext(r, nil)

	goValue := func(x interface{}) *adt.Vertex {
		v := convert.GoValueToValue(ctx, x, true)
		return v.(*adt.Vertex)
	}

	goType := func(x interface{}) *adt.Vertex {
		v, err := convert.GoTypeToExpr(ctx, x)
		if err != nil {
			t.Fatal(err)
		}
		n := &adt.Vertex{}
		n.AddConjunct(adt.MakeRootConjunct(nil, v))
		n.Finalize(ctx)
		return n
	}

	schema := func(field, config string) *adt.Vertex {
		v := build(t, ctx, config)
		f := adt.MakeIdentLabel(r, field, "")
		return v.Lookup(f)
	}

	testCases := []struct {
		name    string
		a, b    *adt.Vertex
		want    string
		verbose bool
	}{{

		// Issue #530
		a: schema("Steps", `
			Steps: [...#Step]

			#Step: Args: _
			`),
		b: goValue([]*struct{ Args interface{} }{{
			Args: map[string]interface{}{
				"Message": "Hello, world!",
			},
		}}),
		want: `[{Args:{Message:"Hello, world!"}}]`,
	}, {

		name: "list of values",
		a:    goValue([]int{1, 2, 3}),
		b:    goType([]int{}),
		want: `[1,2,3]`,
	}, {
		name: "list of list of values",
		a:    goValue([][]int{{1, 2, 3}}),
		b:    goType([][]int{}),
		want: `[[1,2,3]]`,
	}, {
		a: schema("Steps", `
		Steps: [...#Step]

		#Step: Args: _
		`),
		b: schema("a", `
		a: [{Args: { Message: "hello" }}]
	`),
		want: `[{Args:{Message:"hello"}}]`,
	}, {

		// Issue #530
		a: schema("Steps", `
		Steps: [...#Step]

		#Step: Args: _
		`),
		b: goValue([]*struct{ Args interface{} }{{
			Args: map[string]interface{}{
				"Message": "Hello, world!",
			},
		}}),
		want: `[{Args:{Message:"Hello, world!"}}]`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {

			n := &adt.Vertex{}
			eval.AddVertex(n, tc.a)
			eval.AddVertex(n, tc.b)
			n.Finalize(ctx)

			got := debug.NodeString(r, n, &debug.Config{Compact: !tc.verbose})
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func build(t *testing.T, ctx *adt.OpContext, schema string) *adt.Vertex {
	f, err := parser.ParseFile("test", schema)
	if err != nil {
		t.Fatal(err)
	}

	v, err := compile.Files(nil, ctx, "test", f)
	if err != nil {
		t.Fatal(err)
	}

	v.Finalize(ctx)

	return v
}
