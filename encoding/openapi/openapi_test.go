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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal/cuetest"
)

func TestParseDefinitions(t *testing.T) {
	info := struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	}{"test", "v1"}
	defaultConfig := &openapi.Config{}
	resolveRefs := &openapi.Config{Info: info, ExpandReferences: true}

	testCases := []struct {
		in, out string
		variant string
		config  *openapi.Config
		err     string
	}{{
		in:     "structural.cue",
		out:    "structural.json",
		config: resolveRefs,
	}, {
		in:  "protobuf.cue",
		out: "protobuf.json",
		config: &openapi.Config{
			Info:             info,
			ExpandReferences: true,
		},
	}, {
		in:     "nested.cue",
		out:    "nested.json",
		config: defaultConfig,
	}, {
		in:     "simple.cue",
		out:    "simple.json",
		config: resolveRefs,
	}, {
		in:     "simple.cue",
		out:    "simple-filter.json",
		config: &openapi.Config{Info: info, FieldFilter: "min.*|max.*"},
	}, {
		in:     "array.cue",
		out:    "array.json",
		config: defaultConfig,
	}, {
		in:     "enum.cue",
		out:    "enum.json",
		config: defaultConfig,
	}, {
		in:     "struct.cue",
		out:    "struct.json",
		config: defaultConfig,
	}, {
		in:     "strings.cue",
		out:    "strings.json",
		config: defaultConfig,
	}, {
		in:     "nums.cue",
		out:    "nums.json",
		config: defaultConfig,
	}, {
		in:     "nums.cue",
		out:    "nums-v3.1.0.json",
		config: &openapi.Config{Info: info, Version: "3.1.0"},
	}, {
		in:     "builtins.cue",
		out:    "builtins.json",
		config: defaultConfig,
	}, {
		in:     "oneof.cue",
		out:    "oneof.json",
		config: defaultConfig,
	}, {
		in:     "oneof.cue",
		out:    "oneof-resolve.json",
		config: resolveRefs,
	}, {
		in:     "openapi.cue",
		out:    "openapi.json",
		config: defaultConfig,
	}, {
		in:     "openapi.cue",
		out:    "openapi-norefs.json",
		config: resolveRefs,
	}, {
		in:     "embed.cue",
		out:    "embed.json",
		config: defaultConfig,
	}, {
		in:     "embed.cue",
		out:    "embed-norefs.json",
		config: resolveRefs,
	}, {
		in:  "oneof.cue",
		out: "oneof-funcs.json",
		config: &openapi.Config{
			Info: info,
			NameFunc: func(v cue.Value, path cue.Path) string {
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
			DescriptionFunc: func(v cue.Value) string {
				return "Randomly picked description from a set of size one."
			},
		},
	}, {
		in:  "refs.cue",
		out: "refs.json",
		config: &openapi.Config{
			Info: info,
			NameFunc: func(v cue.Value, path cue.Path) string {
				switch {
				case strings.HasPrefix(path.Selectors()[0].String(), "#Excluded"):
					return ""
				}
				return strings.TrimPrefix(path.String(), "#")
			},
		},
	}, {
		in:     "issue131.cue",
		out:    "issue131.json",
		config: &openapi.Config{Info: info, SelfContained: true},
	}, {
		// Issue #915
		in:     "cycle.cue",
		out:    "cycle.json",
		config: &openapi.Config{Info: info},
	}, {
		// Issue #915
		in:     "cycle.cue",
		config: &openapi.Config{Info: info, ExpandReferences: true},
		err:    "cycle",
	}, {
		in:     "quotedfield.cue",
		out:    "quotedfield.json",
		config: defaultConfig,
	}, {
		in:     "omitvalue.cue",
		out:    "omitvalue.json",
		config: defaultConfig,
	}}
	for _, tc := range testCases {
		t.Run(tc.out+tc.variant, func(t *testing.T) {
			run := func(t *testing.T, inst cue.InstanceOrValue) {
				b, err := openapi.Gen(inst, tc.config)
				if err != nil {
					if tc.err == "" {
						t.Fatal("unexpected error:", errors.Details(err, nil))
					}
					return
				}

				if tc.err != "" {
					t.Fatal("unexpected success:", tc.err)
				} else {
					_, err := openapi.Generate(inst, tc.config)
					if err != nil {
						t.Fatal(err)
					}
				}

				var out = &bytes.Buffer{}
				_ = json.Indent(out, b, "", "   ")

				wantFile := filepath.Join("testdata", tc.out)
				if cuetest.UpdateGoldenFiles {
					_ = os.WriteFile(wantFile, out.Bytes(), 0666)
					return
				}

				b, err = os.ReadFile(wantFile)
				if err != nil {
					t.Fatal(err)
				}

				if d := cmp.Diff(string(b), out.String()); d != "" {
					t.Errorf("files differ:\n%v", d)
				}
			}
			filename := filepath.FromSlash(tc.in)
			inst := load.Instances([]string{filename}, &load.Config{
				Dir: "./testdata",
			})[0]
			ctx := cuecontext.New()
			v := ctx.BuildInstance(inst)
			if err := v.Err(); err != nil {
				t.Fatal(errors.Details(err, nil))
			}
			run(t, v)
		})
	}
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
