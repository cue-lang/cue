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

import (
	"slices"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestKindString(t *testing.T) {
	testCases := []struct {
		input     Kind
		want      string
		wantKinds []Kind
	}{{
		input: BottomKind,
		want:  "_|_",
	}, {
		input:     TopKind,
		want:      "_",
		wantKinds: []Kind{NullKind, BoolKind, IntKind, FloatKind, StringKind, BytesKind, FuncKind, ListKind, StructKind},
	}, {
		input:     IntKind | ListKind,
		want:      `(int|[...])`,
		wantKinds: []Kind{IntKind, ListKind},
	}, {
		input:     NullKind,
		want:      "null",
		wantKinds: []Kind{NullKind},
	}, {
		input:     IntKind,
		want:      "int",
		wantKinds: []Kind{IntKind},
	}, {
		input:     FloatKind,
		want:      "float",
		wantKinds: []Kind{FloatKind},
	}, {
		input:     StringKind,
		want:      "string",
		wantKinds: []Kind{StringKind},
	}, {
		input:     BytesKind,
		want:      "bytes",
		wantKinds: []Kind{BytesKind},
	}, {
		input:     StructKind,
		want:      "{...}",
		wantKinds: []Kind{StructKind},
	}, {
		input:     ListKind,
		want:      "[...]",
		wantKinds: []Kind{ListKind},
	}, {
		input:     NumberKind,
		want:      "number",
		wantKinds: []Kind{IntKind, FloatKind},
	}, {
		input:     BoolKind | NumberKind | ListKind,
		want:      "(bool|[...]|number)",
		wantKinds: []Kind{BoolKind, IntKind, FloatKind, ListKind},
	}, {
		input:     1 << 15,
		want:      "bad(15)",
		wantKinds: []Kind{1 << 15},
	}}
	for _, tc := range testCases {
		t.Run(tc.want, func(t *testing.T) {
			got := tc.input.TypeString()
			if got != tc.want {
				t.Errorf("\n got %v;\nwant %v", got, tc.want)
			}
			qt.Check(t, qt.Equals(tc.input.Count(), len(tc.wantKinds)))
			qt.Check(t, qt.DeepEquals(slices.Collect(tc.input.Kinds()), tc.wantKinds))
		})
	}
}
