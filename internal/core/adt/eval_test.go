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
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	goruntime "runtime"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/validate"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

// TestEval tests the default implementation of the evaluator.
func TestEval(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "../../../cue/testdata",
		Name: "eval",
		Skip: alwaysSkip,
		ToDo: needFix,
	}

	if *todo {
		test.ToDo = nil
	}

	test.Run(t, func(tc *cuetxtar.Test) {
		runEvalTest(tc, internal.DefaultVersion, cuedebug.Config{})
	})
}

var alwaysSkip = map[string]string{
	"compile/erralias": "compile error",
}

var needFix = map[string]string{
	"DIR/NAME":       "reason",
	"cycle/patterns": "cycle detection in v2",
}

// skipDebugDepErrors is a temporary hack to skip tests that are known to have
// counter errors.
// TODO: These counters should all go to zero.
var skipDebugDepErrors = map[string]int{}

func TestEvalAlpha(t *testing.T) {
	// TODO: remove use of externalDeps for processing. Currently, enabling
	// this would fix some issues, but also introduce some closedness bugs.
	// As a first step, we should ensure that the temporary hack of using
	// externalDeps to agitate pending dependencies is replaced with a
	// dedicated mechanism.
	//
	adt.DebugDeps = true // check unmatched dependencies.

	flags := cuedebug.Config{
		Sharing: true,
	}

	var todoAlpha = map[string]string{}

	test := cuetxtar.TxTarTest{
		Root:     "../../../cue/testdata",
		Name:     "evalalpha",
		Fallback: "eval", // Allow eval golden files to pass these tests.
		Skip:     alwaysSkip,
		ToDo:     todoAlpha,
	}

	if *todo {
		test.ToDo = nil
	}

	var ran, skipped, errorCount int

	test.Run(t, func(t *cuetxtar.Test) {
		if reason := skipFiles(t.Instance().Files...); reason != "" {
			skipped++
			t.Skip(reason)
		}
		ran++

		errorCount += runEvalTest(t, internal.DevVersion, flags)
	})

	t.Logf("todo: %d, ran: %d, skipped: %d, nodeErrors: %d",
		len(todoAlpha), ran, skipped, errorCount)
}

// skipFiles returns true if the given files contain CUE that is not yet handled
// by the development version of the evaluator.
func skipFiles(a ...*ast.File) (reason string) {
	// Skip disjunctions.
	fn := func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op == token.OR {
				// Uncomment to disable disjunction testing.
				// NOTE: keep around until implementation of disjunctions
				// is complete.
				// reason = "disjunctions"
			}
		}
		return true
	}
	for _, f := range a {
		ast.Walk(f, fn, nil)
	}
	return reason
}

