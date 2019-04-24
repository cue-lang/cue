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
	"strings"
	"testing"

	"cuelang.org/go/cue/build"
)

func toString(t *testing.T, v Value) string {
	t.Helper()

	b, err := v.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	return strings.Replace(string(b), `"`, "", -1)
}

func TestMerge(t *testing.T) {
	insts := func(s ...string) []string { return s }
	testCases := []struct {
		desc      string
		instances []string
		out       string
		isErr     bool
	}{{
		desc:      "single",
		instances: insts(`a: 1, b: 2`),
		out:       `{a:1,b:2}`,
	}, {
		desc: "multiple",
		instances: insts(
			`a: 1`,
			`b: 2`,
			`a: int`,
		),
		out: `{a:1,b:2}`,
	}, {
		desc: "templates",
		instances: insts(`
			obj <X>: { a: "A" }
			obj alpha: { b: 2 }
			`,
			`
			obj <X>: { a: "B" }
			obj beta: { b: 3 }
			`,
		),
		out: `{obj:{alpha:{a:A,b:2},beta:{a:B,b:3}}}`,
	}, {
		// Structs that are shared in templates may have conflicting results.
		// However, this is not an issue as long as these value are not
		// referenced during evaluation. For generating JSON this is not an
		// issue as such fields are typically hidden.
		desc: "shared struct",
		instances: insts(`
			_shared: { a: "A" }
			obj <X>: _shared & {}
			obj alpha: { b: 2 }
			`,
			`
			_shared: { a: "B" }
			obj <X>: _shared & {}
			obj beta: { b: 3 }
			`,
		),
		out: `{obj:{alpha:{a:A,b:2},beta:{a:B,b:3}}}`,
	}, {
		desc: "top-level comprehensions",
		instances: insts(`
			t: {"\(k)": 10 for k, x in s}
			s <Name>: {}
			s foo a: 1
			`,
			`
			t: {"\(k)": 10 for k, x in s}
			s <Name>: {}
			s bar b: 2
			`,
		),
		out: `{t:{foo:10,bar:10},s:{foo:{a:1},bar:{b:2}}}`,
	}, {
		desc:      "error",
		instances: insts(`a:`),
		out:       `{}`,
		isErr:     true,
	}}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ctx := build.NewContext()
			in := []*build.Instance{}
			for _, str := range tc.instances {
				bi := ctx.NewInstance("dir", nil) // no packages
				bi.AddFile("file", str)
				in = append(in, bi)
			}
			merged := Merge(Build(in)...)
			if err := merged.Err; err != nil {
				if !tc.isErr {
					t.Fatal(err)
				}
			}

			if got := toString(t, merged.Value()); got != tc.out {
				t.Errorf("\n got: %s\nwant: %s", got, tc.out)
			}
		})
	}
}

func TestInstance_Build(t *testing.T) {
	testCases := []struct {
		desc     string
		instance string
		overlay  string
		out      string
	}{{
		desc:     "single",
		instance: `a: {b: 1, c: 2}`,
		overlay:  `res: a`,
		out:      `{res:{b:1,c:2}}`,
	}}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ctx := build.NewContext()

			bi := ctx.NewInstance("main", nil) // no packages
			bi.AddFile("file", tc.instance)
			main := Build([]*build.Instance{bi})
			if err := main[0].Err; err != nil {
				t.Fatal(err)
			}

			bi = ctx.NewInstance("overlay", nil) // no packages
			bi.AddFile("file", tc.overlay)

			overlay := main[0].Build(bi)

			if got := toString(t, overlay.Value()); got != tc.out {
				t.Errorf("\n got: %s\nwant: %s", got, tc.out)
			}
		})
	}
}
