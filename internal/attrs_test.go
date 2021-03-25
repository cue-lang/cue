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

package internal

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/token"
)

func TestAttributeBody(t *testing.T) {
	testdata := []struct {
		in, out string
		err     string
	}{{
		in:  "",
		out: "[{ 0}]",
	}, {
		in:  "bb",
		out: "[{bb 0}]",
	}, {
		in:  "a,",
		out: "[{a 0} { 0}]",
	}, {
		in:  `"a",`,
		out: "[{a 0} { 0}]",
	}, {
		in:  "a,b",
		out: "[{a 0} {b 0}]",
	}, {
		in:  `foo,"bar",#"baz"#`,
		out: "[{foo 0} {bar 0} {baz 0}]",
	}, {
		in:  `foo,bar,baz`,
		out: "[{foo 0} {bar 0} {baz 0}]",
	}, {
		in:  `1,map[int]string`,
		out: "[{1 0} {map[int]string 0}]",
	}, {
		in:  `1,map[int]string`,
		out: "[{1 0} {map[int]string 0}]",
	}, {
		in:  `bar=str`,
		out: "[{bar=str 3}]",
	}, {
		in:  `bar="str"`,
		out: "[{bar=str 3}]",
	}, {
		in:  `foo.bar="str"`,
		out: "[{foo.bar=str 7}]",
	}, {
		in:  `bar=,baz=`,
		out: "[{bar= 3} {baz= 3}]",
	}, {
		in:  `foo=1,bar="str",baz=free form`,
		out: "[{foo=1 3} {bar=str 3} {baz=free form 3}]",
	}, {
		in:  `foo=1,bar="str",baz=free form  `,
		out: "[{foo=1 3} {bar=str 3} {baz=free form 3}]",
	}, {
		in:  `foo=1,bar="str"  ,baz="free form  "`,
		out: "[{foo=1 3} {bar=str 3} {baz=free form   3}]",
	}, {
		in: `"""
		"""`,
		out: "[{ 0}]",
	}, {
		in: `#'''
			\#x20
			'''#`,
		out: "[{  0}]",
	}, {
		in:  "'' ,b",
		out: "[{ 0} {b 0}]",
	}, {
		in:  "' ,b",
		err: "not terminated",
	}, {
		in:  `"\ "`,
		err: "invalid attribute",
	}, {
		in:  `# `,
		err: "invalid attribute",
	}}
	for _, tc := range testdata {
		t.Run(tc.in, func(t *testing.T) {
			pa := ParseAttrBody(token.NoPos, tc.in)
			err := pa.Err

			if tc.err != "" {
				if !strings.Contains(err.Error(), tc.err) {
					t.Errorf("error was %v; want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if got := fmt.Sprint(pa.Fields); got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
		})
	}
}
