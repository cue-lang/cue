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

package adt_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
)

// TestCloseContext tests the intricacies of the closedness algorithm.
// Much of this could be tested using the existing testing framework, but the
// code is intricate enough that it is hard to map real-life cases onto
// specific edge cases. Also, changes in the higher-level order of evaluation
// may cause certain test cases to go untested.
//
// This test enables covering such edge cases with confidence by allowing
// fine control over the order of execution of conjuncts
//
// NOTE: much of the code is in export_test.go. This test needs access to
// higher level functionality which prevents it from being defined in
// package adt. The code in export_test.go provides access to the
// low-level functionality that this test needs.
func TestCloseContext(t *testing.T) {
	r := runtime.New()
	ctx := eval.NewContext(r, nil)

	v := func(s string) adt.Value {
		v, _ := r.Compile(nil, s)
		if err := v.Err(ctx); err != nil {
			t.Fatal(err.Err)
		}
		v.Finalize(ctx)
		return v.Value()
	}
	ref := func(s string) adt.Expr {
		f := r.StrLabel(s)
		return &adt.FieldReference{Label: f}
	}
	// TODO: this may be needed once we optimize inserting scalar values.
	// pe := func(s string) adt.Expr {
	// 	x, err := parser.ParseExpr("", s)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	c, err := compile.Expr(nil, r, "test", x)
	// 	if err != nil {
	// 		t.Fatal(err)
	// 	}
	// 	return c.Expr()
	// }
	type testCase struct {
		name string
		run  func(*adt.FieldTester)

		// arcs shows a hierarchical representation of the arcs below the node.
		arcs string

		// patters shows a list of patterns and associated conjuncts
		patterns string

		// allowed is the computed allowed expression.
		allowed string

		// err holds all errors or "" if none.
		err string
	}
	cases := []testCase{{
		name: "one",
		run: func(x *adt.FieldTester) {
			x.Run(x.Field("a", "foo"))
		},
		arcs: `a: {"foo"}`,
	}, {
		name: "two",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("a", "foo"),
				x.Field("a", "bar"),
			)
		},
		arcs: `a: {"foo" "bar"}`,
	}, {
		// This could be optimized as both nestings have a single value.
		// This cause some provenance data to be lost, so this could be an
		// option instead.
		name: "double nested",
		// a: "foo"
		// #A
		//
		// where
		//
		// #A: {
		//    #B
		//    b: "foo"
		// }
		// #B: a: "foo"
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("a", "foo"),
				x.EmbedDef(
					x.EmbedDef(x.Field("a", "foo")),
					x.Field("b", "foo"),
				),
			)
		},
		arcs: `
			a: {
				"foo"
				[e]{
					[d]{
						[e]{
							[d]{"foo"}
						}
					}
				}
			}
			b: {
				[e]{
					[d]{"foo"}
				}
			}`,
	}, {
		name: "single pattern",
		run: func(x *adt.FieldTester) {
			x.Run(x.Pat(v(`<="foo"`), v("1")))
		},
		arcs:     "",
		patterns: `<="foo": {1}`,
		allowed:  `<="foo"`,
	}, {
		name: "total patterns",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Pat(v(`<="foo"`), x.NewString("foo")),
				x.Pat(v(`string`), v("1")),
				x.Pat(v(`string`), v("1")),
				x.Pat(v(`<="foo"`), v("2")),
			)
		},

		arcs: "",
		patterns: `
			<="foo": {"foo" 2}
			string: {1 1}`,

		// Should be empty or string only, as all fields match.
		allowed: "",
	}, {
		name: "multi patterns",
		run: func(x *adt.FieldTester) {
			shared := v("100")
			x.Run(
				x.Pat(v(`<="foo"`), x.NewString("foo")),
				x.Pat(v(`>"bar"`), shared),
				x.Pat(v(`>"bar"`), shared),
				x.Pat(v(`<="foo"`), v("1")),
			)
		},

		// should have only a single 100
		patterns: `
			<="foo": {"foo" 1}
			>"bar": {100}`,

		// TODO: normalize the output, Greater than first.
		allowed: `|(<="foo", >"bar")`,
	}, {
		name: "pattern defined after matching field",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("a", "foo"),
				x.Pat(v(`string`), v(`string`)),
			)
		},
		arcs:     `a: {"foo" string}`,
		patterns: `string: {string}`,
		allowed:  "", // all fields
	}, {
		name: "pattern defined before matching field",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Pat(v(`string`), v(`string`)),
				x.Field("a", "foo"),
			)
		},
		arcs:     `a: {"foo" string}`,
		patterns: `string: {string}`,
		allowed:  "", // all fields
	}, {
		name: "shared on one level",
		run: func(x *adt.FieldTester) {
			shared := ref("shared")
			x.Run(
				x.Pat(v(`string`), v(`1`)),
				x.Pat(v(`>"a"`), shared),
				x.Pat(v(`>"a"`), shared),
				x.Field("m", "foo"),
				x.Pat(v(`string`), v(`2`)),
				x.Pat(v(`>"a"`), shared),
				x.Pat(v(`>"b"`), shared),
			)
		},
		arcs: `m: {"foo" 1 shared 2}`,
		patterns: `
			string: {1 2}
			>"a": {shared}
			>"b": {shared}`,
	}, {
		// The same conjunct in different groups could result in the different
		// closedness rules. Hence they should not be shared.
		name: "do not share between groups",
		run: func(x *adt.FieldTester) {
			notShared := ref("notShared")
			x.Run(
				x.Def(x.Field("m", notShared)),

				// TODO(perf): since the nodes in Def have strictly more
				// restrictive closedness requirements, this node could be
				// eliminated from arcs.
				x.Field("m", notShared),

				// This could be shared with the first entry, but since there
				// is no mechanism to identify equality for tis case, it is
				// not shared.
				x.Def(x.Field("m", notShared)),

				// Under some conditions the same holds for embeddings.
				x.Embed(x.Field("m", notShared)),
			)
		},
		arcs: `m: {
				[d]{notShared}
				notShared
				[d]{notShared}
				[e]{notShared}
			}`,
	}, {
		name: "conjunction of patterns",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Def(x.Pat(v(`>"a"`), v("1"))),
				x.Def(x.Pat(v(`<"m"`), v("2"))),
			)
		},
		patterns: `
			>"a": {1}
			<"m": {2}`,
		// allowed reflects explicitly matched fields, even if node is not closed.
		allowed: `&(>"a", <"m")`,
	}, {
		name: "pattern in definition in embedding",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Embed(x.Def(x.Pat(v(`>"a"`), v("1")))),
			)
		},
		patterns: `>"a": {1}`,
		allowed:  `>"a"`,
	}, {
		name: "avoid duplicate pattern entries",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("b", "bar"),
				x.Field("b", "baz"),
				x.Pat(v(`string`), v("2")),
				x.Pat(v(`string`), v("3")),
				x.Field("c", "bar"),
				x.Field("c", "baz"),
			)
		},
		arcs: `
			b: {"bar" "baz" 2 3}
			c: {"bar" 2 3 "baz"}`,

		patterns: `string: {2 3}`,
	}, {
		name: "conjunction in embedding",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("b", "foo"),
				x.Embed(
					x.Def(x.Pat(v(`>"a"`), v("1"))),
					x.Def(x.Pat(v(`<"z"`), v("2"))),
				),
				x.Field("c", "bar"),
				x.Pat(v(`<"q"`), v("3")),
			)
		},
		arcs: `
			b: {
				"foo"
				[e]{
					[d]{1}
					[d]{2}
				}
				3
			}
			c: {
				"bar"
				[ed]{
					[d]{1}
					[d]{2}
				}
				3
			}`, //
		patterns: `
			>"a": {1}
			<"z": {2}
			<"q": {3}`,
		allowed: `|(<"q", &(>"a", <"z"))`,
	}, {
		// The point of this test is to see if the "allow" expression nests
		// properly.
		name: "conjunctions in embedding 1",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Def(
					x.Embed(
						x.Def(x.Pat(v(`=~"^[a-s]*$"`), v("3"))),
						x.Def(x.Pat(v(`=~"^xxx$"`), v("4"))),
					),
					x.Pat(v(`=~"^b*$"`), v("4")),
				),
				x.Field("b", v("4")),
			)
		},
		arcs: `b: {
				4
				[d]{
					[ed]{
						[d]{3}
					}
					4
				}
			}`,
		err: ``,
		patterns: `
			=~"^[a-s]*$": {3}
			=~"^xxx$": {4}
			=~"^b*$": {4}`, allowed: `|(=~"^b*$", &(=~"^[a-s]*$", =~"^xxx$"))`,
	}, {
		name: "conjunctions in embedding 2",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Embed(
					x.Field("b", v("4")),
					x.Embed(
						x.Def(x.Pat(v(`>"b"`), v("3"))),
						x.Def(x.Pat(v(`<"h"`), v("4"))),
					),
				),
			)
		},
		arcs: `b: {
				[e]{
					4
					[ed]{
						[d]{4}
					}
				}
			}`,

		err: ``,
		patterns: `
			>"b": {3}
			<"h": {4}`,
		// Allowed is defined here, because this embeds definitions with
		// patterns.
		allowed: `&(>"b", <"h")`,
	}, {
		name: "conjunctions in embedding 3",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Embed(
					x.Def(
						x.Field("b", "foo"),
						x.Embed(
							x.Def(x.Pat(v(`>"a"`), v("1"))),
							x.Def(x.Pat(v(`<"g"`), v("2"))),
							x.Pat(v(`<"z"`), v("3")),
						),
						x.Embed(
							x.Def(x.Pat(v(`>"b"`), v("3"))),
							x.Def(x.Pat(v(`<"h"`), v("4"))),
						),
						x.Field("c", "bar"),
						x.Pat(v(`<"q"`), v("5")),
					),
					x.Def(
						x.Embed(
							x.Def(x.Pat(v(`>"m"`), v("6"))),
							x.Def(x.Pat(v(`<"y"`), v("7"))),
							x.Pat(v(`<"z"`), v("8")),
						),
						x.Embed(
							x.Def(x.Pat(v(`>"n"`), v("9"))),
							x.Def(x.Pat(v(`<"z"`), v("10"))),
						),
						x.Field("c", "bar"),
						x.Pat(v(`<"q"`), v("11")),
					),
				),
				x.Pat(v(`<"h"`), v("12")),
			)
		},
		arcs: `
			b: {
				[e]{
					[d]{
						"foo"
						[e]{
							[d]{1}
							[d]{2}
							3
						}
						[ed]{
							[d]{4}
						}
						5
					}
					[d]{
						[ed]{
							[d]{7}
							8
						}
						[ed]{
							[d]{10}
						}
						11
					}
				}
				12
			}
			c: {
				[e]{
					[d]{
						"bar"
						[ed]{
							[d]{1}
							[d]{2}
							3
						}
						[ed]{
							[d]{3}
							[d]{4}
						}
						5
					}
					[d]{
						[ed]{
							[d]{7}
							8
						}
						[ed]{
							[d]{10}
						}
						"bar"
						11
					}
				}
				12
			}`,
		patterns: `
			>"a": {1}
			<"g": {2}
			<"z": {3 8 10}
			>"b": {3}
			<"h": {4 12}
			<"q": {5 11}
			>"m": {6}
			<"y": {7}
			>"n": {9}`,
		allowed: `|(<"h", &(|(<"q", &(>"b", <"h"), &(>"a", <"g")), |(<"q", &(>"n", <"z"), &(>"m", <"y"))))`,
	}, {
		name: "dedup equal",
		run: func(x *adt.FieldTester) {
			shared := v("1")
			x.Run(
				x.FieldDedup("a", shared),
				x.FieldDedup("a", shared),
			)
		},
		patterns: "",
		allowed:  "",
		arcs:     `a: {1}`,
	}, {
		// Effectively {a: 1} & #D, where #D is {b: 1}
		name: "disallowed before",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("a", v("1")),
				x.Def(x.Field("b", v("1"))),
			)
		},
		arcs: `
			a: {1}
			b: {
				[d]{1}
			}`,
		err: `a: field not allowed`,
	}, {
		// Effectively #D & {a: 1}, where #D is {b: 1}
		name: "disallowed after",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Def(x.Field("b", v("1"))),
				x.Field("a", v("1")),
			)
		},
		arcs: `
			b: {
				[d]{1}
			}
			a: {1}`,
		err: `a: field not allowed`,
	}, {
		// a: {#A}
		// a: c: 1
		// #A: b: 1
		name: "def embed",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Group(x.EmbedDef(x.Field("b", "foo"))),
				x.Field("c", "bar"),
			)
		},
		arcs: `
			b: {
				[]{
					[e]{
						[d]{"foo"}
					}
				}
			}
			c: {"bar"}`,
		err: `c: field not allowed`,
	}, {
		// a: {#A}
		// a: c: 1
		// #A: b: 1
		name: "def embed",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Group(x.EmbedDef(x.Field("b", "foo")),
					x.Field("c", "foo"),
				),
				x.Field("d", "bar"),
			)
		},
		arcs: `
			b: {
				[]{
					[e]{
						[d]{"foo"}
					}
				}
			}
			c: {
				[d]{"foo"}
			}
			d: {"bar"}`,
		err: `d: field not allowed`,
	}, {
		// This test is for debugging and can be changed.
		name: "X",
		run: func(x *adt.FieldTester) {
			x.Run(
				x.Field("b", "foo"),
				x.Embed(
					x.Def(x.Pat(v(`>"b"`), v("3"))),
					x.Def(x.Pat(v(`<"h"`), v("4"))),
				),
			)
		},
		// TODO: could probably be b: {"foo" 4}. See TODO(constraintNode).
		arcs: `b: {
				"foo"
				[ed]{
					[d]{4}
				}
			}`,
		err: ``,
		patterns: `
			>"b": {3}
			<"h": {4}`,
		allowed: `&(>"b", <"h")`,
	}}

	cuetest.Run(t, cases, func(t *cuetest.T, tc *testCase) {
		// Uncomment to debug isolated test X.
		// adt.Debug = true
		// adt.Verbosity = 2
		// t.Select("X")

		adt.DebugDeps = true
		showGraph := false
		x := adt.NewFieldTester(r)
		tc.run(x)

		ctx := x.OpContext

		switch graph, hasError := adt.CreateMermaidGraph(ctx, x.Root, true); {
		case !hasError:
		case showGraph:
			path := filepath.Join(".debug", "TestCloseContext", tc.name)
			adt.OpenNodeGraph(tc.name, path, "in", "out", graph)
			fallthrough
		default:
			t.Errorf("imbalanced counters")
		}

		t.Equal(writeArcs(x, x.Root), tc.arcs)
		t.Equal(x.Error(), tc.err)

		var patterns, allowed string
		if pcs := x.Root.PatternConstraints; pcs != nil {
			patterns = writePatterns(x, pcs.Pairs)
			if pcs.Allowed != nil {
				allowed = debug.NodeString(r, pcs.Allowed, nil)
				// TODO: output is nicer, but need either parenthesis or
				// removed spaces around more tightly bound expressions.
				// allowed = pExpr(x, pcs.Allowed)
			}
		}

		t.Equal(patterns, tc.patterns)
		t.Equal(allowed, tc.allowed)
	})
}