func runEvalTest(t *cuetxtar.Test, version internal.EvaluatorVersion, flags cuedebug.Config) (errorCount int) {
	flags.OpenInline = t.Bool("openInline")

	a := t.Instance()
	r := runtime.NewWithSettings(version, flags)

	v, err := r.Build(nil, a)
	if err != nil {
		t.WriteErrors(err)
		return
	}

	e := eval.New(r)
	ctx := e.NewContext(v)
	ctx.Version = version
	ctx.Config = flags
	v.Finalize(ctx)

	// Print discrepancies in dependencies.
	m := ctx.ErrorGraphs
	name := t.T.Name()[len("TestEvalAlpha/"):]
	expectErrs := skipDebugDepErrors[name]

	if adt.DebugDeps && len(m) != expectErrs {
		if expectErrs == 0 {
			t.Errorf("unexpected node errors: %d", len(m))
		} else {
			t.Errorf("unexpected number of node errors: got %d; expected %d",
				len(m), expectErrs)
		}

		errorCount += 1 // Could use len(m), but this seems more useful.
		i := 0
		keys := make([]string, len(m))
		for k := range m {
			keys[i] = k
			i++
		}
		slices.Sort(keys)
		for _, s := range keys {
			t.Errorf("  -- path: %s", s)
		}
	}

	switch counts := ctx.Stats(); {
	case version == internal.DevVersion:
		for _, f := range t.Archive.Files {
			if f.Name != "out/eval/stats" {
				continue
			}
			c := cuecontext.New()
			v := c.CompileBytes(f.Data)
			var orig stats.Counts
			v.Decode(&orig)

			// TODO: do something more principled.
			switch {
			case orig.Disjuncts < counts.Disjuncts,
				orig.Disjuncts > counts.Disjuncts*5 &&
					counts.Disjuncts > 20,
				orig.Conjuncts > counts.Conjuncts*2:
				// For now, we only care about disjuncts.
				// TODO: add triggers once the disjunction issues have bene
				// solved.
				w := t.Writer("stats")
				fmt.Fprintln(w, counts)
			}
			break
		}

	default:
		w := t.Writer("stats")
		fmt.Fprintln(w, counts)
	}

	// if n := stats.Leaks(); n > 0 {
	// 	t.Skipf("%d leaks reported", n)
	// }

	if b := validate.Validate(ctx, v, &validate.Config{
		AllErrors: true,
	}); b != nil {
		fmt.Fprintln(t, "Errors:")
		t.WriteErrors(b.Err)
		fmt.Fprintln(t, "")
		fmt.Fprintln(t, "Result:")
	}

	if v == nil {
		return
	}

	t.Write(debug.AppendNode(nil, r, v, &debug.Config{Cwd: t.Dir}))
	fmt.Fprintln(t)

	return
}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	adt.DebugDeps = true
	adt.OpenGraphs = true

	flags := cuedebug.Config{
		Sharing: true, // Uncomment to turn sharing off.
		// OpenInline: true,
		LogEval: 1, // Uncomment to turn logging off
	}

	version := internal.DefaultVersion
	version = internal.DevVersion // comment to use default implementation.

	in := `
-- cue.mod/module.cue --
module: "mod.test"
language: version: "v0.9.0"
-- in.cue --
Y: matchN(1, [X])
X: b?: Y
a: X
a: b: 1

// // foo: {
// y: 1
// #X: {}
// #X
// // }


// mixed: ok3: {
// 	#Schema: {
// 		exports?: matchN(1, [
// 			close({ never?: _ }), // fail
// 			#exportsObject,       // pass
// 		])

// 		#exports: matchN(1, [string, #exportsObject])
// 		#exportsObject: exp1?: #exports
// 	}
// 	out: #Schema & {
// 		exports: exp1: "str"
// 	}
// }


// #D1: {
// 	env: a: "A"
// 	env: b: "B"
// 	#def: {a: "A"}
// 	#def: {b: "B"}
// }

// d1: #D1 & {env: c: "C"}

// #A: {
// 	...
// 	{a: int}
// }
// x: #A & { c: int }

// import "list"
// issue2052: full: {
// 	#RecurseN: {
// 		#maxiter: uint | *2
// 		#funcFactory: {
// 			#next: _
// 			#func: _
// 		}
// 		for k, v in list.Range(0, #maxiter, 1) {
// 			#funcs: "a\(k)": (#funcFactory & {#next: #funcs["a\(k+1)"]}).#func
// 		}

// 		#funcs: "\(#maxiter)": null
// 		#funcs["a0"]
// 	}

// 	#DepthF: {
// 		#next: _
// 		#func: {
// 			#in:    _
// 			#basic: int | null
// 			out: {
// 				if (#in & #basic) != _|_ {1}
// 			}
// 		}
// 	}
// 	#Depth: #RecurseN & {#maxiter: 1, #funcFactory: #DepthF}
// }

// a: {
// 	#A: depends_on: [...#AnyA]
// 	#AnyA: {
// 		depends_on: [...#AnyA]
// 		...
// 	}
// 	#A1: {
// 		#A
// 		x: int
// 	}
// 	#A2: { #A }
// 	s: [Name=string]: #AnyA & {}
// 	s: foo: #A1
// 	s: bar: #A2 & {
// 		depends_on: [s.foo]
// 	}
// }

// ellipsis: ok: {
// 	out: #Schema & {
// 		field: shouldBeAllowed: 123
// 	}
// 	#Schema: {
// 		field?: #anything
// 		#anything: matchN(1, [{ ... }])
// 	}
// }
// #x2: {a: int}
// y2: #x2
// y2: {}
// y3: y2 & {a: 3}


// b: c: int
// #D: a: b

// a: #D
// a: a: d: 3

// test1: {
//     #x: matchN(1, [
//         [{}],
//     ])

//     x: #x
//     x: [{a: 1}]
// }

// test2: {
//     #x: matchN(1, [
//         {},
//     ])
//     x: #x
//     x: {a: 1}
// }

// #Workflow: {{
//     perms: matchN(1, ["all", close({"foo": "bar"})])
// }}
// out: #Workflow & {
//     perms: files: "read"
// }

// should pass
// #Schema: {
//     exports?: matchN(1, [close({
//         never?: _
//     }), #exportsObject])

//     #exports: matchN(1, [string, #exportsObject])
//     #exportsObject: {
//         exp1?: #exports
//     }
// }

// out: #Schema & {
//     exports: {
//         exp1: "./main-module.js"
//     }
// }


// #Schema: {
//     exports?: matchN(1, [null, #exportsObject])
//     #exports: matchN(1, [null, #exportsObject])
//     #exportsObject: {
//         exp1?: #exports
//     }
// }
// out: #Schema & {
//     exports: {
//         exp1: "./main-module.js"
//     }
// }


// disableEmbed: err1: {
// 	#Schema: {{
// 		a: matchN(1, ["all", {foo: "bar"}])
// 	}}
// 	out: #Schema & {
// 		a: baz: "notAllowed"
// 	}
// }
// disableEmbed: errWithClose: {
// 	#Schema: {{
// 		a: matchN(1, ["all", close({foo: "bar"})])
// 	}}
// 	out: #Schema & {
// 		a: baz: "notAllowed"
// 	}
// }
// disableEmbed: ok1: {
// 	#Schema: {{
//         a?: matchN(1, [
// 				null, // fail
// 				{ [string]: string }, // pass
// 				{b: {...}}, // fail
// 			])
// 	}}
// 	out: #Schema & {
// 		a: allowed: "once"
// 	}
// }
// disableEmbed: okWithClose: {
// 	#Schema: {{
//         a?: matchN(1, [
// 				null, // fail
// 				close({ [string]: string }), // pass
// 				close({b: {...}}), // fail
// 			])
// 	}}
// 	out: #Schema & {
// 		a: allowed: "once"
// 	}
// }

	// #a: [>="k"]: p: int
	// #b: [<="m"]: p: int
	// #c: [>="w"]: p: int
	// #d: [<="y"]: p: int
	// andOrEmbed: t2:{
	// 	#X: {
	// 		#c & #d
	// 		#a & #b
	// 	}
	// 	ok1: #X
	// 	ok1: k: {}
	// }

// patterns: shallow: {
// 	#a: [>="k"]: int
// 	#b: [<="m"]: int
// 	#c: [>="w"]: int
// 	#d: [<="y"]: int

// 	andEmbed: p1: {
// 		#X: { #a & #b } // "k" <= x && x <= "m"
// 		err: #X
// 		err: j: 3
// 	}
// }

// // issue370
// #C1: name: string
// #C2: {
// 	#C1
// 	age: int
// }
// c1: #C1 & { name: "cueckoo" }
// c2: #C2 & {
// 	c1
// 	age: 5
// }

// // 039_augment
// #A: { [=~"^[a-s]*$"]: int }
// #B: { [=~"^[m-z]*$"]: int }
// #C: {
// 	#A & #B
// 	[=~"^Q*$"]: int
// }
// c: #C & {EQQ: 3}
// c: #C & {QQ: 3}


// issue3580: {
// 	x: close({
// 		a: _
// 		b: x.a
// 	})
// }

	// #Context1: {}
	// Context2: {}
	// #Config1: cfg: #Context1
	// #Config3: cfg: #Context1
	// Config2: cfg: Context2
	// #CConfig: #Config1 & Config2
	// out: #Config3
	// out: #CConfig

// #A: {f1: int, f2: int}
// for k, v in {f3: int} {
// 	a: #A & {"\(k)": v}
// }
	// a: #X
	// a: id:  "foo"
	// #X: { #Y }
	// #Y: #Z | {}
	// #Z: id: string

// #T: {
// 	if true {
// 		// We'd like to restrict the possible members of x in this case,
// 		// but this doesn't work.
// 		x: close({
// 			f1: int
// 		})
// 	}
// 	x: _
// }
// z: #T & {
// 	x: {
// 		f1: 99
// 		f2: "i want to disallow this"
// 	}
// }

// outerErr: {
// 	_inToOut: {
// 		in: _
// 		out: in.foo
// 	}
// 	// Test that the same principle works with the close builtin.
// 	usingClose: {
// 		// Same as above, but with an additional level of nesting.
// 		#Inner:  foo: close({minor: 2})
// 		#Outer: version: { major: 1, ... }

// 		t1: #Outer
// 		t1: version: (_inToOut & {in: #Inner}).out
// 	}
// }

	// #Context1: ctx: {}
	// Context2: ctx: {}

	// // Must both refer to #Context1
	// #Config1: cfg: #Context1
	// #Config3: cfg: #Context1

	// Config2: cfg: Context2

	// Config: #Config1 & Config2

	// // order matters
	// out: Config // Indirection necessary.
	// out: #Config3

// let F = { // must be a let.
//     // moving this one level up fixes it.
//     base: {
//         in: string
//         let X = [ {msg: "\(in)"} ][0]
//         out: X.msg
//     }
//     XXX: base & {in: "foo"}
// }
// output: F.XXX.out // confirm that it does not work outside let.



// out: #Schema & {
//     field: shouldBeAllowed: 123
// }
// #Schema: {
//     field?: #anything
//     #anything: matchN(1, [{ ... }])
// }


	// #Common: Name: string
	// #A: {
	// 	#Common
	// 	Something: int
	// }
	// #B: {
	// 	#Common
	// 	Else: int
	// }
	// x: #B
	// x: #A & {
	// 	Name:      "a"
	// 	Something: 4
	// }

// #ImageTag: {
//     version?: string
//     output: version
// }
// #ImageTags: {
//     versions: [string]: string
//     cfg: (#ImageTag & {
//         version: versions["may-exist-later"]
//     }).output
// }

	// ok2: {
	// 	out: #Workflow & {
	// 		_b: #step & {
	// 			run: "foo bar"
	// 		}
	// 	}
	// 	#Workflow: {}
	// 	#step: matchN(1, [{ run!: _ }])
	// }
// issue3694: simple: {
// 	#step: matchN(1, [{
// 		uses!: _
// 	}])
// 	#step: close({
// 		uses?: string
// 	})
// }


// definitions/embed
// reclose3: {
//     #Common: Name: string
// 	#A: {#Common}
// 	#Step: {#Common}
// 	x: #A & #Step
// 	x: Name: "a"
// }

// // cycle/inline
// issue3731: full: {
// 	#Workspace: {
// 		workspaceA?: {}
// 		workspaceB?: {}
// 	}
// 	#AccountConfig: {
// 		workspaces: #Workspace
// 		siblings?: [...string]
// 	}
// 	#AccountConfigSub1: {
// 		#AccountConfig
// 		workspaces: "workspaceA": {}
// 	}
// 	#AccountConfigSub2: {
// 		#AccountConfig
// 		workspaces: "workspaceB": {}
// 	}
// 	tree: env1: {
// 		"region1": {
// 			"env1-r1-account-sub1": #AccountConfigSub1
// 			"env1-r1-account-sub2-1": #AccountConfigSub2
// 		}
// 	}
// 	#lookupSiblings: {
// 		envtree: {...}
// 		out: [
// 			for region, v in envtree
// 			for account, config in v
// 			if config.workspaces."workspaceB" != _|_ { account },
// 		]
// 	}
// 	tree: ENVTREE=env1: [_]: [_]: #AccountConfig & {
// 		siblings: (#lookupSiblings & {envtree: ENVTREE}).out
// 	}
// }


// eval/sharing
// issue3641: simplified: t1: {
// 	#Context1: ctx: {}
// 	Context2: ctx: {}
// 	// Must both refer to #Context1
// 	#Config1: cfg: #Context1
// 	#Config3: cfg: #Context1
// 	Config2: cfg: Context2
// 	Config: #Config1 & Config2
// 	// order matters
// 	out: Config // Indirection necessary.
// 	out: #Config3
// }
// issue3641: simplified: t2: {
// 	#Context1: ctx: {}
// 	Context2: ctx: {}
// 	// Must both refer to #Context1
// 	#Config1: cfg: #Context1
// 	#Config3: cfg: #Context1
// 	Config2: cfg: Context2
// 	Config: #Config1 & Config2
// 	// order matters
// 	out: Config // Indirection necessary.
// 	out: #Config3
// }
// // Variant where sharing is explicitly disabled.
// issue3641: simplified: t3: {
// 	#Context1: ctx: {}
// 	Context2: ctx: {}
// 	// Must both refer to #Context1
// 	#Config1: cfg: #Context1
// 	#Config3: cfg: #Context1
// 	Config2: cfg: Context2
// 	Config: #Config1 & Config2
// 	// order matters
// 	out: __no_sharing
// 	out: Config // Indirection necessary.
// 	out: #Config3
// }

// #T: [_]: _
// #T: close({"a": string})
// x:  #T
// x: b: "foo"

// a: { #A }
// a: c: 1
// #A: b: 1


// // Should fail: embed should count
// #A: {f1: int, f2: int}
// for k, v in {f3: int} {
// 	a: #A & {"\(k)": v}
// }


// recloseSimple: {
// 	#foo: {}
// 	a: {#foo} & {b: int}
// }

// #k1: {a: int, b?: int} & #A// & close({a: int})
// #A: {a: int}


// items: #JSONSchemaProps
// #JSONSchemaProps: {
//     props?: [string]: #JSONSchemaProps

//     repeat0?: [...#JSONSchemaProps]
//     repeat1?: [...#JSONSchemaProps]
//     repeat2?: [...#JSONSchemaProps]
// }
// items: {
//     props: a1: props: a2: props: a3: props: a4: props: a5: {}
//     props: b1: props: b2: props: b3: props: b4: props: b5: {}
//     props: c1: props: c2: props: c3: props: c4: props: c5: {}
// }

	`

	if strings.HasSuffix(strings.TrimSpace(in), ".cue --") {
		t.Skip()
	}

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	r := runtime.NewWithSettings(version, flags)

	adt.TestInFinalize = true
	v, err := r.Build(nil, instance)
	if err != nil {
		t.Fatal(err)
	}

	e := eval.New(r)
	ctx := e.NewContext(v)
	ctx.Config = flags

	var memStats goruntime.MemStats
	goruntime.ReadMemStats(&memStats)
	allocBytes := memStats.Alloc
	allocObjects := memStats.Mallocs

	v.Finalize(ctx)

	goruntime.ReadMemStats(&memStats)
	allocBytes = memStats.Alloc - allocBytes
	allocObjects = memStats.Mallocs - allocObjects

	out := debug.NodeString(r, v, nil)
	if adt.OpenGraphs {
		for p, g := range ctx.ErrorGraphs {
			path := filepath.Join(".debug/TestX", p)
			adt.OpenNodeGraph("TestX", path, in, out, g)
		}
	}

	if b := validate.Validate(ctx, v, &validate.Config{
		AllErrors: true,
	}); b != nil {
		t.Log(errors.Details(b.Err, nil))
	}

	t.Error(out)

	var stats struct {
		// CUE groups stats obtained from the CUE evaluator.
		CUE stats.Counts

		// Go groups stats obtained from the Go runtime.
		Go struct {
			AllocBytes   uint64
			AllocObjects uint64
		}
	}

	stats.CUE = *ctx.Stats()
	stats.Go.AllocBytes = allocBytes
	stats.Go.AllocObjects = allocObjects
	t.Log(stats)
}

func BenchmarkUnifyAPI(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx := cuecontext.New()
		v := ctx.CompileString("")
		for j := 0; j < 500; j++ {
			if j == 400 {
				b.StartTimer()
			}
			v = v.FillPath(cue.ParsePath(fmt.Sprintf("i_%d", i)), i)
		}
	}
}

func TestIssue2293(t *testing.T) {
	ctx := cuecontext.New()
	c := `a: {}, a`
	v1 := ctx.CompileString(c)
	v2 := ctx.CompileString(c)

	v1.Unify(v2)
}
