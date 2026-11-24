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

package textproto_test

import (
	"bytes"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/protobuf/textproto"
	"cuelang.org/go/internal/cuetxtar"
)

func TestParse(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/decoder",
		Name: "decode",
	}

	d := textproto.NewDecoder()

	test.Run(t, func(t *cuetxtar.Test) {
		// TODO: use high-level API.

		var schema cue.Value
		var filename string
		var b []byte

		for _, f := range t.Archive.Files {
			switch {
			case strings.HasSuffix(f.Name, ".cue"):
				schema = t.CueContext().CompileBytes(f.Data)
				if err := schema.Err(); err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}

			case strings.HasSuffix(f.Name, ".textproto"):
				filename = f.Name
				b = f.Data
			}
		}

		decode := func() ([]byte, error) {
			x, err := d.Parse(schema, filename, b)
			if err != nil {
				return nil, err
			}
			f, err := astutil.ToFile(x)
			if err != nil {
				return nil, err
			}
			return format.Node(f)
		}
		render := func() []byte {
			out, err := decode()
			if err == nil {
				return out
			}
			var buf bytes.Buffer
			errors.Print(&buf, errors.Promote(err, "test"), &errors.Config{
				Cwd:     t.Dir,
				ToSlash: true,
			})
			return buf.Bytes()
		}
		first := render()
		_, _ = t.Write(first)

		// Record a second Parse with the same Decoder if its result differs.
		if second := render(); !bytes.Equal(first, second) {
			_, _ = t.Writer("reparse").Write(second)
		}
	})
}
