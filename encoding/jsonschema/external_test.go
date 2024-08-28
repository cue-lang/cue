package jsonschema_test

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
	"cuelang.org/go/internal/cuetest"
)

// TestExternal runs the externally defined JSON Schema test suite,
// as defined in https://github.com/json-schema-org/JSON-Schema-Test-Suite.
func TestExternal(t *testing.T) {
	tests, err := externaltest.ReadTestDir("testdata/external")
	qt.Assert(t, qt.IsNil(err))

	t.Logf("read %d test files", len(tests))
	// Group the tests under a single subtest so that we can use
	// t.Parallel and still guarantee that all tests have completed
	// by the end.
	t.Run("tests", func(t *testing.T) {
		// Run tests in deterministic order so we get some consistency between runs.
		for _, filename := range sortedKeys(tests) {
			schemas := tests[filename]
			t.Run(strings.ReplaceAll(filename, "/", "__"), func(t *testing.T) {
				for _, s := range schemas {
					t.Run(strings.ReplaceAll(s.Description, "/", "__"), func(t *testing.T) {
						runExternalSchemaTests(t, filename, s)
					})
				}
			})
		}
	})
	if !cuetest.UpdateGoldenFiles {
		return
	}
	for filename, schemas := range tests {
		filename = filepath.Join("testdata/external", filename)
		data, err := stdjson.MarshalIndent(schemas, "", "\t")
		qt.Assert(t, qt.IsNil(err))
		data = append(data, '\n')
		oldData, err := os.ReadFile(filename)
		qt.Assert(t, qt.IsNil(err))
		if bytes.Equal(oldData, data) {
			continue
		}
		err = os.WriteFile(filename, data, 0o666)
		qt.Assert(t, qt.IsNil(err))
	}
}

func runExternalSchemaTests(t *testing.T, filename string, s *externaltest.Schema) {
	t.Logf("file %v", path.Join("testdata/external", filename))
	ctx := cuecontext.New()
	jsonSchemaAST, err := json.Extract("schema.json", s.Schema)
	qt.Assert(t, qt.IsNil(err))
	jsonSchemav := ctx.BuildExpr(jsonSchemaAST)
	qt.Assert(t, qt.IsNil(jsonSchemav.Err()))
	versStr, _, _ := strings.Cut(strings.TrimPrefix(filename, "tests/"), "/")
	vers, ok := extVersionToVersion[versStr]
	if !ok {
		t.Fatalf("unknown JSON schema version for file %q", filename)
	}
	if vers == jsonschema.VersionUnknown {
		t.Skipf("skipping test for unknown schema version %v", versStr)
	}
	schemaAST, extractErr := jsonschema.Extract(jsonSchemav, &jsonschema.Config{
		Strict:         true,
		DefaultVersion: vers,
	})
	var schemav cue.Value
	if extractErr == nil {
		// Round-trip via bytes because that's what will usually happen
		// to the generated schema.
		b, err := format.Node(schemaAST, format.Simplify())
		qt.Assert(t, qt.IsNil(err))
		schemav = ctx.CompileBytes(b, cue.Filename("generated.cue"))
		if err := schemav.Err(); err != nil {
			extractErr = fmt.Errorf("cannot compile resulting schema: %v", errors.Details(err, nil))
		}
	}

	if extractErr != nil {
		if cuetest.UpdateGoldenFiles {
			s.Skip = fmt.Sprintf("extract error: %v", extractErr)
			for _, t := range s.Tests {
				t.Skip = "could not compile schema"
			}
			return
		}
		if s.Skip != "" {
			t.SkipNow()
		}
		t.Fatalf("extract error: %v", extractErr)
	}
	if s.Skip != "" {
		t.Errorf("unexpected test success on skipped test")
	}

	for _, test := range s.Tests {
		t.Run("", func(t *testing.T) {
			t.Logf("description: %s", test.Description)
			instAST, err := json.Extract("instance.json", test.Data)
			if err != nil {
				t.Fatal(err)
			}

			qt.Assert(t, qt.IsNil(err), qt.Commentf("test data: %q; details: %v", test.Data, errors.Details(err, nil)))

			instv := ctx.BuildExpr(instAST)
			qt.Assert(t, qt.IsNil(instv.Err()))
			err = instv.Unify(schemav).Err()
			if test.Valid {
				if cuetest.UpdateGoldenFiles {
					if err == nil {
						test.Skip = ""
					} else {
						test.Skip = errors.Details(err, nil)
					}
					return
				}
				if err != nil {
					if test.Skip != "" {
						t.SkipNow()
					}
					t.Fatalf("error: %v", errors.Details(err, nil))
				} else if test.Skip != "" {
					t.Error("unexpectedly more correct behavior (test success) on skipped test")
				}
			} else {
				if cuetest.UpdateGoldenFiles {
					if err != nil {
						test.Skip = ""
					} else {
						test.Skip = "unexpected success"
					}
					return
				}
				if err == nil {
					if test.Skip != "" {
						t.SkipNow()
					}
					t.Fatal("unexpected success")
				} else if test.Skip != "" {
					t.Error("unexpectedly more correct behavior (test failure) on skipped test")
				}
			}
		})
	}
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
