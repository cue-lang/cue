// Copyright 2025 CUE Authors
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
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/go-quicktest/qt"
)

func TestGenerate(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/generate",
		Name:   "generate",
		Matrix: cuetdtest.SmallMatrix,
	}
	test.Run(t, func(t *cuetxtar.Test) {
		ctx := t.CueContext()
		v := ctx.BuildInstance(t.Instance())
		qt.Assert(t, qt.IsNil(v.Err()))
		if p, ok := t.Value("path"); ok {
			v = v.LookupPath(cue.ParsePath(p))
		}
		r, err := jsonschema.Generate(v, nil)
		qt.Assert(t, qt.IsNil(err))
		data, err := format.Node(r)
		qt.Assert(t, qt.IsNil(err))
		t.Write(data)

		// Round-trip test: convert generated JSON Schema back to CUE to validate
		// First compile the AST to a CUE value, then marshal to JSON
		schemaValue := ctx.BuildExpr(r)
		qt.Assert(t, qt.IsNil(schemaValue.Err()))

		schemaBytes, err := schemaValue.MarshalJSON()
		qt.Assert(t, qt.IsNil(err))

		// Parse the JSON back to a CUE value for extraction
		schemaValue = ctx.CompileBytes(schemaBytes)
		qt.Assert(t, qt.IsNil(schemaValue.Err()))

		// Extract back to CUE with strict validation
		_, err = jsonschema.Extract(schemaValue, &jsonschema.Config{
			StrictFeatures: true,
			StrictKeywords: true,
		})
		qt.Assert(t, qt.IsNil(err), qt.Commentf("generated JSON Schema should round-trip cleanly via Extract"))
	})
}
