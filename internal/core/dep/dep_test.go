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

package dep_test

import (
	"fmt"
	"strings"
	"testing"
	"text/tabwriter"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/dep"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/value"
)

func TestVisit(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "dependencies",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		val := cuecontext.New().BuildInstance(t.Instance())
		if val.Err() != nil {
			t.Fatal(val.Err())
		}

		ctxt := eval.NewContext(value.ToInternal(val))

		testCases := []struct {
			name string
			root string
			fn   func(*adt.OpContext, *adt.ImportReference, *adt.Vertex, dep.VisitFunc) error
		}{{
			name: "field",
			root: "a.b",
			fn:   dep.Visit,
		}, {
			name: "all",
			root: "a",
			fn:   dep.VisitAll,
		}, {
			name: "dynamic",
			root: "a",
			fn:   dep.VisitFields,
		}}

		for _, tc := range testCases {
			v := val.LookupPath(cue.ParsePath(tc.root))

			_, n := value.ToInternal(v)
			w := t.Writer(tc.name)

			t.Run(tc.name, func(sub *testing.T) {
				tw := tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
				defer tw.Flush()

				tc.fn(ctxt, nil, n, func(d dep.Dependency) error {
					var ref string
					var line int
					// TODO: remove check at some point.
					if d.Reference != nil {
						src := d.Reference.Source()
						line = src.Pos().Line()
						b, _ := format.Node(src)
						ref = string(b)
					}
					str := value.Make(ctxt, d.Node).Path().String()
					if i := d.Import(); i != nil {
						path := i.ImportPath.StringValue(ctxt)
						str = fmt.Sprintf("%q.%s", path, str)
					}
					fmt.Fprintf(tw, "%d:\v%s:\v%s\n", line, ref, str)
					return nil
				})
			})
		}
	})
}

// DO NOT REMOVE: for Testing purposes.
func TestX(t *testing.T) {
	in := `
	`

	if strings.TrimSpace(in) == "" {
		t.Skip()
	}

	rt := cue.Runtime{}
	inst, err := rt.Compile("", in)
	if err != nil {
		t.Fatal(err)
	}

	v := inst.Lookup("a")

	r, n := value.ToInternal(v)

	ctxt := eval.NewContext(r, n)

	for _, c := range n.Conjuncts {
		str := debug.NodeString(ctxt, c.Elem(), nil)
		t.Log(str)
	}

	deps := []string{}

	_ = dep.VisitFields(ctxt, nil, n, func(d dep.Dependency) error {
		str := value.Make(ctxt, d.Node).Path().String()
		if i := d.Import(); i != nil {
			path := i.ImportPath.StringValue(ctxt)
			str = fmt.Sprintf("%q.%s", path, str)
		}
		deps = append(deps, str)
		return nil
	})

	t.Error(deps)
}
