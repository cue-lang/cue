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

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/dep"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/value"
)

func TestVisit(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "dependencies",
		Update: cuetest.UpdateGoldenFiles,
	}

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		inst := cue.Build(a)[0].Value()
		if inst.Err() != nil {
			t.Fatal(inst.Err())
		}

		ctxt := eval.NewContext(value.ToInternal(inst))

		testCases := []struct {
			name string
			root string
			fn   func(*adt.OpContext, *adt.Vertex, dep.VisitFunc) error
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
			v := inst.LookupPath(cue.ParsePath(tc.root))

			_, n := value.ToInternal(v)
			w := t.Writer(tc.name)

			t.Run(tc.name, func(sub *testing.T) {
				tc.fn(ctxt, n, func(d dep.Dependency) error {
					str := value.Make(ctxt, d.Node).Path().String()
					if i := d.Import(); i != nil {
						path := i.ImportPath.StringValue(ctxt)
						str = fmt.Sprintf("%q.%s", path, str)
					}
					fmt.Fprintln(w, str)
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

	_ = dep.VisitFields(ctxt, n, func(d dep.Dependency) error {
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
