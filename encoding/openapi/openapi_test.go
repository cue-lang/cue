// Copyright 2019 CUE Authors
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

package openapi_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
)

var (
	matrix = cuetdtest.FullMatrix
)

func TestGenerateOpenAPI(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   t.Name(),
		Matrix: matrix,
	}

	nameFuncs := map[string]func(v cue.Value, path cue.Path) string{
		"toUpper": func(v cue.Value, path cue.Path) string {
			var buf strings.Builder
			for i, sel := range path.Selectors() {
				if i > 0 {
					buf.WriteByte('_')
				}
				s := sel.String()
				s = strings.TrimPrefix(s, "#")
				buf.WriteString(strings.ToUpper(s))
			}
			return buf.String()
		},
		"excludeExcluded": func(v cue.Value, path cue.Path) string {
			switch {
			case strings.HasPrefix(path.Selectors()[0].String(), "#Excluded"):
				return ""
			}
			return strings.TrimPrefix(path.String(), "#")
		},
	}

	descFuncs := map[string]func(v cue.Value) string{
		"randomish": func(v cue.Value) string {
			return "Randomly picked description from a set of size one."
		},
	}

	test.Run(t, func(t *cuetxtar.Test) {
		t.Parallel()
		a := t.Instance()
		ctx := t.CueContext()
		v := ctx.BuildInstance(a)

		if err := v.Err(); err != nil {
			t.Fatal(errors.Details(err, nil))
		}

		config := openapi.Config{}
		if t.HasTag("ExpandReferences") {
			config.ExpandReferences = true
		}
		if filter, ok := t.Value("FieldFilter"); ok {
			config.FieldFilter = filter
		}
		if version, ok := t.Value("Version"); ok {
			config.Version = version
		}
		if name, ok := t.Value("NameFunc"); ok {
			if fun, found := nameFuncs[name]; found {
				config.NameFunc = fun
			} else {
				t.Fatal("Unknown NameFunc", name)
			}
		}
		if desc, ok := t.Value("DescriptionFunc"); ok {
			if fun, found := descFuncs[desc]; found {
				config.DescriptionFunc = fun
			} else {
				t.Fatal("Unknown DescriptionFunc", desc)
			}
		}

		expectedErr, shouldErr := t.Value("ExpectError")
		f, err := openapi.Generate(v, &config)
		if err != nil {
			details := errors.Details(err, nil)
			if !shouldErr || !strings.Contains(details, expectedErr) {
				t.Fatal("unexpected error:", details)
			}
			return
		}
		if shouldErr {
			t.Fatal("unexpected success")
		} else {
			_, err := openapi.Gen(v, &config)
			if err != nil {
				t.Fatal(err)
			}
		}
		gen := ctx.BuildFile(f)
		qt.Assert(t, qt.IsNil(gen.Err()))
		var out bytes.Buffer
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "   ")
		err = enc.Encode(gen)
		qt.Assert(t, qt.IsNil(err))
		_, err = t.Writer("out.json").Write(out.Bytes())
		qt.Assert(t, qt.IsNil(err))

		// Check that we can extract the resulting schema without error.
		_, err = openapi.Extract(gen, &config)
		if expectedErr, shouldErr := t.Value("ExpectExtractError"); shouldErr {
			qt.Assert(t, qt.ErrorMatches(err, expectedErr))
			return
		}
		// TODO check that the resulting schema actually validates some
		// data values as expected.
		qt.Assert(t, qt.IsNil(err))
	})
}

// TODO: move OpenAPI testing to txtar and allow errors.
func TestIssue1234(t *testing.T) {
	val := cuecontext.New().CompileString(`
#Test: or([])

	`)
	if err := val.Err(); err != nil {
		t.Fatal(err)
	}

	_, err := openapi.Gen(val, &openapi.Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// This is for debugging purposes. Do not remove.
func TestX(t *testing.T) {
	t.Skip()

	val := cuecontext.New().CompileString(`
	`)
	if err := val.Err(); err != nil {
		t.Fatal(err)
	}

	b, err := openapi.Gen(val, &openapi.Config{
		// ExpandReferences: true,
	})
	if err != nil {
		t.Fatal(errors.Details(err, nil))
	}

	var out = &bytes.Buffer{}
	_ = json.Indent(out, b, "", "   ")
	t.Error(out.String())
}
