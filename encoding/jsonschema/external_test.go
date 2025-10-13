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
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
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

const testDir = "testdata/external"

// TestExternal runs the externally defined JSON Schema test suite,
// as defined in https://github.com/json-schema-org/JSON-Schema-Test-Suite.
func TestExternal(t *testing.T) {
	t.Parallel()
	tests, err := externaltest.ReadTestDir(testDir)
	qt.Assert(t, qt.IsNil(err))

	// Group the tests under a single subtest so that we can use
	// t.Parallel and still guarantee that all tests have completed
	// by the end.
	cuetdtest.SmallMatrix.Run(t, "tests", func(t *testing.T, m *cuetdtest.M) {
		t.Parallel()
		// Run tests in deterministic order so we get some consistency between runs.
		for _, filename := range slices.Sorted(maps.Keys(tests)) {
			schemas := tests[filename]
			t.Run(testName(filename), func(t *testing.T) {
				t.Parallel()
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
		t.Fatalf("not writing test data back because of test failures (try CUE_UPDATE=force to proceed regardless of test regressions)")
	}
	err = externaltest.WriteTestDir(testDir, tests)
	qt.Assert(t, qt.IsNil(err))
	err = writeExternalTestStats(testDir, tests)
	qt.Assert(t, qt.IsNil(err))
}

var rxCharacterClassCategoryAlias = regexp.MustCompile(`\\p{(Cased_Letter|Close_Punctuation|Combining_Mark|Connector_Punctuation|Control|Currency_Symbol|Dash_Punctuation|Decimal_Number|Enclosing_Mark|Final_Punctuation|Format|Initial_Punctuation|Letter|Letter_Number|Line_Separator|Lowercase_Letter|Mark|Math_Symbol|Modifier_Letter|Modifier_Symbol|Nonspacing_Mark|Number|Open_Punctuation|Other|Other_Letter|Other_Number|Other_Punctuation|Other_Symbol|Paragraph_Separator|Private_Use|Punctuation|Separator|Space_Separator|Spacing_Mark|Surrogate|Symbol|Titlecase_Letter|Unassigned|Uppercase_Letter|cntrl|digit|punct)}`)

var supportsCharacterClassCategoryAlias = func() bool {
	_, err := regexp.Compile(`\p{Letter}`)
	return err == nil
}()

var fixesParsingIPv6HostWithoutBrackets = func() bool {
	// We use Sprintf so that staticcheck on Go 1.26 and later does not
	// helpfully report that this URL will always fail to parse.
	_, err := url.Parse(fmt.Sprintf("%s://2001:0db8:85a3:0000:0000:8a2e:0370:7334", "http"))
	return err != nil
}()

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
	maybeSkip(t, vers, versStr, s)
	t.Logf("location: %v", testdataPos(s))

	// Extract the schema from the test data JSON schema.
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
		t.Logf("extracted schema: %q", b)
		schemaValue = ctx.CompileBytes(b, cue.Filename("generated.cue"))
		if err := schemaValue.Err(); err != nil {
			extractErr = fmt.Errorf("cannot compile resulting schema: %v", errors.Details(err, nil))
		}
	}
	t.Run("Extract", func(t *testing.T) {
		if extractErr != nil {
			t.Logf("txtar:\n%s", schemaFailureTxtar(s))
			schemaExtractFailed(t, m, "", s, fmt.Sprintf("extract error: %v", extractErr))
			return
		}
		testSucceeded(t, m, "", &s.Skip, s)
		for _, test := range s.Tests {
			t.Run(testName(test.Description), func(t *testing.T) {
				runExternalSchemaTest(t, m, "", s, test, schemaValue)
			})
		}
	})

	t.Run("RoundTrip", func(t *testing.T) {
		// Run Generate round-trip tests for draft2020-12 only
		const supportedVersion = jsonschema.VersionDraft2020_12
		const variant = "roundtrip"
		var roundTripSchemaValue cue.Value
		var roundTripErr error
		switch {
		case extractErr != nil:
			roundTripErr = fmt.Errorf("inital extract failed")
		case vers != supportedVersion:
			// Generation only supports 2020-12 currently
			roundTripErr = fmt.Errorf("generation only supported in version %v", supportedVersion)
		default:
			roundTripSchemaValue, roundTripErr = roundTripViaGenerate(t, schemaValue)
		}
		if roundTripErr != nil {
			schemaExtractFailed(t, m, variant, s, roundTripErr.Error())
			return
		}
		testSucceeded(t, m, variant, &s.Skip, s)
		for _, test := range s.Tests {
			t.Run(testName(test.Description), func(t *testing.T) {
				runExternalSchemaTest(t, m, variant, s, test, roundTripSchemaValue)
			})
		}
	})
}

// schemaExtractFailed marks a schema extraction as failed and also
// runs all the subtests, marking them as failed too.
func schemaExtractFailed(t *testing.T, m *cuetdtest.M, variant string, s *externaltest.Schema, reason string) {
	for _, test := range s.Tests {
		t.Run("", func(t *testing.T) {
			testFailed(t, m, variant, &test.Skip, test, "could not extract schema")
		})
	}
	testFailed(t, m, variant, &s.Skip, s, reason)
}

func maybeSkip(t *testing.T, vers jsonschema.Version, versStr string, s *externaltest.Schema) {
	switch {
	case vers == jsonschema.VersionUnknown:
		t.Skipf("skipping test for unknown schema version %v", versStr)

	case rxCharacterClassCategoryAlias.Match(s.Schema) && !supportsCharacterClassCategoryAlias:
		// Go 1.25 implements Unicode category aliases in regular expressions,
		// and so e.g. \p{Letter} did not work on Go 1.24.x releases.
		// See: https://github.com/golang/go/issues/70780
		// Our tests must run on the latest two stable Go versions, currently 1.24 and 1.25,
		// where such character classes lead to schema compilation errors on 1.24.
		//
		// As a temporary compromise, only run these tests on Go 1.25 or later.
		// TODO: get rid of this whole thing once we require Go 1.25 or later in the future.
		t.Skip("regexp character classes for Unicode category aliases work only on Go 1.25 and later")

	case bytes.Contains(s.Schema, []byte(`"iri"`)) && fixesParsingIPv6HostWithoutBrackets:
		// Go 1.26 fixes [url.Parse] so that it correctly rejects IPv6 hosts
		// without the required surrounding square brackets.
		// See: https://github.com/golang/go/issues/31024
		// Our tests must run on the latest two stable Go versions, currently 1.24 and 1.25,
		// where such behavior is still buggy.
		//
		// As a temporary compromise, skip the test on 1.26 or later;
		// we care about testing the behavior that most CUE users will see today.
		// TODO: get rid of this whole thing once we require Go 1.26 or later in the future.
		t.Skip("net/url.Parse tightens behavior on IPv6 hosts on Go 1.26 and later")
	}
}

// runExternalSchemaTest runs a single test case against a given schema value.
func runExternalSchemaTest(t *testing.T, m *cuetdtest.M, variant string, s *externaltest.Schema, test *externaltest.Test, schemaValue cue.Value) {
	ctx := schemaValue.Context()
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
			testFailed(t, m, variant, &test.Skip, test, errors.Details(err, nil))
		} else {
			testSucceeded(t, m, variant, &test.Skip, test)
		}
	} else {
		if err == nil {
			testFailed(t, m, variant, &test.Skip, test, "unexpected success")
		} else {
			testSucceeded(t, m, variant, &test.Skip, test)
		}
	}
}

