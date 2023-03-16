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
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/validate"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

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

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.Instance()
		r := runtime.New()

		v, err := r.Build(nil, a)
		if err != nil {
			t.WriteErrors(err)
			return
		}

		e := eval.New(r)
		ctx := e.NewContext(v)
		v.Finalize(ctx)

		stats := ctx.Stats()
		w := t.Writer("stats")
		fmt.Fprintln(w, stats)
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

		debug.WriteNode(t, r, v, &debug.Config{Cwd: t.Dir})
		fmt.Fprintln(t)
	})
}

var alwaysSkip = map[string]string{
	"compile/erralias": "compile error",
}

var needFix = map[string]string{
	"DIR/NAME": "reason",
}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	verbosity := 0
	verbosity = 1 // uncomment to turn logging off.

	in := `
-- cue.mod/module.cue --
module: "mod.test"

-- in.cue --
a: {
	metrics: #Metric
	#Metric: {
		#IDSource | {}
		#TargetAverage | {}
	}

	metrics: {
		id:  "foo"
		avg: 60
	}

	#IDSource: id: string
	#TargetAverage: avg: number
}

b: {
	Foo: #Obj & {spec: foo: {}}
	Bar: #Obj & {spec: bar: {}}

	#Obj: X={
		spec: *#SpecFoo | #SpecBar

		out: #Out & {
			_Xspec: X.spec
			if _Xspec.foo != _|_ {
				minFoo: _Xspec.foo.min
			}
			if _Xspec.bar != _|_ {
				minBar: _Xspec.bar.min
			}
		}
	}

	#SpecFoo: foo: min: int | *10
	#SpecBar: bar: min: int | *20

	#Out: {
		{minFoo: int} | {minBar: int}

		*{nullFoo: null} | {nullBar: null}
	}
}

c: {
	#FormFoo: fooID: string
	#FormBar: barID: string
	#Form: { #FormFoo | #FormBar }

	data: {fooID: "123"}
	out1: #Form & data
	out2: #Form & out1
}

// m: {
// 	if: {
// 		[
// 			if [#str][0] != "" {
// 				let prefix = "baar"
// 				prefix
// 			},
// 			"foo"
// 		][0]
// 		#str: "refs/tags/v*"
// 	}
// 	if: number | string
// }

// m: {
// 	if: {
// 		[
// 			if [#str][0] != "" {
// 				let prefix = "baar"
// 				prefix
// 			},
// 			"foo"
// 		][0]
// 		#str: "refs/tags/v*"
// 	}
// 	if: number | string
// }

// merged: t2: p2: {
// 	x: { #in2: c1: string, #in2.c1 } &
// 		{ #in2: c1: "V 1",  _ }
// }

// m2: {
// 	if: { [][0] }
// 	if: number | string
// }

// _fn: {
// 	in: _
// 	out: in.f
// }
// fnExists: _fn != _|_
	`

	if strings.HasSuffix(strings.TrimSpace(in), ".cue --") {
		t.Skip()
	}

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, t.TempDir())[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	r := runtime.New()

	v, err := r.Build(nil, instance)
	if err != nil {
		t.Fatal(err)
	}

	// t.Error(debug.NodeString(r, v, nil))
	// eval.Debug = true
	adt.Verbosity = verbosity

	e := eval.New(r)
	ctx := e.NewContext(v)
	v.Finalize(ctx)
	adt.Verbosity = 0

	// b := validate.Validate(ctx, v, &validate.Config{Concrete: true})
	// t.Log(errors.Details(b.Err, nil))

	t.Error(debug.NodeString(r, v, nil))

	t.Log(ctx.Stats())
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
	adt.Verbosity = 1

	ctx := cuecontext.New()
	c := `a: {}, a`
	v1 := ctx.CompileString(c)
	v2 := ctx.CompileString(c)

	v1.Unify(v2)
}
