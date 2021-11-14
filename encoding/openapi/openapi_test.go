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
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/diff"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal/cuetest"
)

func TestParseDefinitions(t *testing.T) {
	info := *(*openapi.OrderedMap)(ast.NewStruct(
		"title", ast.NewString("test"),
		"version", ast.NewString("v1"),
	))
	defaultConfig := &openapi.Config{}
	resolveRefs := &openapi.Config{Info: info, ExpandReferences: true}

	testCases := []struct {
		in, out string
		config  *openapi.Config
		err     string
	}{{
		in:     "structural.cue",
		out:    "structural.json",
		config: resolveRefs,
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
			ReferenceFunc: func(inst *cue.Instance, path []string) string {
				return strings.ToUpper(strings.Join(path, "_"))
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
			ReferenceFunc: func(inst *cue.Instance, path []string) string {
				switch {
				case strings.HasPrefix(path[0], "Excluded"):
					return ""
				}
				return strings.Join(path, ".")
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
	}}
	for _, tc := range testCases {
		t.Run(tc.out, func(t *testing.T) {
			filename := filepath.FromSlash(tc.in)

			inst := cue.Build(load.Instances([]string{filename}, &load.Config{
				Dir: "./testdata",
			}))[0]
			if inst.Err != nil {
				t.Fatal(errors.Details(inst.Err, nil))
			}

			b, err := openapi.Gen(inst, tc.config)
			if err != nil {
				if tc.err == "" {
					t.Fatal("unexpected error:", errors.Details(inst.Err, nil))
				}
				return
			}

			if tc.err != "" {
				t.Fatal("unexpected success:", tc.err)
			} else {
				all, err := tc.config.All(inst)
				if err != nil {
					t.Fatal(err)
				}
				walk(all)
			}

			var out = &bytes.Buffer{}
			_ = json.Indent(out, b, "", "   ")

			wantFile := filepath.Join("testdata", tc.out)
			if cuetest.UpdateGoldenFiles {
				_ = ioutil.WriteFile(wantFile, out.Bytes(), 0644)
				return
			}

			b, err = ioutil.ReadFile(wantFile)
			if err != nil {
				t.Fatal(err)
			}

			if d := diff.Diff(string(b), out.String()); d != "" {
				t.Errorf("files differ:\n%v", d)
			}
		})
	}
}

// walk traverses an openapi.OrderedMap. This is a helper function
// used to ensure that a generated OpenAPI value is well-formed.
func walk(om *openapi.OrderedMap) {
	for _, p := range om.Pairs() {
		switch p := p.Value.(type) {
		case *openapi.OrderedMap:
			walk(p)
		case []*openapi.OrderedMap:
			for _, om := range p {
				walk(om)
			}
		}
	}
}

// TODO: move OpenAPI testing to txtar and allow errors.
func TestIssue1234(t *testing.T) {
	var r cue.Runtime
	inst, err := r.Compile("test", `
#Test: or([])

	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = openapi.Gen(inst, &openapi.Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// This is for debugging purposes. Do not remove.
func TestX(t *testing.T) {
	t.Skip()

	var r cue.Runtime
	inst, err := r.Compile("test", `
	`)
	if err != nil {
		t.Fatal(err)
	}

	b, err := openapi.Gen(inst, &openapi.Config{
		// ExpandReferences: true,
	})
	if err != nil {
		t.Fatal(errors.Details(err, nil))
	}

	var out = &bytes.Buffer{}
	_ = json.Indent(out, b, "", "   ")
	t.Error(out.String())
}
