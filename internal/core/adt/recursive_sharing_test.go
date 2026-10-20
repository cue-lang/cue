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
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
)

// Recursive constraints should preserve sharing regardless of the order in
// which the referenced fields are declared. Both graphs have O(depth) edges;
// unfolding either graph while applying #T causes exponential evaluation.
func TestRecursiveSharingComplexity(t *testing.T) {
	for _, shape := range []string{"fanin", "disjunction"} {
		t.Run(shape, func(t *testing.T) {
			for _, order := range []string{"ascending", "reverse", "mixed"} {
				t.Run(order, func(t *testing.T) {
					// Stop at the first excessive count, before trying larger
					// depths that would be expensive with broken sharing.
					for _, depth := range []int{2, 4, 8, 16, 32} {
						ctx, v := evalRecursiveSharing(t, recursiveSharingSource(shape, order, depth))
						counts := *ctx.Stats()
						t.Logf("depth %d: %d unifications, %d disjuncts", depth, counts.Unifications, counts.Disjuncts)
						// Leave room for evaluator bookkeeping without allowing
						// repeated expansion of the dependency graph.
						budget := int64(64 * (depth + 1))
						if counts.Unifications > budget || counts.Disjuncts > budget {
							t.Fatalf("depth %d: evaluation exceeds linear budget %d: %d unifications, %d disjuncts",
								depth, budget, counts.Unifications, counts.Disjuncts)
						}
						if b := adt.Validate(ctx, v, &adt.ValidateConfig{Concrete: true}); b != nil {
							t.Fatalf("depth %d: unexpected evaluation error: %v", depth, b.Err)
						}
					}
				})
			}
		})
	}
}

func recursiveSharingSource(shape, order string, depth int) string {
	var src strings.Builder
	switch shape {
	case "fanin":
		src.WriteString("#T: {cmd: string, deps?: [...#T]}\n")
	case "disjunction":
		src.WriteString(`#T: {
	cmd?: string
	script?: string
	({cmd!: string, script?: _|_} | {script!: string, cmd?: _|_})
	deps?: [...#T]
}
`)
	}
	src.WriteString("tasks: [string]: #T\ntasks: {\n")
	var fields []string
	for i := 0; i <= depth; i++ {
		switch shape {
		case "fanin":
			for _, prefix := range []string{"a", "b"} {
				field := fmt.Sprintf("%s%d: {cmd: \"x\"", prefix, i)
				if i > 0 {
					field += fmt.Sprintf(", deps: [a%d, b%d]", i-1, i-1)
				}
				fields = append(fields, field+"}\n")
			}
		case "disjunction":
			field := fmt.Sprintf("n%d: {cmd: \"x\"", i)
			if i > 0 {
				field += fmt.Sprintf(", deps: [n%d]", i-1)
			}
			fields = append(fields, field+"}\n")
		}
	}
	switch order {
	case "reverse":
		slices.Reverse(fields)
	case "mixed":
		// Alternate the last and first remaining declarations, exercising
		// both forward and backward references in the same struct.
		var mixed []string
		for len(fields) > 0 {
			mixed = append(mixed, fields[len(fields)-1])
			fields = fields[:len(fields)-1]
			if len(fields) > 0 {
				mixed = append(mixed, fields[0])
				fields = fields[1:]
			}
		}
		fields = mixed
	}
	for _, field := range fields {
		src.WriteString(field)
	}
	src.WriteString("}\n")
	return src.String()
}

// Sharing an already typed dependency must not hide a constraint added to that
// dependency elsewhere. The extra a1 declaration introduces a structural cycle
// between a1 and a2, contradicting a1's original dependency on a0.
func TestRecursiveSharingRejectsCycle(t *testing.T) {
	fields := []string{
		`a0: {cmd: "x"}`,
		`a1: {cmd: "x", deps: [a0]}`,
		`a2: {cmd: "x", deps: [a0, a1]}`,
		`a1: deps: [a2]`,
	}
	// Include all declaration permutations, including constraints before and
	// after the original value and forward and backward dependency references.
	for a := range fields {
		for b := range fields {
			if b == a {
				continue
			}
			for c := range fields {
				if c == a || c == b {
					continue
				}
				d := 6 - a - b - c // 0 + 1 + 2 + 3
				t.Run(fmt.Sprintf("%d%d%d%d", a, b, c, d), func(t *testing.T) {
					src := "#T: {cmd: string, deps?: [...#T]}\ntasks: [string]: #T\ntasks: {\n"
					src += strings.Join([]string{fields[a], fields[b], fields[c], fields[d]}, "\n") + "\n}\n"
					ctx, v := evalRecursiveSharing(t, src)
					b := adt.Validate(ctx, v, &adt.ValidateConfig{Concrete: true})
					if b == nil {
						t.Fatal("accepted cyclic dependencies with conflicting list constraints")
					}
					if b.Code != adt.StructuralCycleError {
						t.Fatalf("expected structural cycle, got %v: %v", b.Code, b.Err)
					}
				})
			}
		}
	}
}

