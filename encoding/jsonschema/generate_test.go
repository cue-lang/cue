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
	"fmt"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
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
		version := jsonschema.VersionDraft2020_12
		if s, ok := t.Value("version"); ok {
			version = testVersion(t, s)
		}
		r, err := jsonschema.Generate(v, &jsonschema.GenerateConfig{
			Version:      version,
			ExplicitOpen: t.HasTag("explicitOpen"),
		})
		qt.Assert(t, qt.IsNil(err))
		data, err := format.Node(r)
		qt.Assert(t, qt.IsNil(err))
		t.Writer("schema").Write(data)
		t.Logf("generated schema: %q", data)

		// Round-trip test: convert generated JSON Schema back to CUE to validate.
		schemaValue := ctx.BuildExpr(r)
		qt.Assert(t, qt.IsNil(schemaValue.Err()), qt.Commentf("schema data: %q", data))

		schemaBytes, err := schemaValue.MarshalJSON()
		qt.Assert(t, qt.IsNil(err))

		schemaValue = ctx.CompileBytes(schemaBytes)
		qt.Assert(t, qt.IsNil(schemaValue.Err()))

		extractCfg := &jsonschema.Config{
			StrictFeatures: true,
			StrictKeywords: true,
			DefaultVersion: version,
		}
		if version == jsonschema.VersionOpenAPI {
			// The generated OpenAPI output is a document fragment rather
			// than a pure JSON Schema: it carries a components section
			// holding the shared schemas (referenced via
			// #/components/schemas), which is not a JSON Schema keyword.
			// Relax StrictKeywords so that extraction treats the root
			// object as the entry schema while still resolving the
			// in-document references.
			extractCfg.StrictKeywords = false
		}
		extractedSchemaFile, err := jsonschema.Extract(schemaValue, extractCfg)
		if t.HasTag("brokenRoundTrip") {
			t.Skipf("round-trip extraction skipped (brokenRoundTrip)")
			return
		}
		qt.Assert(t, qt.IsNil(err), qt.Commentf("generated JSON Schema should round-trip cleanly via Extract"))
		extractedSchemaValue := ctx.BuildFile(extractedSchemaFile)
		t.Logf("extracted schema: %#v", extractedSchemaValue)
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

func TestGenerateMany(t *testing.T) {
	ctx := cuecontext.New()
	const src = `
#Address: {street: string, city: string}
#Person: {name: string, home: #Address, work: #Address}
#Company: {name: string, address: #Address}
`
	root := ctx.CompileString(src)
	qt.Assert(t, qt.IsNil(root.Err()))

	person := root.LookupPath(cue.ParsePath("#Person"))
	company := root.LookupPath(cue.ParsePath("#Company"))

	exprs, shared, err := jsonschema.GenerateMany(
		[]cue.Value{person, company},
		"#/components/schemas",
		&jsonschema.GenerateConfig{Version: jsonschema.VersionOpenAPI},
	)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.HasLen(exprs, 2))

	// The single shared definition (#Address, referenced by both roots)
	// is deduplicated into one map entry.
	qt.Assert(t, qt.DeepEquals(slices.Sorted(maps.Keys(shared)), []string{"Address"}))

	// Both roots reference the shared definition rather than inlining it,
	// and the reference is rooted at the requested sharedSchemaRoot.
	var buf strings.Builder
	for i, e := range exprs {
		fmt.Fprintf(&buf, "\n-- expr-%d --\n", i)
		fmt.Fprintf(&buf, "%s", formatNode(t, e))
	}
	qt.Assert(t, qt.Equals(buf.String(), `
-- expr-0 --
{
	type:                 "object"
	additionalProperties: false
	properties: {
		home: $ref: "#/components/schemas/Address"
		name: type: "string"
		work: $ref: "#/components/schemas/Address"
	}
	required: ["home", "name", "work"]
}
-- expr-1 --
{
	type:                 "object"
	additionalProperties: false
	properties: {
		address: $ref: "#/components/schemas/Address"
		name: type:    "string"
	}
	required: ["address", "name"]
}`))
}

func formatNode(t *testing.T, n ast.Node) string {
	data, err := format.Node(n)
	qt.Assert(t, qt.IsNil(err))
	return string(data)
}

type generateDataTest struct {
	Data  cue.Value `json:"data"`
	Error bool      `json:"error"`
}

// testVersion maps a short version name used in the #version test tag
// to a [jsonschema.Version].
func testVersion(t *cuetxtar.Test, s string) jsonschema.Version {
	switch s {
	case "2020-12":
		return jsonschema.VersionDraft2020_12
	case "draft7", "draft-07":
		return jsonschema.VersionDraft7
	case "openapi":
		return jsonschema.VersionOpenAPI
	}
	t.Fatalf("unknown #version tag %q", s)
	return jsonschema.VersionUnknown
}
