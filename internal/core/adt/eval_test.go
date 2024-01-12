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
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
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
		runEvalTest(tc, internal.DefaultVersion)
	})
}

var alwaysSkip = map[string]string{
	"compile/erralias": "compile error",
}

var needFix = map[string]string{
	"DIR/NAME": "reason",
}

func TestEvalAlpha(t *testing.T) {
	adt.DebugDeps = true // check unmatched dependencies.

	var todoAlpha = map[string]string{
		// The list package defines some disjunctions. Even those these tests
		// do not have any disjunctions in the test, they still fail because
		// they trigger the disjunction in the list package.
		// Some other tests use the 'or' builtin, which is also not yet
		// supported.
		"builtins/list/sort": "list package",
		"benchmarks/sort":    "list package",
		"fulleval/032_or_builtin_should_not_fail_on_non-concrete_empty_list": "unimplemented",
		"resolve/048_builtins":                     "unimplemented",
		"fulleval/049_alias_reuse_in_nested_scope": "list",
	}

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

		errorCount += runEvalTest(t, internal.DevVersion)
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
				reason = "disjunctions"
			}
		}
		return true
	}
	for _, f := range a {
		ast.Walk(f, fn, nil)
	}
	return reason
}

func runEvalTest(t *cuetxtar.Test, version internal.EvaluatorVersion) (errorCount int) {
	a := t.Instance()
	// TODO: use version once we implement disjunctions.
	r := runtime.NewVersioned(internal.DefaultVersion)

	v, err := r.Build(nil, a)
	if err != nil {
		t.WriteErrors(err)
		return
	}

	e := eval.New(r)
	ctx := e.NewContext(v)
	ctx.Version = version
	v.Finalize(ctx)

	// Print discrepancies in dependencies.
	if m := ctx.ErrorGraphs; len(m) > 0 {
		errorCount += 1 // Could use len(m), but this seems more useful.
		i := 0
		keys := make([]string, len(m))
		for k := range m {
			keys[i] = k
			i++
		}
		t.Errorf("unexpected node errors: %d", len(ctx.ErrorGraphs))
		sort.Strings(keys)
		for _, s := range keys {
			t.Errorf("  -- path: %s", s)
		}
	}

	if version != internal.DevVersion {
		stats := ctx.Stats()
		w := t.Writer("stats")
		fmt.Fprintln(w, stats)
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

	debug.WriteNode(t, r, v, &debug.Config{Cwd: t.Dir})
	fmt.Fprintln(t)

	return
}

// TestX is for debugging. Do not delete.
func TestX(t *testing.T) {
	var verbosity int
	verbosity = 1 // comment to turn logging off.

	adt.DebugDeps = true

	var version internal.EvaluatorVersion
	version = internal.DevVersion // comment to use default implementation.
	openGraph := true
	// openGraph = false

	in := `
-- cue.mod/module.cue --
module: "mod.test"

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

	r := runtime.NewVersioned(version)

	v, err := r.Build(nil, instance)
	if err != nil {
		t.Fatal(err)
	}

	// t.Error(debug.NodeString(r, v, nil))
	// eval.Debug = true
	adt.Verbosity = verbosity
	t.Cleanup(func() { adt.Verbosity = 0 })

	e := eval.New(r)
	ctx := e.NewContext(v)
	v.Finalize(ctx)
	adt.Verbosity = 0

	out := debug.NodeString(r, v, nil)
	if openGraph {
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
