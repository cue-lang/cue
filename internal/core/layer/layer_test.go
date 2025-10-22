// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package layer_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/layer"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cuetxtar"
)

func TestLayers(t *testing.T) {
	adt.DebugDeps = true // check unmatched dependencies.

	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "layer",
	}

	cuedebug.Init()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.Instance()
		ctx := cuecontext.New()

		// Inject layering based on @layer attribute.
		for _, f := range a.Files {
			for i, d := range f.Decls {
				_ = i
				attr, ok := d.(*ast.Attribute)
				if !ok {
					// no preamble
					break
				}
				key, body := attr.Split()
				if key != "layer" {
					continue
				}

				args := strings.Split(body, ",")

				priority, err := strconv.ParseInt(args[0], 10, 8)
				if err != nil {
					t.Fatalf("invalid priority %q: %v", args[0], err)
				}

				f.Pos().File().SetLayer(&layer.Layer{
					Priority: layer.Priority(priority),
					IsData:   slices.Contains(args, "defaultData"),
				})

				f.Decls = slices.Delete(f.Decls, i, i+1)
				break
			}
		}

		v := ctx.BuildInstance(a)
		if err := v.Err(); err != nil {
			t.WriteErrors(errors.Promote(err, "layers"))
			return
		}

		s := fmt.Sprint(v)
		t.Write([]byte(s))
		return
	})
}
