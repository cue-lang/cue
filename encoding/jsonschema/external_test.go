// Copyright 2024 CUE Authors
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
	stdjson "encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
)

// Pull in the external test data.
// The commit below references the JSON schema test main branch as of Sun May 19 19:01:03 2024 +0300

//go:generate go run vendor_external.go -- 9fc880bfb6d8ccd093bc82431f17d13681ffae8e
//go:generate go run teststats.go -o external_teststats.txt

const testDir = "testdata/external"

// TestExternal runs the externally defined JSON Schema test suite,
// as defined in https://github.com/json-schema-org/JSON-Schema-Test-Suite.
func TestExternal(t *testing.T) {
	tests, err := externaltest.ReadTestDir(testDir)
	qt.Assert(t, qt.IsNil(err))

	// Group the tests under a single subtest so that we can use
	// t.Parallel and still guarantee that all tests have completed
	// by the end.
	cuetdtest.SmallMatrix.Run(t, "tests", func(t *testing.T, m *cuetdtest.M) {
		// Run tests in deterministic order so we get some consistency between runs.
		for _, filename := range sortedKeys(tests) {
			schemas := tests[filename]
			t.Run(testName(filename), func(t *testing.T) {
				for _, s := range schemas {
					t.Run(testName(s.Description), func(t *testing.T) {
						runExternalSchemaTests(t, m, filename, s)
					})
				}
			})
		}
	})
	if !cuetest.UpdateGoldenFiles {
		return
	}
	if t.Failed() {
		t.Fatalf("not writing test data back because of test failures")
	}
	err = externaltest.WriteTestDir(testDir, tests)
	qt.Assert(t, qt.IsNil(err))
}

func runExternalSchemaTests(t *testing.T, m *cuetdtest.M, filename string, s *externaltest.Schema) {
	t.Logf("file %v", path.Join("testdata/external", filename))
	ctx := m.CueContext()
	jsonAST, err := json.Extract("schema.json", s.Schema)
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
	var schemaValue cue.Value
	if extractErr == nil {
		// Round-trip via bytes because that's what will usually happen
		// to the generated schema.
		b, err := format.Node(schemaAST, format.Simplify())
		qt.Assert(t, qt.IsNil(err))
		schemaValue = ctx.CompileBytes(b, cue.Filename("generated.cue"))
		if err := schemaValue.Err(); err != nil {
			t.Logf("extracted schema: %q", b)
			extractErr = fmt.Errorf("cannot compile resulting schema: %v", errors.Details(err, nil))
		}
	}

	if extractErr != nil {
		t.Logf("location: %v", testdataPos(s))
		t.Logf("txtar:\n%s", schemaFailureTxtar(s))
		for _, test := range s.Tests {
			t.Run("", func(t *testing.T) {
				testFailed(t, m, &test.Skip, test, "could not compile schema")
			})
		}
		testFailed(t, m, &s.Skip, s, fmt.Sprintf("extract error: %v", extractErr))
		return
	}
	testSucceeded(t, m, &s.Skip, s)

	for _, test := range s.Tests {
		t.Run(testName(test.Description), func(t *testing.T) {
			defer func() {
				if t.Failed() || testing.Verbose() {
					t.Logf("txtar:\n%s", testCaseTxtar(s, test))
				}
			}()
			t.Logf("location: %v", testdataPos(test))
			instAST, err := json.Extract("instance.json", test.Data)
			if err != nil {
				t.Fatal(err)
			}

			qt.Assert(t, qt.IsNil(err), qt.Commentf("test data: %q; details: %v", test.Data, errors.Details(err, nil)))

			instValue := ctx.BuildExpr(instAST)
			qt.Assert(t, qt.IsNil(instValue.Err()))
			err = instValue.Unify(schemaValue).Validate(cue.Concrete(true))
			if test.Valid {
				if err != nil {
					testFailed(t, m, &test.Skip, test, errors.Details(err, nil))
				} else {
					testSucceeded(t, m, &test.Skip, test)
				}
			} else {
				if err == nil {
					testFailed(t, m, &test.Skip, test, "unexpected success")
				} else {
					testSucceeded(t, m, &test.Skip, test)
				}
			}
		})
	}
}

