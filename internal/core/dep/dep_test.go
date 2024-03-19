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
	"io"
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

type visitFunc func(*adt.OpContext, *adt.ImportReference, *adt.Vertex, dep.VisitFunc) error

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
			cfg  *dep.Config
		}{{
			name: "field",
			root: "a.b",
			cfg:  nil,
		}, {
			name: "all",
			root: "a",
			cfg:  &dep.Config{Descend: true},
		}, {
			name: "dynamic",
			root: "a",
			cfg:  &dep.Config{Dynamic: true},
		}}

		for _, tc := range testCases {
			v := val.LookupPath(cue.ParsePath(tc.root))

			_, n := value.ToInternal(v)
			w := t.Writer(tc.name)

			t.Run(tc.name, func(sub *testing.T) {
				testVisit(sub, w, ctxt, n, tc.cfg)
			})
		}
	})
}

func testVisit(t *testing.T, w io.Writer, ctxt *adt.OpContext, v *adt.Vertex, cfg *dep.Config) {
	t.Helper()

	tw := tabwriter.NewWriter(w, 0, 4, 1, ' ', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "line \vreference\v   path of resulting vertex\n")

	dep.Visit(cfg, ctxt, v, func(d dep.Dependency) error {
		if d.Reference == nil {
			t.Fatal("no reference")
		}

		src := d.Reference.Source()
		line := src.Pos().Line()
		b, _ := format.Node(src)
		ref := string(b)
		str := value.Make(ctxt, d.Node).Path().String()

		if i := d.Import(); i != nil {
			path := i.ImportPath.StringValue(ctxt)
			str = fmt.Sprintf("%q.%s", path, str)
		} else if !d.Node.Rooted() {
			str = "**non-rooted**"
		}

		fmt.Fprintf(tw, "%d:\v%s\v=> %s\n", line, ref, str)

		return nil
	})
}

// DO NOT REMOVE: for Testing purposes.
func TestX(t *testing.T) {
	cfg := &dep.Config{
		Dynamic: true,
		// Recurse: true,
	}

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

	n.VisitLeafConjuncts(func(c adt.Conjunct) bool {
		str := debug.NodeString(ctxt, c.Elem(), nil)
		t.Log(str)
		return true
	})

	w := &strings.Builder{}
	fmt.Fprintln(w)

	testVisit(t, w, ctxt, n, cfg)

	t.Error(w.String())
}
