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
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

var (
	todo = flag.Bool("todo", false, "run tests marked with #todo-compile")
)

// TestEvalV2 tests the old implementation of the evaluator.
func TestEvalV2(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "../../../cue/testdata",
		Name: "eval",
	}

	cuedebug.Init()
	flags := cuedebug.Flags

	if *todo {
		test.ToDo = nil
	}

	test.Run(t, func(tc *cuetxtar.Test) {
		runEvalTest(tc, internal.EvalV2, flags)
	})
}

func TestEvalV3(t *testing.T) {
	// TODO: remove use of externalDeps for processing. Currently, enabling
	// this would fix some issues, but also introduce some closedness bugs.
	// As a first step, we should ensure that the temporary hack of using
	// externalDeps to agitate pending dependencies is replaced with a
	// dedicated mechanism.
	//
	adt.DebugDeps = true // check unmatched dependencies.

	cuedebug.Init()
	flags := cuedebug.Flags

	test := cuetxtar.TxTarTest{
		Root:     "../../../cue/testdata",
		Name:     "evalalpha",
		Fallback: "eval", // Allow eval golden files to pass these tests.
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

		errorCount += runEvalTest(t, internal.EvalV3, flags)
	})

	t.Logf("ran: %d, skipped: %d, nodeErrors: %d",
		ran, skipped, errorCount)
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
	ctx.SimplifyValidators = t.HasTag("simplifyValidators")
	v.Finalize(ctx)

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
				orig.Conjuncts > counts.Conjuncts*2,
				counts.CloseIDElems > 1000:
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
	fmt.Fprintln(t)

	return
}

func TestIssue3985(t *testing.T) {
	// We run the evaluator twice with different versions of the evaluator. Each
	// results in the use of emptyNode. Ensure that the it does not get
	// assigned a nodeContext.
	cuecontext.New(cuecontext.EvaluatorVersion(cuecontext.EvalV3)).CompileString(`a!: _, b: [for c in a if a != _|_ {}]`)

	cuecontext.New(cuecontext.EvaluatorVersion(cuecontext.EvalV2)).CompileString(`matchN(0, [_|_]) & []`)
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

	version := internal.DefaultVersion
	version = internal.DevVersion // comment to use default implementation.

	in := `
-- cue.mod/module.cue --
module: "mod.test"

language: version: "v0.9.0"

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
