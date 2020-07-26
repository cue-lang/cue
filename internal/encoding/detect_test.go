// Copyright 2020 CUE Authors
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

package encoding

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
)

func TestDetect(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		out  build.Interpretation
	}{{
		name: "validOpenAPI",
		in: `
		openapi: "3.0.0"
		info: title: "Foo"
		info: version: "v1alpha1"
		`,
		out: build.OpenAPI,
	}, {
		name: "noOpenAPI",
		in: `
		info: title: "Foo"
		info: version: "v1alpha1"
		`,
	}, {
		name: "noTitle",
		in: `
		openapi: "3.0.0"
		info: version: "v1alpha1"
		`,
	}, {
		name: "noVersion",
		in: `
		openapi: "3.0.0"
		info: title: "Foo"
		`,
	}, {
		name: "validJSONSchema",
		in: `
		$schema: "https://json-schema.org/schema#"
		`,
		out: build.JSONSchema,
	}, {
		name: "validJSONSchema",
		in: `
		$schema: "https://json-schema.org/draft-07/schema#"
		`,
		out: build.JSONSchema,
	}, {
		name: "noSchema",
		in: `
		$id: "https://acme.com/schema#"
		`,
	}, {
		name: "wrongHost",
		in: `
		$schema: "https://acme.com/schema#"
		`,
	}, {
		name: "invalidURL",
		in: `
		$schema: "://json-schema.org/draft-07"
		`,
	}, {
		name: "invalidPath",
		in: `
		$schema: "https://json-schema.org/draft-07"
		`,
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var r cue.Runtime
			inst, err := r.Compile(tc.name, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			got := Detect(inst.Value())
			if got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
		})
	}
}