func evalRecursiveSharing(t *testing.T, src string) (*adt.OpContext, *adt.Vertex) {
	t.Helper()
	r := runtime.NewWithSettings(internal.EvalV3, cuedebug.Config{Sharing: true})
	v, inst := r.Compile(&runtime.Config{Filename: "sharing.cue"}, src)
	if inst.Err != nil {
		t.Fatal(inst.Err)
	}
	ctx := adt.NewContext(r, v)
	v.Finalize(ctx)
	return ctx, v
}

// Selecting a disjunct that refers back to its consumer must retain structural
// cycle detection, even when the referenced task already satisfies #T.
func TestRecursiveSharingDisjunctCycle(t *testing.T) {
	for _, disjunctOrder := range []string{"cycleFirst", "cycleLast"} {
		for _, fieldOrder := range []string{"ascending", "reverse"} {
			for _, aliases := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/aliases=%t", disjunctOrder, fieldOrder, aliases), func(t *testing.T) {
					disjuncts := []string{`{cmd: "x", deps: [a1]}`, `{cmd: "y"}`}
					if disjunctOrder == "cycleLast" {
						slices.Reverse(disjuncts)
					}
					fields := []string{
						"a0: (" + strings.Join(disjuncts, " | ") + `) & {cmd: "x"}`,
						`a1: {cmd: "x", deps: [a0]}`,
					}
					if fieldOrder == "reverse" {
						slices.Reverse(fields)
					}
					src := "#T: {cmd: string, deps?: [...#T]}\ntasks: [string]: #T\ntasks: {\n"
					src += strings.Join(fields, "\n") + "\n}\n"
					if aliases {
						src += `
alias0: (#T & tasks.a0) | ({cmd: "z"} & tasks.a0)
alias1: (#T & tasks.a1) | ({cmd: "z"} & tasks.a1)
`
					}
					ctx, v := evalRecursiveSharing(t, src)
					b := adt.Validate(ctx, v, &adt.ValidateConfig{Concrete: true})
					if b == nil {
						t.Fatal("accepted a structural cycle through a selected disjunct")
					}
					if b.Code != adt.StructuralCycleError {
						t.Fatalf("expected structural cycle, got %v: %v", b.Code, b.Err)
					}
				})
			}
		}
	}
}

