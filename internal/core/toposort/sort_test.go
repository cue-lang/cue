// Copyright 2024 CUE Authors
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

package toposort_test

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/toposort"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/value"
)

func TestTopologicalSort(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "testdata",
		Name:   "toposort",
		Matrix: cuetdtest.SmallMatrix,
	}

	for _, lexico := range []bool{false, true} {
		t.Run(fmt.Sprintf("lexicographical=%v", lexico), func(t *testing.T) {
			test.Name = t.Name()
			test.Run(t, func(t *cuetxtar.Test) {
				t.Flags.SortFields = lexico
				run := t.Runtime()

				inst := t.Instance()

				v, err := run.Build(nil, inst)
				if err != nil {
					t.Fatal(err)
				}

				v.Finalize(adt.NewContext(run, v))

				evalWithOptions := export.Profile{
					TakeDefaults:    true,
					ShowOptional:    true,
					ShowDefinitions: true,
					ShowAttributes:  true,
				}

				render := func(ctx *adt.OpContext) []byte {
					expr, err := evalWithOptions.Value(ctx, inst.ID(), v)
					if err != nil {
						t.Fatal(err)
					}
					b, fmtErr := format.Node(expr)
					if fmtErr != nil {
						t.Fatal(fmtErr)
					}
					return b
				}

				// A fresh context, so the stats cover only the export.
				ctx := adt.NewContext(run, v)
				b := render(ctx)
				if !lexico {
					// Record the stats once, for the mode whose graph
					// keeps the edges it requests.
					fmt.Fprintln(t.WriterFallback("stats"), ctx.Stats())
				}
				_, _ = t.Write(b)

				// The order of fields must not depend on the order in which
				// the evaluator added structs to a vertex. Reversing swaps
				// every pair, which a shuffle may not; a shuffle also covers
				// the other permutations.
				permuteStructs(v, slices.Reverse)
				if got := render(adt.NewContext(run, v)); !bytes.Equal(got, b) {
					t.Errorf("field order differs with the structs reversed:\n%s", got)
				}
				seed := rand.Uint64()
				rng := rand.New(rand.NewPCG(seed, 0))
				permuteStructs(v, func(s []adt.StructInfo) {
					rng.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
				})
				if got := render(adt.NewContext(run, v)); !bytes.Equal(got, b) {
					t.Errorf("field order differs with the structs shuffled by seed %d:\n%s", seed, got)
				}
			})
		})
	}
}

// permuteStructs applies permute to [adt.Vertex.Structs] in v and its descendants.
func permuteStructs(v *adt.Vertex, permute func([]adt.StructInfo)) {
	permute(v.Structs)
	for _, arc := range v.Arcs {
		permuteStructs(arc, permute)
	}
}

func TestIssue3698(t *testing.T) {
	str := `import "list"
list.Repeat([{x: 5}], 1000)
`
	ctx := cuecontext.New()
	val := ctx.CompileString(str, cue.Filename("issue3698"))
	if err := val.Err(); err != nil {
		t.Fatal(err)
	}
	run, v := value.ToInternal(val)
	// If toposort does not correctly handle feature names,
	// specifically IntLabels, then this will panic:
	toposort.VertexFeatures(adt.NewContext(run, v), v)
}
