// Copyright 2019 CUE Authors
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

package jsonschema_test

import (
	"bytes"
	"path"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	_ "cuelang.org/go/pkg"
)

// TestDecode reads the testdata/*.txtar files, converts the contained
// JSON schema to CUE and compares it against the output.
//
// Set CUE_UPDATE=1 to update test files with the corresponding output.
func TestDecode(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "decode",
		Matrix: cuetdtest.FullMatrix,
	}
	test.Run(t, func(t *cuetxtar.Test) {
		cfg := &jsonschema.Config{}

		if t.HasTag("openapi") {
			cfg.Root = "#/components/schemas/"
			cfg.Map = func(p token.Pos, a []string) ([]ast.Label, error) {
				// Just for testing: does not validate the path.
				return []ast.Label{ast.NewIdent("#" + a[len(a)-1])}, nil
			}
		}

		ctx := t.Context()
		var v cue.Value

		for _, f := range t.Archive.Files {
			switch path.Ext(f.Name) {
			case ".json":
				expr, err := json.Extract(f.Name, f.Data)
				if err != nil {
					t.Fatal(err)
				}
				v = ctx.BuildExpr(expr)
			case ".yaml":
				file, err := yaml.Extract(f.Name, f.Data)
				if err != nil {
					t.Fatal(err)
				}
				v = ctx.BuildFile(file)
			}
		}

		expr, err := jsonschema.Extract(v, cfg)
		if err != nil {
			got := []byte(errors.Details(err, nil))
			got = append(bytes.TrimSpace(got), '\n')
			t.Writer("err").Write(got)
		}

		if expr != nil {
			b, err := format.Node(expr, format.Simplify())
			if err != nil {
				t.Fatal(errors.Details(err, nil))
			}

			// verify the generated CUE.
			if !t.HasTag("noverify") {
				v := ctx.CompileBytes(b, cue.Filename("generated.cue"))
				if err := v.Err(); err != nil {
					t.Fatal(errors.Details(err, nil))
				}
			}

			b = append(bytes.TrimSpace(b), '\n')
			t.Writer("cue").Write(b)
		}
	})
}

func TestX(t *testing.T) {
	t.Skip()
	data := `
-- schema.json --
`

	a := txtar.Parse([]byte(data))

	ctx := cuecontext.New()
	var v cue.Value
	var err error
	for _, f := range a.Files {
		switch path.Ext(f.Name) {
		case ".json":
			expr, err := json.Extract(f.Name, f.Data)
			if err != nil {
				t.Fatal(err)
			}
			v = ctx.BuildExpr(expr)
		case ".yaml":
			file, err := yaml.Extract(f.Name, f.Data)
			if err != nil {
				t.Fatal(err)
			}
			v = ctx.BuildFile(file)
		}
	}

	cfg := &jsonschema.Config{ID: "test"}
	expr, err := jsonschema.Extract(v, cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Fatal(astinternal.DebugStr(expr))
}
