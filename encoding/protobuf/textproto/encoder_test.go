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
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/protobuf/textproto"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
)

func TestEncode(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/encoder",
		Name:   "encode",
		Update: cuetest.UpdateGoldenFiles,
	}

	r := cue.Runtime{}

	test.Run(t, func(t *cuetxtar.Test) {
		// TODO: use high-level API.

		var schema, value cue.Value

		for _, f := range t.Archive.Files {
			switch {
			case strings.HasSuffix(f.Name, ".cue"):
				inst, err := r.Compile(f.Name, f.Data)
				if err != nil {
					t.WriteErrors(errors.Promote(err, "test"))
					return
				}
				switch f.Name {
				case "schema.cue":
					schema = inst.Value()
				case "value.cue":
					value = inst.Value()
				}
			}
		}

		v := schema.Unify(value)
		if err := v.Err(); err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
			return
		}

		b, err := textproto.NewEncoder().Encode(v)
		if err != nil {
			t.WriteErrors(errors.Promote(err, "test"))
			return
		}
		_, _ = t.Write(b)

	})
}