const (
	initialIndent = 3
	indentString  = "\t"
)

func writeArcs(x adt.Runtime, v *adt.Vertex) string {
	b := &strings.Builder{}
	for _, a := range v.Arcs {
		if len(v.Arcs) > 1 {
			fmt.Fprint(b, "\n", strings.Repeat(indentString, initialIndent))
		}
		fmt.Fprintf(b, "%s: ", a.Label.RawString(x))

		// TODO(perf): optimize this so that a single-element conjunct does
		// not need a group.
		if len(a.Conjuncts) != 1 {
			panic("unexpected conjunct length")
		}
		g := a.Conjuncts[0].Elem().(*adt.ConjunctGroup)
		vertexString(x, b, *g, initialIndent)
	}
	return b.String()
}

func writePatterns(x adt.Runtime, pairs []adt.PatternConstraint) string {
	b := &strings.Builder{}
	for _, pc := range pairs {
		if len(pairs) > 1 {
			fmt.Fprint(b, "\n", strings.Repeat(indentString, initialIndent))
		}
		b.WriteString(pExpr(x, pc.Pattern))
		b.WriteString(": ")
		vertexString(x, b, pc.Constraint.Conjuncts, initialIndent)
	}
	return b.String()
}

func hasVertex(a []adt.Conjunct) bool {
	for _, c := range a {
		switch c.Elem().(type) {
		case *adt.ConjunctGroup:
			return true
		case *adt.Vertex:
			return true
		}
	}
	return false
}

func vertexString(x adt.Runtime, b *strings.Builder, a []adt.Conjunct, indent int) {
	hasVertex := hasVertex(a)

	b.WriteString("{")
	for i, c := range a {
		if g, ok := c.Elem().(*adt.ConjunctGroup); ok {
			if hasVertex {
				doIndent(b, indent+1)
			}
			b.WriteString("[")
			if c.CloseInfo.FromEmbed {
				b.WriteString("e")
			}
			if c.CloseInfo.FromDef {
				b.WriteString("d")
			}
			b.WriteString("]")
			vertexString(x, b, *g, indent+1)
		} else {
			if hasVertex {
				doIndent(b, indent+1)
			} else if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(pExpr(x, c.Expr()))
		}
	}
	if hasVertex {
		doIndent(b, indent)
	}
	b.WriteString("}")
}

func doIndent(b *strings.Builder, indent int) {
	fmt.Fprint(b, "\n", strings.Repeat(indentString, indent))
}

func pExpr(x adt.Runtime, e adt.Expr) string {
	a, _ := export.All.Expr(x, "test", e)
	b, _ := format.Node(a)
	return string(b)
}
