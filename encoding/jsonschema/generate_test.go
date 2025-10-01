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
	"io"
	"io/fs"
	"maps"
	"slices"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestGenerate(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/generate",
		Name:   "generate",
		Matrix: cuetdtest.SmallMatrix,
	}
	test.Run(t, func(t *cuetxtar.Test) {
		t.Logf("test name %q", t.T.Name())
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
		t.Writer("schema").Write(data)

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
		extractedSchemaFile, err := jsonschema.Extract(schemaValue, &jsonschema.Config{
			StrictFeatures: true,
			StrictKeywords: true,
		})
		qt.Assert(t, qt.IsNil(err), qt.Commentf("generated JSON Schema should round-trip cleanly via Extract"))
		extractedSchemaValue := ctx.BuildFile(extractedSchemaFile)
		qt.Assert(t, qt.IsNil(extractedSchemaValue.Err()))

		txfs, err := txtar.FS(t.Archive)
		if err != nil {
			for _, f := range t.Archive.Files {
				t.Logf("- %v", f.Name)
			}
		}
		qt.Assert(t, qt.IsNil(err))
		if _, err := fs.Stat(txfs, "datatest"); err != nil {
			return
		}
		dataTestInst := t.Instances("./datatest")[0]
		dataTestv := ctx.BuildInstance(dataTestInst)
		var tests map[string]*generateDataTest
		err = dataTestv.Decode(&tests)
		qt.Assert(t, qt.IsNil(err))
		cwd := t.Dir
		outert := t
		for _, testName := range slices.Sorted(maps.Keys(tests)) {
			dataTest := tests[testName]
			// TODO it would be nice to be able to run each data test as
			// its own subtest but that's not nicely compatible with the
			// way that cuetxtar works, because either we have one
			// "out" file per test, in which case we'll end up with orphan
			// out files every time a test name changes, or we have one
			// out file for all tests, but that's awkward to manage when the
			// user can run any subset of tests with cue test -run.
			// A better approach might be to avoid cuetxtar completely
			// in favor of something more akin to the external decoder tests,
			// but it's harder to update CUE than JSON, so for now we
			// just avoid running subtests.
			t.Run(testName, func(t *testing.T) {
				qt.Assert(t, qt.IsTrue(dataTest.Data.Exists()))
				qt.Assert(t, qt.IsNil(dataTest.Data.Validate(cue.Concrete(true))))
				rv := dataTest.Data.Unify(extractedSchemaValue)
				err := rv.Validate(cue.Concrete(true))
				w := outert.Writer(testName)
				if dataTest.Error {
					qt.Assert(t, qt.Not(qt.IsNil(err)))
					errStr := errors.Details(err, &errors.Config{
						Cwd:     cwd,
						ToSlash: true,
					})
					io.WriteString(w, errStr)
					return
				}
				qt.Assert(t, qt.IsNil(err))
			})
		}
	})
}

type generateDataTest struct {
	Data  cue.Value `json:"data"`
	Error bool      `json:"error"`
}
