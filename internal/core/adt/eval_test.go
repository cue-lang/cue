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
	"sort"
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
	"DIR/NAME": "reason",
}

// skipDebugDepErrors is a temporary hack to skip tests that are known to have
// counter errors.
// TODO: These counters should all go to zero.
var skipDebugDepErrors = map[string]int{
	"benchmarks/issue1684":     16,
	"builtins/default":         1,
	"comprehensions/pushdown":  3,
	"cycle/chain":              4,
	"cycle/compbottom2":        4,
	"cycle/comprehension":      1,
	"cycle/disjunction":        4,
	"cycle/issue990":           1,
	"cycle/structural":         17,
	"disjunctions/edge":        1,
	"disjunctions/elimination": 8,
	"disjunctions/embed":       6,
	"disjunctions/errors":      2,
	"eval/conjuncts":           3,
	"eval/disjunctions":        1,
	"eval/issue2146":           4,
	"eval/issue599":            1,
	"export/031":               1,
	"fulleval/054_issue312":    1,
	"scalars/embed":            1,
}

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

	if len(m) != expectErrs {
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
		sort.Strings(keys)
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
	// adt.OpenGraphs = true

	flags := cuedebug.Config{
		Sharing: true, // Uncomment to turn sharing off.
		LogEval: 1,    // Uncomment to turn logging off
	}

	var version internal.EvaluatorVersion
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

	if b := validate.Validate(ctx, v, &validate.Config{
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