// roundTripViaGenerate takes a CUE schema as produced by Extract,
// invokes Generate on it, then returns the result of invoking Extract on
// the result of that.
func roundTripViaGenerate(t *testing.T, schemaValue cue.Value) (cue.Value, error) {
	t.Logf("round tripping from schema %#v", schemaValue)
	ctx := schemaValue.Context()
	// Generate JSON Schema from the extracted CUE
	jsonAST, err := jsonschema.Generate(schemaValue, &jsonschema.GenerateConfig{
		Version: jsonschema.VersionDraft2020_12,
	})
	if err != nil {
		return cue.Value{}, fmt.Errorf("generate error: %v", err)
	}
	jsonValue := ctx.BuildExpr(jsonAST)
	if err := jsonValue.Err(); err != nil {
		// This really shouldn't happen.
		return cue.Value{}, fmt.Errorf("cannot build value from JSON: %v", err)
	}
	t.Logf("generated JSON schema: %v", jsonValue)

	generatedSchemaAST, err := jsonschema.Extract(jsonValue, &jsonschema.Config{
		StrictFeatures: true,
		DefaultVersion: jsonschema.VersionDraft2020_12,
	})
	if err != nil {
		return cue.Value{}, fmt.Errorf("cannot extract generated schema: %v", err)
	}
	schemaValue1 := ctx.BuildFile(generatedSchemaAST)
	if err := schemaValue1.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("cannot build extracted schema: %v", err)
	}
	return schemaValue1, nil
}

