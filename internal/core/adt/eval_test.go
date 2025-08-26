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
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

// TestEvalV2 tests the old implementation of the evaluator.
// Note that [TestEvalV3] with CUE_UPDATE=1 assumes it runs after this test
// for the sake of comparing results between the two evaluator versions.
// As such, these two tests are not parallel at the top level.
//
// Note that this also means that CUE_UPDATE=1 is broken under `go test -shuffle`.
func TestEvalV2(t *testing.T) {
	t.Skip("TODO: this will become the main test after EvalV3 promotes")
	test := cuetxtar.TxTarTest{
		Root: "../../../cue/testdata",
		Name: "eval",
	}
	cuedebug.Init()
	dbg := cuedebug.Flags
	cueexperiment.Init()
	exp := cueexperiment.Flags
	if *todo {
		test.ToDo = nil
	}
	test.Run(t, func(t *cuetxtar.Test) {
		t.Parallel()
		runEvalTest(t, internal.EvalV3, dbg, exp)
	})
}

func TestEvalV3(t *testing.T) {
	adt.DebugDeps = true // check unmatched dependencies.

	test := cuetxtar.TxTarTest{
		Root:     "../../../cue/testdata",
		Name:     "evalalpha",
		Fallback: "eval", // Allow eval golden files to pass these tests.
	}

	cuedebug.Init()
	dbg := cuedebug.Flags
	cueexperiment.Init()
	exp := cueexperiment.Flags

	if *todo {
		test.ToDo = nil
	}

	test.Run(t, func(t *cuetxtar.Test) {
		t.Parallel()
		runEvalTest(t, internal.EvalV3, dbg, exp)
	})
}

func runEvalTest(t *cuetxtar.Test, version internal.EvaluatorVersion, dbg cuedebug.Config, exp cueexperiment.Config) (errorCount int64) {
	exp.KeepValidators = !t.HasTag("simplifyValidators")

	a := t.Instance()
	r := runtime.NewWithSettings(version, dbg)
	r.SetGlobalExperiments(&exp)

	v, err := r.Build(nil, a)
	if err != nil {
		t.WriteErrors(err)
		return
	}

	e := eval.New(r)
	ctx := e.NewContext(v)
	v.Finalize(ctx)

	switch counts := ctx.Stats(); {
	case version == internal.DevVersion:
		hasDiff := false
		for _, f := range t.Archive.Files {
			if f.Name == "out/evalalpha/stats" {
				hasDiff = true
			}
		}
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
			case hasDiff || cuetest.ForceUpdateGoldenFiles:
				// With CUE_UPDATE=force, we update the stats file
				// unconditionally.
				// NOTE: if the reuse of force clashes too much with other uses,
				// we could also introduce a different enum value for this.
				fallthrough
			case orig.Disjuncts < counts.Disjuncts,
				orig.Disjuncts > counts.Disjuncts*5 &&
					counts.Disjuncts > 20,
				orig.Conjuncts > counts.Conjuncts*2,
				counts.Notifications > 10,
				counts.NumCloseIDs > 100,
				counts.MaxReqSets > 15,
				counts.Leaks()-orig.Leaks() > 17,
				counts.Allocs-orig.Allocs > 50:
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

	if b := adt.Validate(ctx, v, &adt.ValidateConfig{
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
	return
}

func TestIssue3985(t *testing.T) {
	// We run the evaluator twice with different versions of the evaluator. Each
	// results in the use of emptyNode. Ensure that the it does not get
	// assigned a nodeContext.
	cuecontext.New(cuecontext.EvaluatorVersion(cuecontext.EvalV3)).CompileString(`a!: _, b: [for c in a if a != _|_ {}]`)

}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	adt.DebugDeps = true
	// adt.OpenGraphs = true

	flags := cuedebug.Config{
		Sharing: true, // Uncomment to turn sharing off.
		LogEval: 1,    // Uncomment to turn logging off
	}
	cueexperiment.Init()
	exps := cueexperiment.Flags

	version := internal.EvalV3

	in := `
-- cue.mod/module.cue --
module: "mod.test"

language: version: "v0.15.0"

-- in.cue --
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
	r.SetGlobalExperiments(&exps)

	v, err := r.Build(nil, instance)
	if err != nil {
		t.Fatal(err)
	}

	e := eval.New(r)
	ctx := e.NewContext(v)
	ctx.Config = flags
	v.Finalize(ctx)

	out := debug.NodeString(r, v, nil)
	if adt.OpenGraphs {
		for p, g := range ctx.ErrorGraphs {
			path := filepath.Join(".debug/TestX", p)
			adt.OpenNodeGraph("TestX", path, in, out, g)
		}
	}

	if b := adt.Validate(ctx, v, &adt.ValidateConfig{
		AllErrors: true,
	}); b != nil {
		t.Log(errors.Details(b.Err, nil))
	}

	t.Error(out)

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
	ctx := cuecontext.New()
	c := `a: {}, a`
	v1 := ctx.CompileString(c)
	v2 := ctx.CompileString(c)

	v1.Unify(v2)
}
