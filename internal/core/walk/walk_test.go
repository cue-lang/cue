// Copyright 2026 CUE Authors
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

package walk_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/value"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/walk"
)

// TestFeaturesFuncCallRef verifies that walking a function call reference
// reports the features used by all signatures enforced on the call: not
// only the called function's own expressions, but also the parameter and
// result constraints of the function types the value was tightened with.
// Export's markUsedFeatures relies on this to avoid generating synthetic
// names that collide with names referenced only by a type constraint.
func TestFeaturesFuncCallRef(t *testing.T) {
	const config = `
@experiment(functions)

lim: 10
T:   func(n: <lim, ...) -> _
f:   T & (func(n: number) -> _: {v: n})
`
	ctx := cuecontext.New()
	scope := ctx.CompileString(config)
	if err := scope.Err(); err != nil {
		t.Fatal(err)
	}

	// Evaluating a call expression yields the inline call vertex, whose
	// conjunct is the call reference carrying the tightened value's type
	// constraints. This mirrors how export processes the result of an
	// expression evaluation (e.g. cue def -e "f(5)").
	expr, err := parser.ParseExpr("expr", "f(5)")
	if err != nil {
		t.Fatal(err)
	}
	v := ctx.BuildExpr(expr, cue.Scope(scope))
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	r, vertex := value.ToInternal(v)

	var ref *adt.FuncCallRef
	for c := range vertex.LeafConjuncts() {
		if x, ok := c.Elem().(*adt.FuncCallRef); ok {
			ref = x
		}
	}
	if ref == nil {
		t.Fatal("no FuncCallRef conjunct found on call result vertex")
	}

	features := map[string]bool{}
	w := &walk.Visitor{
		Feature: func(f adt.Feature, src adt.Node) {
			features[f.SelectorString(r)] = true
		},
	}
	w.Elem(ref)

	// lim is referenced only by the parameter bound of T, which constrains
	// the call through the reference's recorded signature constraints.
	if !features["lim"] {
		t.Errorf("feature lim not reported; got %v", features)
	}
}
