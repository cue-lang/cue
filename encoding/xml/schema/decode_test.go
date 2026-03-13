// Copyright 2025 The CUE Authors
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

package schema_test

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/xml/schema"
	"cuelang.org/go/internal/cuetxtar"
)

func TestParse(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/decoder",
		Name: "decode",
	}

	d := schema.NewDecoder()

	test.Run(t, func(t *cuetxtar.Test) {
		var schemaVal cue.Value
		var filename string
		var b []byte

		for _, f := range t.Archive.Files {
			switch {
			case strings.HasSuffix(f.Name, ".cue"):
				schemaVal = t.CueContext().CompileBytes(f.Data)
				if err := schemaVal.Err(); err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}

			case strings.HasSuffix(f.Name, ".xml"):
				filename = f.Name
				b = f.Data
			}
		}

		x, err := d.Parse(schemaVal, filename, b)
		if err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
			return
		}

		f, err := astutil.ToFile(x)
		if err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
		}
		b, err = format.Node(f)
		if err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
		}
		_, _ = t.Write(b)
	})
}

func TestEncode(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/encoder",
		Name: "encode",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		var schemaVal, value cue.Value
		rootTag := "root"

		for _, f := range t.Archive.Files {
			switch f.Name {
			case "schema.cue":
				schemaVal = t.CueContext().CompileBytes(f.Data)
				if err := schemaVal.Err(); err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}
			case "value.cue":
				value = t.CueContext().CompileBytes(f.Data)
				if err := value.Err(); err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}
			case "root.txt":
				rootTag = strings.TrimSpace(string(f.Data))
			}
		}

		v := schemaVal.Unify(value)
		if err := v.Err(); err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
			return
		}

		b, err := schema.NewEncoder().Encode(v, rootTag)
		if err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
			return
		}
		_, _ = t.Write(b)
	})
}
