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

// +build !gen

package gocode

import (
	"strings"
	"testing"

	"cuelang.org/go/encoding/gocode/testdata/pkg1"
	"cuelang.org/go/encoding/gocode/testdata/pkg2"
)

func TestPackages(t *testing.T) {
	testCases := []struct {
		name string
		got  error
		want string
	}{{
		name: "failing int",
		got: func() error {
			v := pkg2.PickMe(4)
			return v.Validate()
		}(),
		want: "invalid value 4 (out of bound >5):\n    pkg2/instance.cue:x:x",
	}, {
		name: "failing field with validator",
		got: func() error {
			v := &pkg1.OtherStruct{A: "car"}
			return v.Validate()
		}(),
		want: "A: invalid value \"car\" (does not satisfy strings.ContainsAny(\"X\")):\n    pkg1/instance.cue:x:x",
	}, {
		name: "failing field of type int",
		got: func() error {
			v := &pkg1.MyStruct{A: 11, B: "dog"}
			return v.Validate()
		}(),
		want: "A: invalid value 11 (out of bound <=10):\n    pkg1/instance.cue:x:x",
	}, {
		name: "failing nested struct ",
		got: func() error {
			v := &pkg1.MyStruct{A: 5, B: "dog", O: &pkg1.OtherStruct{A: "car"}}
			return v.Validate()
		}(),
		want: "O.A: invalid value \"car\" (does not satisfy strings.ContainsAny(\"X\")):\n    pkg1/instance.cue:x:x",
	}, {
		name: "fail nested struct of different package",
		got: func() error {
			v := &pkg1.MyStruct{A: 5, B: "dog", O: &pkg1.OtherStruct{A: "X", P: 4}}
			return v.Validate()
		}(),
		want: "O.P: invalid value 4 (out of bound >5):\n    pkg2/instance.cue:x:x",
	}, {
		name: "all good",
		got: func() error {
			v := &pkg1.MyStruct{
				A: 5,
				B: "dog",
				I: &pkg2.ImportMe{A: 1000, B: "a"},
			}
			return v.Validate()
		}(),
		want: "nil",
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.TrimSpace(errStr(tc.got))
			want := tc.want
			if got != want {
				t.Errorf("got:\n%q\nwant:\n%q", got, want)
			}

		})
	}
}
