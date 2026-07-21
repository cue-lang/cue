// Copyright 2026 CUE Authors
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

package yaml_test

import (
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/encoding/yaml"
)

func TestExtractLenient(t *testing.T) {
	testCases := []struct {
		name string
		yaml string
		want string
		// errs holds substrings of the joined error;
		// empty means no error is expected.
		errs []string
	}{{
		name: "valid",
		yaml: "a: 1\nb: two",
		want: `a: 1
b: "two"`,
	}, {
		name: "empty",
		yaml: "",
		want: "*null | _",
	}, {
		name: "new key being typed",
		yaml: "a: 1\nnewkey",
		want: "a: 1",
		errs: []string{"t.yaml:2: non-map value is specified"},
	}, {
		name: "new key being typed mid-document",
		yaml: "spec:\n  replicas: 3\n  tem\nkind: x",
		want: "spec: {\n\treplicas: 3\n}",
		errs: []string{"t.yaml:3: non-map value is specified"},
	}, {
		// The dangling key before the broken value survives as null.
		name: "unclosed double quote",
		yaml: "a: 1\nb: \"fo",
		want: "a: 1\nb: null",
		errs: []string{"t.yaml:2: could not find end character of double-quoted text"},
	}, {
		name: "unclosed flow sequence",
		yaml: "a: 1\nb: [1, 2",
		want: "a: 1\nb: null",
		errs: []string{"t.yaml:2: sequence end token ']' not found"},
	}, {
		name: "dangling anchor",
		yaml: "a: &",
		want: "a: null",
		errs: []string{"t.yaml:1: undefined anchor name"},
	}, {
		name: "bad dedent",
		yaml: "a:\n  b: 1\n c: 2\nd: 3",
		want: "a: {\n\tb: 1\n}",
		errs: []string{"t.yaml:3: value is not allowed in this context"},
	}, {
		// The documents before and after the broken one still decode.
		name: "broken document in stream",
		yaml: "a: 1\n---\nb: \"x\n---\nc: 3",
		want: "[\n\t{a: 1},\n\t{\n\n\t\tb: null\n\t},\n\t{\n\n\t\tc: 3\n\t}\n]",
		errs: []string{"t.yaml:4: found unexpected document separator"},
	}, {
		name: "extraction error skips document",
		yaml: "a: !!binary '!'\n---\nb: 2",
		want: "b: 2",
		errs: []string{"t.yaml:1: !!binary value contains invalid base64 data"},
	}, {
		// When no document yields any content, the result matches an
		// empty input, still alongside the error.
		name: "nothing parses",
		yaml: "@",
		want: "*null | _",
		errs: []string{"t.yaml:1: "},
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := yaml.ExtractLenient("t.yaml", []byte(tc.yaml))
			if f == nil {
				t.Fatal("ExtractLenient returned a nil file")
			}
			b, ferr := format.Node(f)
			if ferr != nil {
				t.Fatalf("cannot format partial file: %v", ferr)
			}
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("file:\n    got:\n%s\n    want:\n%s", got, tc.want)
			}
			if len(tc.errs) == 0 {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected an error containing %q", tc.errs)
				return
			}
			for _, want := range tc.errs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}
