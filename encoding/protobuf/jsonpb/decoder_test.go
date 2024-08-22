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

package jsonpb_test

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/protobuf/jsonpb"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal/cuetxtar"
)

func TestParse(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/decoder",
		Name: "jsonpb",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		// TODO: use high-level API.

		var schema cue.Value
		var file *ast.File

		for _, f := range t.Archive.Files {
			switch {
			case f.Name == "schema.cue":
				schema = t.CueContext().CompileBytes(f.Data, cue.Filename(f.Name))
				if err := schema.Err(); err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}
				continue

			case strings.HasPrefix(f.Name, "out/"):
				continue

			case strings.HasSuffix(f.Name, ".cue"):
				f, err := parser.ParseFile(f.Name, f.Data, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				file = f

			case strings.HasSuffix(f.Name, ".json"):
				x, err := json.Extract(f.Name, f.Data)
				if err != nil {
					t.Fatal(err)
				}
				file, err = astutil.ToFile(x)
				if err != nil {
					t.Fatal(err)
				}

			case strings.HasSuffix(f.Name, ".yaml"):
				f, err := yaml.Extract(f.Name, f.Data)
				if err != nil {
					t.Fatal(err)
				}
				file = f
			}

			w := t.Writer(f.Name)
			err := jsonpb.NewDecoder(schema).RewriteFile(file)
			if err != nil {
				errors.Print(w, err, nil)
				continue
			}

			b, err := format.Node(file)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write(b)
		}
	})
}

// For debugging purposes: DO NOT REMOVE.
func TestX(t *testing.T) {
	const schema = `

		`
	const data = `
`
	if strings.TrimSpace(data) == "" {
		t.Skip()
	}
	val := cuecontext.New().CompileString(schema)
	if err := val.Err(); err != nil {
		t.Fatal(err)
	}

	file, err := parser.ParseFile("data", data)
	if err != nil {
		t.Fatal(err)
	}

	if err := jsonpb.NewDecoder(val).RewriteFile(file); err != nil {
		t.Fatal(err)
	}

	b, err := format.Node(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Error(string(b))
}
