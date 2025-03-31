// Copyright 2021 CUE Authors
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

package cuecontext

import (
	"fmt"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cueexperiment"
)

func TestAPI(t *testing.T) {
	testCases := []struct {
		name string
		fun  func() cue.Value
		want string
	}{{
		name: "issue1204",
		fun: func() cue.Value {
			ctx := New()
			expr := ast.NewCall(ast.NewIdent("close"), ast.NewStruct())
			return ctx.BuildExpr(expr)
		},
		want: `close({})`,
	}, {
		name: "issue1131",
		fun: func() cue.Value {
			m := make(map[string]interface{})
			ctx := New()
			cv := ctx.Encode(m)
			return cv
		},
		want: "", // empty file.
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := fmt.Sprintf("%#v", tc.fun())
			if got != tc.want {
				t.Errorf("got:\n%v;\nwant:\n%v", got, tc.want)
			}
		})
	}
}

// TestConcurrency tests whether concurrent use of an index is allowed.
// This test only functions well with the --race flag.
func TestConcurrency(t *testing.T) {
	c := New()
	go func() {
		c.CompileString(`
		package foo
		a: 1
		`)
	}()
	go func() {
		c.CompileString(`
		package bar
		a: 2
		`)
	}()
}

func TestEvalVersion(t *testing.T) {
	cueexperiment.Init()
	saved := cueexperiment.Flags.EvalV3
	defer func() { cueexperiment.Flags.EvalV3 = saved }()

	test := func(c *cue.Context, want internal.EvaluatorVersion) {
		t.Helper()
		opCtx := adt.NewContext((*runtime.Runtime)(c), nil)
		qt.Check(t, qt.Equals(opCtx.Version, want))
	}

	// The experiment evaluator version setting does not affect the specific
	// versions like Stable or V3, as they are fixed.
	testFixedVersions := func() {
		// We currently don't have an experimental version, so it's the current version.
		test(New(EvaluatorVersion(EvalExperiment)), internal.EvalV3)
		test(New(EvaluatorVersion(EvalV2)), internal.EvalV2)
		test(New(EvaluatorVersion(EvalV3)), internal.EvalV3)
	}

	// The current and default evaluator version is EvalV3.
	qt.Assert(t, qt.Equals(cueexperiment.Flags.EvalV3, true))
	test(New(), internal.EvalV3)
	// TODO(mvdan): explicitly selecting the default should result in evalv3 here,
	// just like implicitly selecting the default by not using the EvaluatorVersion flag.
	// It currently does not, because internally, we treat "unset" vs "default"
	// as different version selection scenarios.
	//
	// Or, if we want an evaluator version to describe "latest stable", opposing
	// EvalExperiment to describe "latest experimental", we should rename it to EvalStable
	// and keep its current behavior.
	test(New(EvaluatorVersion(EvalDefault)), internal.EvalV2)

	testFixedVersions()

	// Turning off the evalv3 experiment switches the default back to EvalV2.
	cueexperiment.Flags.EvalV3 = false
	test(New(), internal.EvalV2)
	test(New(EvaluatorVersion(EvalDefault)), internal.EvalV2)

	testFixedVersions()
}
