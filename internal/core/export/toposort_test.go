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

package export

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"github.com/google/go-cmp/cmp"
)

func TestSortArcs(t *testing.T) {
	testCases := []struct {
		desc string
		in   string
		out  string
	}{{
		desc: "empty",
		in:   ``,
		out:  ``,
	}, {
		desc: "multiple empty",
		in:   `||`,
		out:  ``,
	}, {
		desc: "single list",
		in:   `a b c`,
		out:  `a b c`,
	}, {
		desc: "several one-elem lists",
		in:   `a|b|c`,
		out:  `a b c`,
	}, {
		desc: "glue1",
		in:   `a b c | g h i | c d e g`,
		out:  `a b c d e g h i`,
	}, {
		desc: "glue2",
		in:   `c d e g | a b c | g h i|`,
		out:  `a b c d e g h i`,
	}, {
		desc: "interleaved, prefer first",
		in:   `a b d h k | c d h i k l m`,
		out:  `a b c d h i k l m`,
	}, {
		desc: "subsumed",
		in:   `a b c d e f g h i j k | c e f | i j k`,
		out:  `a b c d e f g h i j k`,
	}, {
		desc: "cycle, single list",
		in:   `a b a`,
		out:  `a b`,
	}, {
		desc: "cycle, across lists",
		in:   `a b | b c | c a`,
		out:  `a b c`,
	}}

	r := runtime.New()

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fa := parseFeatures(r, tc.in)

			keys := sortedArcs(fa)

			want := parseFeatures(r, tc.out)[0]

			if !cmp.Equal(keys, want) {
				got := ""
				for _, f := range keys {
					got += " " + f.SelectorString(r)
				}
				t.Errorf("got: %s\nwant: %s", got, tc.out)
			}
		})
	}
}

func parseFeatures(r adt.Runtime, s string) (res [][]adt.Feature) {
	for _, v := range strings.Split(s, "|") {
		a := []adt.Feature{}
		for _, w := range strings.Fields(v) {
			a = append(a, adt.MakeStringLabel(r, w))
		}
		res = append(res, a)
	}
	return res
}
