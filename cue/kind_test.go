// Copyright 2018 The CUE Authors
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

package cue

import (
	"fmt"
	"testing"
)

func TestMatchBinOpKind(t *testing.T) {
	testCases := []struct {
		op   op
		a    kind
		b    kind
		want kind
	}{{
		op:   opMul,
		a:    floatKind,
		b:    numKind,
		want: floatKind,
	}, {
		op:   opMul,
		a:    intKind,
		b:    numKind,
		want: intKind,
	}, {
		op:   opMul,
		a:    floatKind,
		b:    intKind,
		want: floatKind,
	}, {
		op:   opMul,
		a:    listKind,
		b:    intKind,
		want: listKind,
	}, {
		op:   opMul,
		a:    intKind,
		b:    listKind,
		want: listKind,
	}}
	for _, tc := range testCases {
		key := fmt.Sprintf("%s(%v, %v)", tc.op, tc.a, tc.b)
		t.Run(key, func(t *testing.T) {
			got, _ := matchBinOpKind(tc.op, tc.a, tc.b)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
