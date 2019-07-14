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

package yaml

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
)

func TestYAML(t *testing.T) {
	testCases := []struct {
		name     string
		yaml     string
		want     string
		isStream bool
	}{{
		name: "string literal",
		yaml: `foo`,
		want: `"foo"`,
	}, {
		name: "struct",
		yaml: `a: foo
b: bar`,
		want: `a: "foo"
b: "bar"`,
	}, {
		name: "stream",
		yaml: `a: foo
---
b: bar
c: baz
`,
		want: `[{
	a: "foo"
}, {
	b: "bar"
	c: "baz"
}]`,
		isStream: true,
	}, {
		name:     "emtpy",
		isStream: true,
	}}
	r := &cue.Runtime{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Extract(tc.name, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := format.Node(f)
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}

			inst, err := Decode(r, tc.name, tc.yaml)
			if err != nil {
				t.Fatal(err)
			}
			n := inst.Value().Syntax()
			if s, ok := n.(*ast.StructLit); ok {
				n = &ast.File{Decls: s.Elts}
			}
			b, _ = format.Node(n)
			if got := strings.TrimSpace(string(b)); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}

			inst, _ = r.Compile(tc.name, tc.want)
			if !tc.isStream {
				b, err = Encode(inst.Value())
				if err != nil {
					t.Error(err)
				}
				if got := strings.TrimSpace(string(b)); got != tc.yaml {
					t.Errorf("got %q; want %q", got, tc.yaml)
				}
			} else {
				iter, _ := inst.Value().List()
				b, err := EncodeStream(iter)
				if err != nil {
					t.Error(err)
				}
				if got := string(b); got != tc.yaml {
					t.Errorf("got %q; want %q", got, tc.yaml)
				}
			}
		})
	}
}
