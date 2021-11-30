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

package adt_test

import (
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
)

// TestClosedness is a bootstrap and debugging test for developing the
// closedness algorithm. Most details of closedness is tested in the standard
// test suite.
func TestClosedness(t *testing.T) {
	r := runtime.New()
	ctx := eval.NewContext(r, nil)

	mkStruct := func(info adt.CloseInfo, s string) *adt.StructInfo {
		x, err := parser.ParseExpr("", s)
		if err != nil {
			t.Fatal(err)
		}
		expr, err := compile.Expr(nil, r, "", x)
		if err != nil {
			t.Fatal(err)
		}
		st := expr.Elem().(*adt.StructLit)
		st.Init()

		return &adt.StructInfo{
			StructLit: st,
			CloseInfo: info,
		}
	}

	mkRef := func(s string) adt.Expr {
		f := adt.MakeIdentLabel(r, s, "")
		return &adt.FieldReference{Label: f}
	}

	type test struct {
		f     string
		found bool
	}

	testCases := []struct {
		desc     string
		n        func() *adt.Vertex
		tests    []test
		required bool
	}{{
		desc: "simple embedding",
		// test: {
		//     a: 1
		//     c: 1
		//
		//     def
		// }
		// def: {
		//     c: 1
		//     d: 1
		// }
		n: func() *adt.Vertex {
			var (
				root  adt.CloseInfo
				embed = root.SpawnEmbed(mkRef("dummy"))
				def   = embed.SpawnRef(nil, false, mkRef("def"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(root, "{a: 1, c: 1}"),
					mkStruct(def, "{c: 1, d: 1}"),
				},
			}
		},
		tests: []test{
			{"a", true},
			{"c", true},
			{"d", true},
			{"e", false}, // allowed, but not found
		},
		required: false,
	}, {
		desc: "closing embedding",
		// test: {
		//     a: 1
		//     c: 1
		//
		//     #def
		// }
		// #def: {
		//     c: 1
		//     d: 1
		// }
		n: func() *adt.Vertex {
			var (
				root  adt.CloseInfo
				embed = root.SpawnEmbed(mkRef("dummy"))
				def   = embed.SpawnRef(nil, true, mkRef("#def"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(root, "{a: 1, c: 1}"),
					mkStruct(def, "{c: 1, d: 1}"),
				},
			}
		},
		tests: []test{
			{"a", true},
			{"c", true},
			{"d", true},
			{"e", false},
		},
		required: true,
	}, {
		desc: "narrow down definitions in subfields",
		// test: #foo
		// test: {
		//     a: 1
		//     b: 1
		// }
		// #foo: {
		//     c: #bar
		//     c: #baz
		//     c: d: 1
		//     c: e: 1
		// }
		// #bar: {
		//     d: 1
		//     e: 1
		// }
		// #baz: {
		//     d: 1
		//     f: 1
		// }
		n: func() *adt.Vertex {
			var (
				root adt.CloseInfo
				foo  = root.SpawnRef(nil, true, mkRef("#foo"))
				bar  = foo.SpawnRef(nil, true, mkRef("#bar"))
				baz  = foo.SpawnRef(nil, true, mkRef("#baz"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(root, "{a: 1, b:1}"),
					mkStruct(foo, "{d: 1, e: 1}"),
					mkStruct(bar, "{d: 1, e: 1}"),
					mkStruct(baz, "{d: 1, f: 1}"),
				},
			}
		},
		tests: []test{
			{"a", false},
			{"b", false},
			{"d", true},
			{"e", false},
			{"f", false},
		},
		required: true,
	}, {
		desc: "chained references",
		// test: foo
		// test: {
		//     a: 1
		//     b: 1
		// }
		// foo: bar
		// bar: {
		//     #baz
		//     e: 1
		// }
		// #baz: {
		//     c: 1
		//     d: 1
		// }
		n: func() *adt.Vertex {
			var (
				root adt.CloseInfo
				foo  = root.SpawnRef(nil, false, mkRef("foo"))
				bar  = foo.SpawnRef(nil, false, mkRef("bar"))
				baz  = bar.SpawnEmbed(mkRef("#baz"))
				def  = baz.SpawnRef(nil, true, mkRef("#baz"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(bar, "{e: 1}"),
					mkStruct(def, "{c: 1, d: 1}"),
					mkStruct(root, "{a: 1, c: 1}"),
				},
			}
		},
		tests: []test{
			{"a", false},
			{"c", true},
			{"d", true},
			{"e", true},
			{"f", false},
		},
		required: true,
	}, {
		desc: "conjunction embedding",
		// test: foo
		// test: {
		//     a: 1
		//     b: 1
		// }
		// foo: {
		//     #bar & #baz
		//     f: 1
		// }
		// #bar: {
		//     c: 1
		//     d: 1
		// }
		// #baz: {
		//     d: 1
		// }
		// #baz: {
		//     e: 1
		// }
		n: func() *adt.Vertex {
			var (
				root  adt.CloseInfo
				foo   = root.SpawnRef(nil, false, mkRef("foo"))
				embed = foo.SpawnEmbed(mkRef("dummy"))
				bar   = embed.SpawnRef(nil, true, mkRef("#bar"))
				baz   = embed.SpawnRef(nil, true, mkRef("#baz"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(root, "{a: 1, c: 1}"),
					mkStruct(foo, "{f: 1}"),
					mkStruct(bar, "{c: 1, d: 1}"),
					mkStruct(baz, "{d: 1}"),
					mkStruct(baz, "{e: 1}"),
				},
			}
		},
		tests: []test{
			{"a", false},
			{"c", false},
			{"d", true},
			{"e", false},
			{"f", true},
			{"g", false},
		},
		required: true,
	}, {
		desc: "local closing",
		// test: {
		//     #def
		//     a: 1
		//     b: 1
		// }
		// test: {
		//     c: 1
		//     d: 1
		// }
		// #def: {
		//     c: 1
		//     e: 1
		// }
		n: func() *adt.Vertex {
			var (
				root adt.CloseInfo
				// isolate local struct.
				spawned = root.SpawnRef(nil, false, mkRef("dummy"))
				embed   = spawned.SpawnEmbed(mkRef("dummy"))
				def     = embed.SpawnRef(nil, true, mkRef("#def"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(spawned, "{a: 1, b: 1}"),
					mkStruct(root, "{c: 1, d: 1}"),
					mkStruct(def, "{c: 1, e: 1}"),
				},
			}
		},
		tests: []test{
			{"d", false},
			{"a", true},
			{"c", true},
			{"e", true},
			{"f", false},
		},
		required: true,
	}, {
		desc: "local closing of def",
		// #test: {
		//     #def
		//     a: 1
		//     b: 1
		// }
		// #test: {
		//     c: 1
		//     d: 1
		// }
		// #def: {
		//     c: 1
		//     e: 1
		// }
		n: func() *adt.Vertex {
			var (
				root adt.CloseInfo
				test = root.SpawnRef(nil, true, mkRef("#test"))
				// isolate local struct.
				spawned = test.SpawnRef(nil, false, mkRef("dummy"))
				embed   = spawned.SpawnEmbed(mkRef("dummy"))
				def     = embed.SpawnRef(nil, true, mkRef("#def"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(spawned, "{a: 1, b: 1}"),
					mkStruct(test, "{c: 1, d: 1}"),
					mkStruct(def, "{c: 1, e: 1}"),
				},
			}
		},
		tests: []test{
			{"a", true},
			{"d", false},
			{"c", true},
			{"e", true},
			{"f", false},
		},
		required: true,
	}, {
		desc: "branching",
		// test: #foo
		// #foo: {
		//     c: #bar1
		//     c: #bar2
		// }
		// #bar1: {
		//     d: #baz1
		//     d: #baz2
		// }
		// #bar2: {
		//     d: #baz3
		//     d: {#baz4}
		// }
		// #baz1: e: 1
		// #baz2: e: 1
		// #baz3: e: 1
		// #baz4: e: 1
		n: func() *adt.Vertex {
			var (
				root adt.CloseInfo
				foo  = root.SpawnRef(nil, true, mkRef("#foo"))
				bar1 = foo.SpawnRef(nil, true, mkRef("#bar1"))
				bar2 = foo.SpawnRef(nil, true, mkRef("#bar2"))
				baz1 = bar1.SpawnRef(nil, true, mkRef("#baz1"))
				baz2 = bar1.SpawnRef(nil, true, mkRef("#baz2"))
				baz3 = bar2.SpawnRef(nil, true, mkRef("#baz3"))
				spw3 = bar2.SpawnRef(nil, false, mkRef("spw3"))
				emb2 = spw3.SpawnEmbed(mkRef("emb"))
				baz4 = emb2.SpawnRef(nil, true, mkRef("#baz4"))
			)
			return &adt.Vertex{
				Structs: []*adt.StructInfo{
					mkStruct(root, "{}"),
					mkStruct(foo, "{}"),
					mkStruct(bar1, "{}"),
					mkStruct(bar2, "{}"),
					mkStruct(baz1, "{e: 1, f: 1, g: 1}"),
					mkStruct(baz2, "{e: 1, f: 1, g: 1}"),
					mkStruct(baz3, "{e: 1, g: 1}"),
					mkStruct(baz4, "{e: 1, f: 1}"),
				},
			}
		},
		tests: []test{
			{"a", false},
			{"e", true},
			{"f", false},
			{"g", false},
		},
		required: true,
	}}
	// TODO:
	// dt1: {
	// 	#Test: {
	// 		#SSH:   !~"^ssh://"
	// 		source: #SSH | #Test
	// 	}

	// 	foo: #Test & {
	// 		source: "http://blablabla"
	// 	}

	// 	bar: #Test & {
	// 		source: foo
	// 	}
	// }
	//
	// -----
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			n := tc.n()
			for _, sub := range tc.tests {
				t.Run(sub.f, func(t *testing.T) {
					f := adt.MakeIdentLabel(r, sub.f, "")

					ok, required := adt.Accept(ctx, n, f)
					if ok != sub.found || required != tc.required {
						t.Errorf("got (%v, %v); want (%v, %v)",
							ok, required, sub.found, tc.required)
					}
				})
			}
		})
	}
}
