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

package cue

import (
	"fmt"
	"testing"
)

func TestPaths(t *testing.T) {
	var r Runtime
	inst, _ := r.Compile("", `
		#Foo:   a: b: 1
		"#Foo": c: d: 2
		_foo: b: 5
		a: 3
		b: [4, 5, 6]
		c: "#Foo": 7
		map: [string]: int
		list: [...int]
	`)
	testCases := []struct {
		path Path
		out  string
		str  string
		err  bool
	}{{
		path: MakePath(Str("list"), AnyIndex),
		out:  "int",
		str:  "list.[_]",
	}, {

		path: MakePath(Def("#Foo"), Str("a"), Str("b")),
		out:  "1",
		str:  "#Foo.a.b",
	}, {
		path: ParsePath(`#Foo.a.b`),
		out:  "1",
		str:  "#Foo.a.b",
	}, {
		path: ParsePath(`"#Foo".c.d`),
		out:  "2",
		str:  `"#Foo".c.d`,
	}, {
		// fallback Def(Foo) -> Def(#Foo)
		path: MakePath(Def("Foo"), Str("a"), Str("b")),
		out:  "1",
		str:  "#Foo.a.b",
	}, {
		path: MakePath(Str("b"), Index(2)),
		out:  "6",
		str:  "b[2]", // #Foo.b.2
	}, {
		path: MakePath(Str("c"), Str("#Foo")),
		out:  "7",
		str:  `c."#Foo"`,
	}, {
		path: MakePath(Hid("_foo", "_"), Str("b")),
		out:  "5",
		str:  `_foo.b`,
	}, {
		path: ParsePath("#Foo.a.b"),
		str:  "#Foo.a.b",
		out:  "1",
	}, {
		path: ParsePath("#Foo.a.c"),
		str:  "#Foo.a.c",
		out:  `_|_ // field not found: c`,
	}, {
		path: ParsePath(`b[2]`),
		str:  `b[2]`,
		out:  "6",
	}, {
		path: ParsePath(`c."#Foo"`),
		str:  `c."#Foo"`,
		out:  "7",
	}, {
		path: ParsePath("foo._foo"),
		str:  "_|_",
		err:  true,
		out:  `_|_ // invalid path: hidden label _foo not allowed`,
	}, {
		path: ParsePath(`c."#Foo`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // string literal not terminated`,
	}, {
		path: ParsePath(`b[a]`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // non-constant expression a`,
	}, {
		path: ParsePath(`b['1']`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // invalid string index '1'`,
	}, {
		path: ParsePath(`b[3T]`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // int label out of range (3000000000000 not >=0 and <= 268435454)`,
	}, {
		path: ParsePath(`b[3.3]`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // invalid literal 3.3`,
	}, {
		path: MakePath(Str("map"), AnyString),
		out:  "int",
		str:  "map.[_]",
	}, {
		path: MakePath(Str("list"), AnyIndex),
		out:  "int",
		str:  "list.[_]",
	}}

	v := inst.Value()
	for _, tc := range testCases {
		t.Run(tc.str, func(t *testing.T) {
			if gotErr := tc.path.Err() != nil; gotErr != tc.err {
				t.Errorf("error: got %v; want %v", gotErr, tc.err)
			}

			w := v.LookupPath(tc.path)

			if got := fmt.Sprint(w); got != tc.out {
				t.Errorf("Value: got %v; want %v", got, tc.out)
			}

			if got := tc.path.String(); got != tc.str {
				t.Errorf("String: got %v; want %v", got, tc.str)
			}

			if w.Err() != nil {
				return
			}

			if got := w.Path().String(); got != tc.str {
				t.Errorf("Path: got %v; want %v", got, tc.str)
			}
		})
	}
}
