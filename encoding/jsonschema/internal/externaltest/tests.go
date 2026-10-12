package externaltest

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
)

type Schema struct {
	location
	Description string             `json:"description"`
	Comment     string             `json:"comment,omitempty"`
	Schema      stdjson.RawMessage `json:"schema"`
	Skip        Skip               `json:"skip,omitempty"`
	Tests       []*Test            `json:"tests"`
}

type Test struct {
	location
	Description string             `json:"description"`
	Comment     string             `json:"comment,omitempty"`
	Data        stdjson.RawMessage `json:"data"`
	Valid       bool               `json:"valid"`
	Skip        Skip               `json:"skip,omitempty"`
}

// Skip records information about whether a given schema
// or test will be skipped when testing. If not present,
// the test will be expected to pass.
//
// Each key in the map represents the name of a point
// in the cuetdtest matrix, optionally followed
// by a "-$variant" suffix to indicate a test variant
// such as round-trip testing.
type Skip map[string]string

type location struct {
	// syntax returns the syntax of the whole file,
	// which is only parsed when a position is needed.
	syntax func() ast.Expr
	schema int
	test   int // -1 for the schema itself
}

func (loc location) Pos() token.Pos {
	// The syntax was parsed from JSON which decoded into [Schema] values,
	// so it is a list of structs, and the indices are in range.
	schemas, ok := loc.syntax().(*ast.ListLit)
	if !ok {
		return token.NoPos
	}
	schema := schemas.Elts[loc.schema]
	if loc.test < 0 {
		return schema.Pos()
	}
	for _, decl := range schema.(*ast.StructLit).Elts {
		field := decl.(*ast.Field)
		if name, _, _ := ast.LabelName(field.Label); name == "tests" {
			return field.Value.(*ast.ListLit).Elts[loc.test].Pos()
		}
	}
	return token.NoPos
}

// FormatTestDir returns the test data files that [WriteTestDir] would write,
// keyed by their path under dir.
func FormatTestDir(dir string, tests map[string][]*Schema) (map[string][]byte, error) {
	files := make(map[string][]byte, len(tests))
	for filename, schemas := range tests {
		data, err := stdjson.MarshalIndent(schemas, "", "\t")
		if err != nil {
			return nil, err
		}
		files[filepath.Join(dir, filename)] = append(data, '\n')
	}
	return files, nil
}

// WriteTestDir writes test data files as read by ReadTestDir
// to the given directory. The keys of tests are filenames relative
// to dir.
func WriteTestDir(dir string, tests map[string][]*Schema) error {
	files, err := FormatTestDir(dir, tests)
	if err != nil {
		return err
	}
	for filename, data := range files {
		oldData, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if bytes.Equal(oldData, data) {
			continue
		}
		err = os.WriteFile(filename, data, 0o666)
		if err != nil {
			return err
		}
	}
	return nil
}

var ErrNotFound = fmt.Errorf("no external JSON schema tests found")

// ReadTestDir reads all the external tests from the given directory.
func ReadTestDir(dir string) (tests map[string][]*Schema, err error) {
	if _, err := os.Stat(filepath.Join(dir, "tests")); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	tests = make(map[string][]*Schema)
	fsys := os.DirFS(dir)
	err = fs.WalkDir(fsys, "tests", func(filename string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(filename, ".json") {
			return nil
		}
		data, err := fs.ReadFile(fsys, filename)
		if err != nil {
			return err
		}
		var schemas []*Schema
		if err := stdjson.Unmarshal(data, &schemas); err != nil {
			return fmt.Errorf("%s: %v", filename, err)
		}
		syntax := sync.OnceValue(func() ast.Expr {
			expr, _ := json.Extract(filename, data)
			return expr
		})
		for i, schema := range schemas {
			if schema.Schema == nil {
				return fmt.Errorf("%s: schema %d lacks schema", filename, i)
			}
			schema.location = location{syntax: syntax, schema: i, test: -1}
			schema.Schema = compact(schema.Schema)
			for j, test := range schema.Tests {
				if test.Data == nil {
					return fmt.Errorf("%s: schema %d test %d lacks data", filename, i, j)
				}
				test.location = location{syntax: syntax, schema: i, test: j}
				test.Data = compact(test.Data)
			}
		}
		tests[filename] = schemas
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tests, nil
}

// compact removes insignificant space from the JSON data,
// so that the test data's layout does not affect positions or formatting.
// The data must be valid JSON, as validated by [stdjson.Unmarshal].
func compact(data stdjson.RawMessage) stdjson.RawMessage {
	var buf bytes.Buffer
	if err := stdjson.Compact(&buf, data); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
