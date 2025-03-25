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

package jsonschema_test

import (
	"path"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal/astinternal"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

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

// This is a test for debugging external tests.
func TestExtX(t *testing.T) {
	t.Skip()

	version := cuecontext.EvalDefault
	// version = cuecontext.EvalExperiment
	ctx := cuecontext.New(cuecontext.EvaluatorVersion(version))

	const filename = `tests/draft4/optional/ecmascript-regexp.json`
	const jsonSchema = `
		{
			"type": "object",
			"patternProperties": {
				"\\wcole": {}
			},
			"additionalProperties": false
		}
	`
	const testData = `
		{
			"l'école": "pas de vraie vie"
		}
	`

	jsonAST, err := json.Extract("schema.json", []byte(jsonSchema))
	qt.Assert(t, qt.IsNil(err))
	jsonValue := ctx.BuildExpr(jsonAST)
	qt.Assert(t, qt.IsNil(jsonValue.Err()))
	versStr, _, _ := strings.Cut(strings.TrimPrefix(filename, "tests/"), "/")
	vers, ok := extVersionToVersion[versStr]
	if !ok {
		t.Fatalf("unknown JSON schema version for file %q", filename)
	}
	if vers == jsonschema.VersionUnknown {
		t.Skipf("skipping test for unknown schema version %v", versStr)
	}
	schemaAST, extractErr := jsonschema.Extract(jsonValue, &jsonschema.Config{
		StrictFeatures: true,
		DefaultVersion: vers,
	})
	qt.Assert(t, qt.IsNil(extractErr))
	b, err := format.Node(schemaAST, format.Simplify())
	qt.Assert(t, qt.IsNil(err))
	t.Logf("SCHEMA: %v", string(b))
	schemaValue := ctx.CompileBytes(b, cue.Filename("generated.cue"))
	if err := schemaValue.Err(); err != nil {
		t.Fatalf("cannot compile resulting schema: %v", errors.Details(err, nil))
	}

	instAST, err := json.Extract("instance.json", []byte(testData))
	if err != nil {
		t.Fatal(err)
	}

	qt.Assert(t, qt.IsNil(err), qt.Commentf("test data: %q; details: %v", testData, errors.Details(err, nil)))

	instValue := ctx.BuildExpr(instAST)
	t.Log("VALUE", instValue)
	qt.Assert(t, qt.IsNil(instValue.Err()))
	err = instValue.Unify(schemaValue).Validate(cue.Concrete(true))

	t.Error(errors.Details(err, nil))
}
