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

package adt_test

import (
	"runtime"
	"slices"
	"testing"
	"weak"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/value"
)

// TestReleaseEarlierEvaluations evaluates the same conjuncts repeatedly with
// one OpContext, as tools/flow does after every completed task, keeping only
// the latest result, and checks that the earlier results can be collected.
func TestReleaseEarlierEvaluations(t *testing.T) {
	v := cuecontext.New().CompileString(`
		import (
			"list"
			"tool/file"
		)

		// Fields unified with an imported definition holding a
		// disjunction make the evaluator recycle nodeContexts
		// between evaluations.
		for i in list.Range(0, 10, 1) {
			"t\(i)": file.MkdirAll & {path: "."}
		}

		// Further fields make a nodeContext freed by one evaluation
		// end up recycled by a disjunct of the next.
		data: {
			for i in list.Range(0, 10, 1) {
				"k\(i)": {a: i, b: "v\(i)"}
			}
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	ctx := value.OpContext(v)
	conjuncts := slices.Collect(value.Vertex(v).LeafConjuncts())
	var earlier []weak.Pointer[adt.Vertex]
	var latest *adt.Vertex
	for range 10 {
		latest = &adt.Vertex{Conjuncts: conjuncts}
		latest.Finalize(ctx)
		earlier = append(earlier, weak.Make(latest))
	}
	earlier = earlier[:len(earlier)-1]
	runtime.GC()
	retained := 0
	for _, wp := range earlier {
		if wp.Value() != nil {
			retained++
		}
	}
	runtime.KeepAlive(latest)
	if retained != 0 {
		t.Fatalf("%d of %d earlier evaluations stay reachable", retained, len(earlier))
	}
}
