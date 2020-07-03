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

package adt

import "testing"

func TestKindString(t *testing.T) {
	testCases := []struct {
		input Kind
		want  string
	}{{
		input: BottomKind,
		want:  "_|_",
	}, {
		input: IntKind | ListKind,
		want:  `(int|[...])`,
	}, {
		input: NullKind,
		want:  "null",
	}, {
		input: IntKind,
		want:  "int",
	}, {
		input: FloatKind,
		want:  "float",
	}, {
		input: StringKind,
		want:  "string",
	}, {
		input: BytesKind,
		want:  "bytes",
	}, {
		input: StructKind,
		want:  "{...}",
	}, {
		input: ListKind,
		want:  "[...]",
	}, {
		input: NumberKind,
		want:  "number",
	}, {
		input: BoolKind | NumberKind | ListKind,
		want:  "(bool|[...]|number)",
	}, {
		input: 1 << 15,
		want:  "bad(15)",
	}}
	for _, tc := range testCases {
		t.Run(tc.want, func(t *testing.T) {
			got := tc.input.TypeString()
			if got != tc.want {
				t.Errorf("\n got %v;\nwant %v", got, tc.want)
			}
		})
	}
}
