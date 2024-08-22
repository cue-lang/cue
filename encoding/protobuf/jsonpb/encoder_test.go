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
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/encoding/protobuf/jsonpb"
	"cuelang.org/go/internal/cuetxtar"
)

func TestEncoder(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/encoder",
		Name: "jsonpb",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		// TODO: use high-level API.

		var schema cue.Value
		var file *ast.File

		for _, f := range t.Archive.Files {
			switch f.Name {
			case "schema.cue":
				schema = t.CueContext().CompileBytes(f.Data)
				if err := schema.Err(); err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}
			case "value.cue":
				f, err := parser.ParseFile(f.Name, f.Data, parser.ParseComments)
				if err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}
				file = f
			}
		}

		if !schema.Exists() {
			schema = t.CueContext().BuildFile(file)
			if err := schema.Err(); err != nil {
				t.WriteErrors(errors.Promote(err, "test"))
			}
		}

		err := jsonpb.NewEncoder(schema).RewriteFile(file)
		if err != nil {
			errors.Print(t, err, nil)
		}

		b, err := format.Node(file)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = t.Write(b)
	})
}
