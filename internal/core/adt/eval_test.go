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

	"github.com/rogpeppe/go-internal/txtar"

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
module: "example.com"

-- in.cue --
	`

	if strings.HasSuffix(strings.TrimSpace(in), ".cue --") {
		t.Skip()
	}

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, "/tmp/test")[0]
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
