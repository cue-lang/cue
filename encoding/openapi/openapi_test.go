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

package openapi

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/load"
	"github.com/kylelemons/godebug/diff"
)

var update *bool = flag.Bool("update", false, "update the test output")

func TestParseDefinitions(t *testing.T) {
	info := OrderedMap{KeyValue{"title", "test"}, KeyValue{"version", "v1"}}
	defaultConfig := &Config{}
	resolveRefs := &Config{Info: info, ExpandReferences: true}

	testCases := []struct {
		in, out string
		config  *Config
	}{{
		"simple.cue",
		"simple.json",
		resolveRefs,
	}, {
		"array.cue",
		"array.json",
		defaultConfig,
	}, {
		"oneof.cue",
		"oneof.json",
		defaultConfig,
	}, {
		"openapi.cue",
		"openapi.json",
		defaultConfig,
	}, {
		"openapi.cue",
		"openapi-norefs.json",
		resolveRefs,
	}, {
		"oneof.cue",
		"oneof-funcs.json",
		&Generator{
			Info: info,
			ReferenceFunc: func(inst *cue.Instance, path []string) string {
				return strings.ToUpper(strings.Join(path, "_"))
			},
			DescriptionFunc: func(v cue.Value) string {
				return "Randomly picked description from a set of size one."
			},
		},
	}}
	for _, tc := range testCases {
		t.Run(tc.out, func(t *testing.T) {
			filename := filepath.Join("testdata", filepath.FromSlash(tc.in))

			inst := cue.Build(load.Instances([]string{filename}, nil))[0]

			b, err := Gen(inst, tc.config)
			var out = &bytes.Buffer{}
			_ = json.Indent(out, b, "", "   ")

			wantFile := filepath.Join("testdata", tc.out)
			if *update {
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
