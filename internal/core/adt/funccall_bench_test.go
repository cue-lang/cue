// Copyright 2026 CUE Authors
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
	"strings"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
)

// funcCallBench describes a workload for [BenchmarkFuncCall]. Its source
// evaluates elems list elements, each of which makes calls native function
// calls.
type funcCallBench struct {
	name  string
	calls int // native function calls per element
	decls string
	elem  string // element expression; i is the element index
}

// funcCallChain returns the declarations of a non-recursive chain of depth
// functions f1..fdepth, where each calls the previous one.
func funcCallChain(depth int) string {
	var b strings.Builder
	b.WriteString("f1: func(n: int) -> int: n + 1\n")
	for i := 2; i <= depth; i++ {
		fmt.Fprintf(&b, "f%d: func(n: int) -> int: f%d(n) + 1\n", i, i-1)
	}
	return b.String()
}

// funcCallCompose returns the declarations of a composition c of depth
// closures of a single function literal, all built at the top level.
func funcCallCompose(depth int) string {
	e := "inc"
	for range depth {
		e = "comp(" + e + ", inc)"
	}
	return "comp: func(f: _, g: _): func(x: _): f(g(x))\n" +
		"inc: func(n: int) -> int: n + 1\n" +
		"c: " + e + "\n"
}

// funcCallComposeNested is like [funcCallCompose], but builds each closure
// of the composition from within the body of another call, depth calls deep.
func funcCallComposeNested(depth int) string {
	var b strings.Builder
	b.WriteString("comp: func(f: _, g: _): func(x: _): f(g(x))\n")
	b.WriteString("inc: func(n: int) -> int: n + 1\n")
	b.WriteString("b1: func(f: _): comp(f, inc)\n")
	for i := 2; i <= depth; i++ {
		fmt.Fprintf(&b, "b%d: func(f: _): comp(b%d(f), inc)\n", i, i-1)
	}
	fmt.Fprintf(&b, "c: b%d(inc)\n", depth)
	return b.String()
}

var funcCallBenches = []funcCallBench{{
	// The cost of the surrounding comprehension, without any call.
	name: "nocall",
	elem: "i + i",
}, {
	// A function defined at the top level.
	name:  "static",
	calls: 1,
	decls: "sum: func(a: int, b: int) -> int: a + b",
	elem:  "sum(i, i)",
}, {
	// A closure created by a call, capturing a distinct value per element.
	name:  "closure",
	calls: 2,
	decls: "mk: func(k: int): func(v: int) -> int: v + k",
	elem:  "mk(i)(i)",
}, {
	// A closure created by a call, capturing the same value per element.
	name:  "closureEq",
	calls: 2,
	decls: "mk: func(k: int): func(v: int) -> int: v + k",
	elem:  "mk(1)(i)",
}, {
	// A closure created by a call and passed to another function.
	name:  "higherOrder",
	calls: 3,
	decls: "mk: func(k: int): func(v: int) -> int: v + k\n" +
		"apply: func(f: _, v: _): f(v)",
	elem: "apply(mk(i), i)",
}, {
	name:  "stack/depth=8",
	calls: 8,
	decls: funcCallChain(8),
	elem:  "f8(i)",
}, {
	name:  "stack/depth=64",
	calls: 64,
	decls: funcCallChain(64),
	elem:  "f64(i)",
}, {
	// Closures of one literal, nested through one call site.
	name:  "compose/depth=8",
	calls: 2*8 + 1,
	decls: funcCallCompose(8),
	elem:  "c(i)",
}, {
	name:  "compose/depth=64",
	calls: 2*64 + 1,
	decls: funcCallCompose(64),
	elem:  "c(i)",
}, {
	name:  "composeNested/depth=8",
	calls: 2*8 + 1,
	decls: funcCallComposeNested(8),
	elem:  "c(i)",
}, {
	name:  "composeNested/depth=64",
	calls: 2*64 + 1,
	decls: funcCallComposeNested(64),
	elem:  "c(i)",
}}

// BenchmarkFuncCall measures the evaluation of native CUE function calls,
// excluding parsing and compilation. Each workload runs at two sizes;
// comparing them shows whether costs are linear in the number of calls.
func BenchmarkFuncCall(b *testing.B) {
	for _, tc := range funcCallBenches {
		for _, elems := range []int{1000, 8000} {
			if tc.calls > 16 {
				elems /= 32
			}
			b.Run(fmt.Sprintf("%s/elems=%d", tc.name, elems), func(b *testing.B) {
				benchmarkFuncCall(b, tc, elems)
			})
		}
	}
}

func benchmarkFuncCall(b *testing.B, tc funcCallBench, elems int) {
	src := fmt.Sprintf(`
@experiment(functions)

import "list"

%s
out: [for i in list.Range(0, %d, 1) {%s}]
`, tc.decls, elems, tc.elem)

	r := runtime.New()
	root, inst := r.Compile(nil, src)
	if inst.Err != nil {
		b.Fatal(inst.Err)
	}
	evaluate := func() (*adt.Vertex, *adt.OpContext) {
		v := &adt.Vertex{Conjuncts: root.Conjuncts}
		ctx := eval.NewContext(r, v)
		v.Finalize(ctx)
		return v, ctx
	}

	// Verify the result before measuring.
	v, ctx := evaluate()
	if err := v.Err(ctx); err != nil {
		b.Fatal(err.Err)
	}
	n := 0
	for e := range v.Lookup(ctx.StringLabel("out")).Elems() {
		if _, ok := e.Value().(*adt.Num); !ok {
			b.Fatalf("element %d: got %v, want a number", n, e.Value())
		}
		n++
	}
	if n != elems {
		b.Fatalf("got %d elements, want %d", n, elems)
	}

	b.ReportAllocs()
	for b.Loop() {
		evaluate()
	}
}
