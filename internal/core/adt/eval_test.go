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
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

func TestEval(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "../../../cue/testdata",
		Name:   "eval",
		Update: cuetest.UpdateGoldenFiles,
		Skip:   alwaysSkip,
		ToDo:   needFix,
	}

	if *todo {
		test.ToDo = nil
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, err := r.Build(nil, a[0])
		if err != nil {
			t.WriteErrors(err)
			return
		}

		e := eval.New(r)
		ctx := e.NewContext(v)
		v.Finalize(ctx)

		stats := ctx.Stats()
		t.Log(stats)
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
	in := `
-- cue.mod/module.cue --
module: "mod.test"

-- in.cue --
circularIf: {
	#list: {
		tail: #list | *null
		if tail != null {
		}
	}
}

// Issue #754
Template: {
	from: _

	task: from
}

C: Template & {
	from: Template & {
		// from: Template & {}
	}
}

// simple2: {
// 	y: [string]: b: c: y
// 	x: y
// 	x: c: y
// }

// x: y
// y: [y & {}| bool]

// z: {
// 	a: #A
// 	b: #A
// }
// #A: string | [#A]

// p5: {
// 	#T: {
// 		a: [...{link: #T}]
// 		a: [{}]
// 	}

// 	a: #T & {
// 		a: [{link: a: [{}]}]
// 	}
// }


// d1: {
// 	a: b: c: d: {h: int, t: r}
// 	r: a.b

// }


// b11: {
// 	#list: {
// 		tail: #list | *null
// 		if tail != null {
// 		}
// 	}
// }

// b8: {
// 	x: a
// 	a: f: b
// 	b: a | string
// }

// b10: {
// 	a: close({
// 		b: string | a | c
// 	})
// 	c: close({
// 		d: string | a
// 	})
// }




// shortPathFail: comprehension: {
// 	#list: {
//         tail: #list | *null
//         if tail != null {
//         }
//     }
// }

// withLet: {
// 	schema: next:  _schema_1
// 	let _schema_1 = schema
// }

// Template: {
// 	from: _
// 	task: from
// }

// C: Template & {
// 	from: Template
// }


// a: ["3"] + b
// b: a
// a: ["1", "2"]

// a: ["3"] + b
// b: a
// b: ["1", "2"]

// b: a
// a: ["1", "2"]
// a: ["3"] + b



// Secret: $secret: id: string
// #secrets: Secret | {[string]: #secrets}
// #secrets: Secret | {[string]: #secrets}
// out: #secrets & {
// 	FOO: $secret: id: "100"
// 	ONE: TWO: THREE: $secret: id: "123"
// }


//   #S: a: a: a: #S
//   a: a: a: #S
//   a: a: #S
//   a: a: a: a: a: a: _
//   // Should be:
//   a: a: a: a: a: a: a: a: a: _


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

	adt.Verbosity = 1
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