// testCaseTxtar returns a testscript that runs the given test.
func testCaseTxtar(s *externaltest.Schema, test *externaltest.Test) string {
	var buf strings.Builder
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
func testFailed(t *testing.T, m *cuetdtest.M, variant string, skipField *externaltest.Skip, p positioner, errStr string) {
	name := skipName(m, variant)
	if cuetest.UpdateGoldenFiles {
		if (*skipField)[name] == "" && !cuetest.ForceUpdateGoldenFiles {
			t.Fatalf("test regression; was succeeding, now failing: %v", errStr)
		}
		if *skipField == nil {
			*skipField = make(externaltest.Skip)
		}
		(*skipField)[name] = errStr
		return
	}
	if reason := (*skipField)[name]; reason != "" {
		qt.Assert(t, qt.Equals(reason, errStr), qt.Commentf("error message mismatch"))
		t.Skipf("skipping due to known error: %v", reason)
	}
	t.Fatal(errStr)
}

// testFails marks the current test as succeeded and updates the
// skip field pointed to by skipField if necessary.
func testSucceeded(t *testing.T, m *cuetdtest.M, variant string, skipField *externaltest.Skip, p positioner) {
	name := skipName(m, variant)
	if cuetest.UpdateGoldenFiles {
		delete(*skipField, name)
		if len(*skipField) == 0 {
			*skipField = nil
		}
		return
	}
	if reason := (*skipField)[name]; reason != "" {
		t.Fatalf("unexpectedly more correct behavior (test success) on skipped test")
	}
}

// skipName returns the key to use in the skip field for the
// given matrix entry and test variant.
func skipName(m *cuetdtest.M, variant string) string {
	name := m.Name()
	if variant != "" {
		name += "-" + variant
	}
	return name
}

func testdataPos(p positioner) token.Position {
	pp := p.Pos().Position()
	pp.Filename = path.Join(testDir, pp.Filename)
	return pp
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

func writeExternalTestStats(testDir string, tests map[string][]*externaltest.Schema) error {
	outf, err := os.Create("external_teststats.txt")
	if err != nil {
		return err
	}
	defer outf.Close()
	fmt.Fprintf(outf, "# Generated by CUE_UPDATE=1 go test. DO NOT EDIT\n")
	variants := []string{
		"v3",
		"v3-roundtrip",
	}
	for _, opt := range []string{"Core", "Optional"} {
		fmt.Fprintf(outf, "\n%s tests:\n", opt)
		for _, v := range variants {
			fmt.Fprintf(outf, "\n%s:\n", v)
			showStats(outf, v, opt == "Optional", tests)
		}
	}
	return nil
}

func showStats(outw io.Writer, version string, showOptional bool, tests map[string][]*externaltest.Schema) {
	schemaOK := 0
	schemaTot := 0
	testOK := 0
	testTot := 0
	schemaOKTestOK := 0
	schemaOKTestTot := 0
	for filename, schemas := range tests {
		isOptional := strings.Contains(filename, "/optional/")
		if isOptional != showOptional {
			continue
		}
		for _, schema := range schemas {
			schemaTot++
			schemaSkipped := schema.Skip[version] != ""
			if !schemaSkipped {
				schemaOK++
			}
			for _, test := range schema.Tests {
				testSkipped := test.Skip[version] != ""
				testTot++
				if !testSkipped {
					testOK++
				}
				if !schemaSkipped {
					schemaOKTestTot++
					if !testSkipped {
						schemaOKTestOK++
					}
				}
			}
		}
	}
	fmt.Fprintf(outw, "\tschema extract (pass / total): %d / %d = %.1f%%\n", schemaOK, schemaTot, percent(schemaOK, schemaTot))
	fmt.Fprintf(outw, "\ttests (pass / total): %d / %d = %.1f%%\n", testOK, testTot, percent(testOK, testTot))
	fmt.Fprintf(outw, "\ttests on extracted schemas (pass / total): %d / %d = %.1f%%\n", schemaOKTestOK, schemaOKTestTot, percent(schemaOKTestOK, schemaOKTestTot))
}

func percent(a, b int) float64 {
	return (float64(a) / float64(b)) * 100.0
}