// testCaseTxtar returns a testscript that runs the given test.
func testCaseTxtar(s *externaltest.Schema, test *externaltest.Test) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "env CUE_EXPERIMENT=evalv3\n")
	fmt.Fprintf(&buf, "exec cue def json+jsonschema: schema.json\n")
	if !test.Valid {
		buf.WriteString("! ")
	}
	// TODO add $schema when one isn't already present?
	fmt.Fprintf(&buf, "exec cue vet -c instance.json json+jsonschema: schema.json\n")
	fmt.Fprintf(&buf, "\n")
	fmt.Fprintf(&buf, "-- schema.json --\n%s\n", indentJSON(s.Schema))
	fmt.Fprintf(&buf, "-- instance.json --\n%s\n", indentJSON(test.Data))
	return buf.String()
}

// testCaseTxtar returns a testscript that decodes the given schema.
func schemaFailureTxtar(s *externaltest.Schema) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "env CUE_EXPERIMENT=evalv3\n")
	fmt.Fprintf(&buf, "exec cue def -o schema.cue json+jsonschema: schema.json\n")
	fmt.Fprintf(&buf, "exec cat schema.cue\n")
	fmt.Fprintf(&buf, "exec cue vet schema.cue\n")
	fmt.Fprintf(&buf, "-- schema.json --\n%s\n", indentJSON(s.Schema))
	return buf.String()
}

func indentJSON(x stdjson.RawMessage) []byte {
	data, err := stdjson.MarshalIndent(x, "", "\t")
	if err != nil {
		panic(err)
	}
	return data
}

type positioner interface {
	Pos() token.Pos
}

// testName returns a test name that doesn't contain any
// slashes because slashes muck with matching.
func testName(s string) string {
	return strings.ReplaceAll(s, "/", "__")
}

// testFailed marks the current test as failed with the
// given error message, and updates the
// skip field pointed to by skipField if necessary.
func testFailed(t *testing.T, m *cuetdtest.M, skipField *externaltest.Skip, p positioner, errStr string) {
	if cuetest.UpdateGoldenFiles {
		if *skipField == nil && !allowRegressions() {
			t.Fatalf("test regression; was succeeding, now failing: %v", errStr)
		}
		if *skipField == nil {
			*skipField = make(externaltest.Skip)
		}
		(*skipField)[m.Name()] = errStr
		return
	}
	if reason := (*skipField)[m.Name()]; reason != "" {
		t.Skipf("skipping due to known error: %v", reason)
	}
	t.Fatal(errStr)
}

// testFails marks the current test as succeeded and updates the
// skip field pointed to by skipField if necessary.
func testSucceeded(t *testing.T, m *cuetdtest.M, skipField *externaltest.Skip, p positioner) {
	if cuetest.UpdateGoldenFiles {
		delete(*skipField, m.Name())
		if len(*skipField) == 0 {
			*skipField = nil
		}
		return
	}
	if reason := (*skipField)[m.Name()]; reason != "" {
		t.Fatalf("unexpectedly more correct behavior (test success) on skipped test")
	}
}

func testdataPos(p positioner) token.Position {
	pp := p.Pos().Position()
	pp.Filename = path.Join(testDir, pp.Filename)
	return pp
}

func allowRegressions() bool {
	return os.Getenv("CUE_ALLOW_REGRESSIONS") != ""
}

var extVersionToVersion = map[string]jsonschema.Version{
	"draft3":       jsonschema.VersionUnknown,
	"draft4":       jsonschema.VersionDraft4,
	"draft6":       jsonschema.VersionDraft6,
	"draft7":       jsonschema.VersionDraft7,
	"draft2019-09": jsonschema.VersionDraft2019_09,
	"draft2020-12": jsonschema.VersionDraft2020_12,
	"draft-next":   jsonschema.VersionUnknown,
}

func sortedKeys[T any](m map[string]T) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
