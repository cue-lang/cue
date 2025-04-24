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