func TestRecursiveSharingDisjunctionSemantics(t *testing.T) {
	const exclusive = `#T: {
	cmd?: string
	script?: string
	({cmd!: string, script?: _|_} | {script!: string, cmd?: _|_})
}
`
	tests := []struct {
		name    string
		src     string
		path    string
		want    string
		wantErr bool
	}{{
		name: "member selects exclusive branch",
		src:  exclusive + `x: #T & {cmd: "x"}`,
		path: "x",
		want: `{"cmd":"x"}`,
	}, {
		name: "optional field does not eliminate branch",
		src: `x: {
	cmd?: string
	({cmd: "x", script?: _|_} | *{script: "y", cmd?: _|_})
}`,
		path: "x",
		want: `{"script":"y"}`,
	}, {
		name: "selected branch preserves defaults",
		src: `x: {
	cmd: "x"
	({cmd: string, script?: _|_, value: *1 | 2} | {script: string, cmd?: _|_})
}`,
		path: "x",
		want: `{"cmd":"x","value":1}`,
	}, {
		name: "multiple disjunctions",
		src: `x: {
	cmd?: string
	script?: string
	({cmd!: string, script?: _|_} | {script!: string, cmd?: _|_})
	({n: 1} | {n: 2})
	cmd: "x"
	n: 2
}`,
		path: "x",
		want: `{"cmd":"x","n":2}`,
	}, {
		name: "common field depends on selected branch",
		src:  `x: {a: b, b: int, (*{b: 1} | {b: 2})}`,
		path: "x",
		want: `{"a":1,"b":1}`,
	}, {
		name: "conditional field depends on selected branch",
		src: `x: {
	k: 1
	b: int
	if k == 1 {a: b}
	(*{b: 1} | {b: 2})
}`,
		path: "x",
		want: `{"k":1,"b":1,"a":1}`,
	}, {
		name: "aliased data disjunction preserves dependencies",
		src: `#T: {cmd: string, deps?: [...#T]}
choice: ({cmd: "x", deps: [tasks.a1]} | {cmd: "y"})
tasks: [string]: #T
tasks: {
	a0: choice & {cmd: "x"}
	a1: {cmd: "x", deps: [a0]}
}`,
		path: "tasks.a0.deps[0].cmd",
		want: `"x"`,
	}, {
		name:    "both exclusive fields conflict",
		src:     exclusive + `x: #T & {cmd: "x", script: "y"}`,
		path:    "x",
		wantErr: true,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := cuecontext.New().CompileString(tc.src).LookupPath(cue.ParsePath(tc.path))
			if tc.wantErr {
				if err := v.Validate(); err == nil {
					t.Fatal("accepted conflicting exclusive fields")
				}
				return
			}
			got, err := v.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s; want %s", got, tc.want)
			}
		})
	}
}

// Read operations on finalized values may allocate local evaluation state, but
// must not mutate the shared graph. These bounded loops also run under -race.
func TestRecursiveSharingConcurrentReads(t *testing.T) {
	t.Run("bounded cyclic fields", func(t *testing.T) {
		v := cuecontext.New().CompileString(`
#T: {cmd: string, deps?: [...]}
tasks: [string]: #T
tasks: {
	a0: {cmd: "x"}
	a1: {cmd: "x", deps: [a0]}
	a2: {cmd: "x", deps: [a0, a1]}
	a1: deps: [a2]
}

`).LookupPath(cue.ParsePath("tasks"))
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				for range 32 {
					iter, err := v.Fields()
					if err != nil {
						t.Error(err)
						return
					}
					var count int
					for iter.Next() {
						count++
					}
					if count != 3 {
						t.Errorf("got %d task fields; want 3", count)
					}
				}
			})
		}
		wg.Wait()
	})
	t.Run("synthetic pattern lookup", func(t *testing.T) {
		v := cuecontext.New().CompileString(`#T: {[string]: {self: #T}}`).
			LookupPath(cue.MakePath(cue.Def("T")))
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				for range 32 {
					if err := v.LookupPath(cue.MakePath(cue.AnyString)).Validate(); err == nil || !strings.Contains(err.Error(), "structural cycle") {
						t.Errorf("expected structural cycle, got %v", err)
						return
					}
				}
			})
		}
		wg.Wait()
	})
}

// An exclusive branch may itself introduce a dependency. Selecting that branch
// must still detect cycles through both direct and conditional references.
func TestRecursiveSharingExclusiveBranchCycle(t *testing.T) {
	for _, tc := range []struct {
		name string
		a1   string
	}{
		{"direct", `a1: {cmd: "x", deps: [a0]}`},
		{"conditional", `a1: {cmd: "x", if a0.cmd == "x" {deps: [a0]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `#T: {cmd?: string, script?: string, deps?: [...#T]}
tasks: [string]: #T
tasks: {
	a0: ({cmd!: string, script?: _|_, deps: [a1]} |
		{cmd?: _|_, script!: string}) & {cmd: "x"}
	` + tc.a1 + "\n}\n"
			ctx, v := evalRecursiveSharing(t, src)
			b := adt.Validate(ctx, v, &adt.ValidateConfig{Concrete: true})
			if b == nil {
				t.Fatal("accepted a structural cycle introduced by an exclusive branch")
			}
			if b.Code != adt.StructuralCycleError {
				t.Fatalf("expected structural cycle, got %v: %v", b.Code, b.Err)
			}
		})
	}
}
