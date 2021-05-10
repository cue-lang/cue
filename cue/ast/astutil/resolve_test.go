// Copyright 2021 CUE Authors
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

package astutil_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"text/tabwriter"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
)

func TestResolve(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/resolve",
		Name:   "resolve",
		Update: cuetest.UpdateGoldenFiles,
	}

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		for _, f := range a[0].Files {
			if filepath.Ext(f.Filename) != ".cue" {
				continue
			}

			identMap := map[ast.Node]int{}
			ast.Walk(f, func(n ast.Node) bool {
				switch n.(type) {
				case *ast.File, *ast.StructLit, *ast.Field, *ast.ImportSpec,
					*ast.Ident, *ast.ForClause, *ast.LetClause, *ast.Alias:
					identMap[n] = len(identMap) + 1
				}
				return true
			}, nil)

			// Resolve was already called.

			base := filepath.Base(f.Filename)
			b := t.Writer(base[:len(base)-len(".cue")])
			w := tabwriter.NewWriter(b, 0, 4, 1, ' ', 0)
			ast.Walk(f, func(n ast.Node) bool {
				if x, ok := n.(*ast.Ident); ok {
					fmt.Fprintf(w, "%d[%s]:\tScope: %d[%T]\tNode: %d[%s]\n",
						identMap[x], astinternal.DebugStr(x),
						identMap[x.Scope], x.Scope,
						identMap[x.Node], astinternal.DebugStr(x.Node))
				}
				return true
			}, nil)
			w.Flush()

			fmt.Fprintln(b)
		}
	})
}
