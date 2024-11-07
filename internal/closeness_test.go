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

package internal_test

import (
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
	"github.com/google/go-cmp/cmp"
)

func TestSimplifyClossness(t *testing.T) {
	const src = `
matchN(1, [{a?: bool, ...}, {b?: string, ...}, close({})])
#bar: matchN(1, [{a?: bool, ...}, {b?: string, ...}, close({})])
#foo: bar: close({a?: int})
a1: close({b:1, ...})
close({a: {...}})
close({a: {#b: {...}}})
`
	tests := []struct {
		name  string
		src   string
		asDef bool
		want  string
	}{
		{
			name:  "simplify as open struct",
			src:   src,
			asDef: false,
			want: `
matchN(1, [{a?: bool}, {b?: string}, close({})])
#bar: matchN(1, [{a?: bool}, {b?: string}, close({})])
#foo: bar: a?: int
a1: close({b:1})
close({a: {}})
close({a: {#b: {...}}})
`,
		},
		{
			name:  "simplify as definition",
			src:   src,
			asDef: true,
			want: `
matchN(1, [{a?: bool}, {b?: string}, close({})])
#bar: matchN(1, [{a?: bool}, {b?: string}, close({})])
#foo: bar: a?: int
a1: {b:1}
{a: {...}}
{a: {#b: {...}}}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotf, err := parser.ParseFile("src", tt.src, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			gotn := internal.SimplifyCloseness(gotf, tt.asDef)

			wantf, err := parser.ParseFile("want", tt.want, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			got := astinternal.DebugStr(gotn)
			want := astinternal.DebugStr(wantf)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Logf("actual result:\n%s", got)
				t.Fatalf("unexpected results (-want +got):\n%s", diff)
			}

		})
	}

}
