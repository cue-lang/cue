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

package adt_test

import (
	"slices"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/core/adt"
)

func TestKindString(t *testing.T) {
	testCases := []struct {
		input     adt.Kind
		want      string
		wantKinds []adt.Kind
	}{{
		input: adt.BottomKind,
		want:  "_|_",
	}, {
		input:     adt.TopKind,
		want:      "_",
		wantKinds: []adt.Kind{adt.NullKind, adt.BoolKind, adt.IntKind, adt.FloatKind, adt.StringKind, adt.BytesKind, adt.FuncKind, adt.ListKind, adt.StructKind},
	}, {
		input:     adt.IntKind | adt.ListKind,
		want:      `(int|[...])`,
		wantKinds: []adt.Kind{adt.IntKind, adt.ListKind},
	}, {
		input:     adt.NullKind,
		want:      "null",
		wantKinds: []adt.Kind{adt.NullKind},
	}, {
		input:     adt.IntKind,
		want:      "int",
		wantKinds: []adt.Kind{adt.IntKind},
	}, {
		input:     adt.FloatKind,
		want:      "float",
		wantKinds: []adt.Kind{adt.FloatKind},
	}, {
		input:     adt.StringKind,
		want:      "string",
		wantKinds: []adt.Kind{adt.StringKind},
	}, {
		input:     adt.BytesKind,
		want:      "bytes",
		wantKinds: []adt.Kind{adt.BytesKind},
	}, {
		input:     adt.StructKind,
		want:      "{...}",
		wantKinds: []adt.Kind{adt.StructKind},
	}, {
		input:     adt.ListKind,
		want:      "[...]",
		wantKinds: []adt.Kind{adt.ListKind},
	}, {
		input:     adt.NumberKind,
		want:      "number",
		wantKinds: []adt.Kind{adt.IntKind, adt.FloatKind},
	}, {
		input:     adt.BoolKind | adt.NumberKind | adt.ListKind,
		want:      "(bool|[...]|number)",
		wantKinds: []adt.Kind{adt.BoolKind, adt.IntKind, adt.FloatKind, adt.ListKind},
	}, {
		input:     1 << 15,
		want:      "bad(15)",
		wantKinds: []adt.Kind{1 << 15},
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
