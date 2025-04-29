// Copyright 2025 CUE Authors
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

package genfunc

import (
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
)

var simplifyTests = []struct {
	testName string
	cue      string
	want     string
}{{
	testName: "SimpleScalar",
	cue:      "true",
	want:     "true",
}, {
	testName: "UnifyIdentical",
	cue:      "true & true",
	want:     "true",
}, {
	testName: "UnifyIdenticalLeft",
	cue:      "true & (true & true)",
	want:     "true",
}, {
	testName: "UnifyIdenticalRight",
	cue:      "(true & true) & true",
	want:     "true",
}, {
	// TODO
	testName: "DisjointIdentical",
	cue:      "true | true",
	want:     "true | true",
}, {
	testName: "SimpleEmbed",
	cue:      "{true}",
	want:     "true",
}, {
	testName: "ConjunctionDistributesOverDisjunction",
	cue:      "(1 | 2) & int",
	want:     "int & 1 | int & 2",
}, {
	testName: "ConjunctionDistributesOverDisjunctionWithDefault",
	cue:      "(*1 | 2) & int",
	want:     "*(int & 1) | int & 2",
}, {
	testName: "DisjunctElimination",
	cue:      `*("" & "go") | string & "go"`,
	want:     `"go"`,
}}

func TestSimplify(t *testing.T) {
	ctx := cuecontext.New()
	for _, test := range simplifyTests {
		t.Run(test.testName, func(t *testing.T) {
			v := ctx.CompileString(test.cue)
			qt.Assert(t, qt.IsNil(v.Err()))
			before := v.Syntax(cue.Raw()).(ast.Expr)
			t.Logf("before: %s", dump(before))
			after := simplify(before)
			qt.Assert(t, qt.Equals(dump(after), test.want))
		})
	}
}
