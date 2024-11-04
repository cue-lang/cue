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

//go:build !gen

package gocode

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/gocode/testdata/pkg1"
	"cuelang.org/go/encoding/gocode/testdata/pkg2"
)

type validator interface {
	Validate() error
}

func TestPackages(t *testing.T) {

	testCases := []struct {
		name  string
		value validator
		want  string
	}{{
		name:  "failing int",
		value: pkg2.PickMe(4),
		want:  "invalid value 4 (out of bound >5):\n    pkg2/instance.cue:x:x",
	}, {
		name:  "failing field with validator",
		value: &pkg1.OtherStruct{A: "car"},
		want: `
2 errors in empty disjunction:
conflicting values null and {A:strings.ContainsAny("X"),P:"cuelang.org/go/encoding/gocode/testdata/pkg2".PickMe} (mismatched types null and struct):
    pkg1/instance.cue:x:x
A: invalid value "car" (does not satisfy strings.ContainsAny):
    pkg1/instance.cue:x:x
    pkg1/instance.cue:x:x
`,
	}, {
		name:  "failing field of type int",
		value: &pkg1.MyStruct{A: 11, B: "dog"},
		want: `
2 errors in empty disjunction:
conflicting values null and {A:<=10,B:(=~"cat"|*"dog"),O?:OtherStruct,I:"cuelang.org/go/encoding/gocode/testdata/pkg2".ImportMe} (mismatched types null and struct):
    pkg1/instance.cue:x:x
A: invalid value 11 (out of bound <=10):
    pkg1/instance.cue:x:x
`,
	}, {
		name:  "failing nested struct ",
		value: &pkg1.MyStruct{A: 5, B: "dog", O: &pkg1.OtherStruct{A: "car", P: 6}},
		want: `
4 errors in empty disjunction:
conflicting values null and {A:<=10,B:(=~"cat"|*"dog"),O?:OtherStruct,I:"cuelang.org/go/encoding/gocode/testdata/pkg2".ImportMe} (mismatched types null and struct):
    pkg1/instance.cue:x:x
O: 2 errors in empty disjunction:
O: conflicting values null and {A:strings.ContainsAny("X"),P:"cuelang.org/go/encoding/gocode/testdata/pkg2".PickMe} (mismatched types null and struct):
    pkg1/instance.cue:x:x
    pkg1/instance.cue:x:x
O.A: invalid value "car" (does not satisfy strings.ContainsAny):
    pkg1/instance.cue:x:x
    pkg1/instance.cue:x:x
`,
	}, {
		name:  "fail nested struct of different package",
		value: &pkg1.MyStruct{A: 5, B: "dog", O: &pkg1.OtherStruct{A: "X", P: 4}},
		want: `
4 errors in empty disjunction:
conflicting values null and {A:<=10,B:(=~"cat"|*"dog"),O?:OtherStruct,I:"cuelang.org/go/encoding/gocode/testdata/pkg2".ImportMe} (mismatched types null and struct):
    pkg1/instance.cue:x:x
O: 2 errors in empty disjunction:
O: conflicting values null and {A:strings.ContainsAny("X"),P:"cuelang.org/go/encoding/gocode/testdata/pkg2".PickMe} (mismatched types null and struct):
    pkg1/instance.cue:x:x
    pkg1/instance.cue:x:x
O.P: invalid value 4 (out of bound >5):
    pkg2/instance.cue:x:x
`,
	}, {
		name: "all good",
		value: &pkg1.MyStruct{
			A: 5,
			B: "dog",
			I: &pkg2.ImportMe{A: 1000, B: "a"},
		},
		want: "nil",
	}}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.TrimSpace(errStr(tc.value.Validate()))
			want := strings.TrimSpace(tc.want)
			if got != want {
				t.Errorf("got:\n%q\nwant:\n%q", got, want)
			}
		})
	}
}

func errStr(err error) string {
	if err == nil {
		return "nil"
	}
	buf := &bytes.Buffer{}
	errors.Print(buf, err, nil)
	r := regexp.MustCompile(`.cue:\d+:\d+`)
	return r.ReplaceAllString(buf.String(), ".cue:x:x")
}
