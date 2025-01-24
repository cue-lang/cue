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
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
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
				run.SetTopologicalSort(true)

				inst := t.Instance()

				v, err := run.Build(nil, inst)
				if err != nil {
					t.Fatal(err)
				}

				v.Finalize(eval.NewContext(run, v))

				evalWithOptions := export.Profile{
					TakeDefaults:    true,
					ShowOptional:    true,
					ShowDefinitions: true,
					ShowAttributes:  true,
				}

				expr, err := evalWithOptions.Value(run, inst.ID(), v)
				if err != nil {
					t.Fatal(err)
				}

				{
					b, err := format.Node(expr)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = t.Write(b)
					fmt.Fprintln(t)
				}
			})
		})
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
