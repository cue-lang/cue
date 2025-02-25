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
var skipDebugDepErrors = map[string]int{
	"basicrewrite/018_self-reference_cycles":                      3,
	"cycle/025_cannot_resolve_references_that_would_be_ambiguous": 1,
	"cycle/051_resolved_self-reference_cycles_with_disjunction":   2,

	"cycle/052_resolved_self-reference_cycles_with_disjunction_with_defaults": 1,
	"cycle/builtins": 3,
	"cycle/issue241": 2,
	"cycle/issue429": 1,

	// Some of these counts are related to issue 3750
	"disjunctions/elimination": 19,
	"eval/issue2146":           4,
	"eval/notify":              8,

	// TODO(issue3750): commented out reflect counts that would be there if we
	// disabled the counter also for non-disjunctions.
	"builtins/closed": 6,
	// "builtins/validators":      1,
	// "comprehensions/closed":    4,
	// "comprehensions/issue1732": 8,
	// "comprehensions/issue287":  3,
	// "comprehensions/issue3762": 38,
	"comprehensions/issue843": 1, // 2,
	// "comprehensions/nested2":  38,
	// "comprehensions/pushdown": 46,
	// "cycle/023_reentrance":    1,
	// "cycle/chain":             2,
	// "cycle/compbottom2":   44,
	// "cycle/comprehension": 10,
	// "cycle/freeze":     27,
	// "cycle/issue990":   7,
	"cycle/structural": 4, // 7,
	// "definitions/037_closing_with_comprehensions": 3,
	// "definitions/comprehensions": 3,
	// "disjunctions/elimination":   23, // + 17
	"disjunctions/errors": 3, // 6,
	// "disjunctions/operands": 1,
	// "eval/closedness":       5,
	// "eval/comprehensions":   13,
	"eval/disjunctions": 1,
	// "eval/embed":            1,
	// "eval/incomplete": 2,
	// "eval/issue2146":        4, + 3
	// "eval/issue2235": 43,
	"eval/counters": 6, // 10,
	// "eval/let":       4,
	// "eval/letjoin":   8,
	// "eval/merge":     14,
	// "eval/notify":    13, // + 10
	// "eval/sharing": 2,
	// "eval/v0.7": 9,
	// "fulleval/042_cross-dependent_comprehension": 1,
	// "resolve/038_incomplete_comprehensions":      4,
	"scalars/embed": 2,
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
	// adt.OpenGraphs = true

	flags := cuedebug.Config{
		Sharing:    true, // Uncomment to turn sharing off.
		OpenInline: true,
		LogEval:    1, // Uncomment to turn logging off
	}

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
