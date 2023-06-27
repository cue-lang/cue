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
	"github.com/google/go-cmp/cmp"
)

type keyVals [][3]string

func TestAttributeBody(t *testing.T) {
	testdata := []struct {
		in  string
		out keyVals
		err string
	}{{
		in:  "",
		out: keyVals{{}},
	}, {
		in:  "bb",
		out: keyVals{{"", "bb", "bb"}},
	}, {
		in:  "a,",
		out: keyVals{{"", "a", "a"}, {"", ""}},
	}, {
		in:  `"a",`,
		out: keyVals{{"", "a", `"a"`}, {"", ""}},
	}, {
		in:  "a,b",
		out: keyVals{{"", "a", "a"}, {"", "b", "b"}},
	}, {
		in:  `foo,"bar",#"baz"#`,
		out: keyVals{{"", "foo", "foo"}, {"", "bar", `"bar"`}, {"", "baz", `#"baz"#`}},
	}, {
		in:  `foo,bar,baz`,
		out: keyVals{{"", "foo", "foo"}, {"", "bar", "bar"}, {"", "baz", "baz"}},
	}, {
		in:  `1,map[int]string`,
		out: keyVals{{"", "1", "1"}, {"", "map[int]string", "map[int]string"}},
	}, {
		in:  `bar=str`,
		out: keyVals{{"bar", "str", "bar=str"}},
	}, {
		in:  `bar="str"`,
		out: keyVals{{"bar", "str", `bar="str"`}},
	}, {
		in:  `foo.bar="str"`,
		out: keyVals{{"foo.bar", "str", `foo.bar="str"`}},
	}, {
		in:  `bar=,baz=`,
		out: keyVals{{"bar", "", "bar="}, {"baz", "", "baz="}},
	}, {
		in:  `foo=1,bar="str",baz=free form`,
		out: keyVals{{"foo", "1", "foo=1"}, {"bar", "str", `bar="str"`}, {"baz", "free form", "baz=free form"}},
	}, {
		in:  `foo=1,bar="str",baz=free form  `,
		out: keyVals{{"foo", "1", "foo=1"}, {"bar", "str", `bar="str"`}, {"baz", "free form", "baz=free form  "}},
	}, {
		in:  `foo=1,bar="str"  ,baz="free form  "`,
		out: keyVals{{"foo", "1", "foo=1"}, {"bar", "str", `bar="str"  `}, {"baz", "free form  ", `baz="free form  "`}},
	}, {
		in: `"""
		"""`,
		out: keyVals{{"", "", `"""
		"""`}},
	}, {
		in: `#'''
			\#x20
			'''#`,
		out: keyVals{{"", " ", `#'''
			\#x20
			'''#`}},
	}, {
		in:  "'' ,b",
		out: keyVals{{"", "", "'' "}, {"", "b", "b"}},
	}, {
		in:  "' ,b",
		err: "error scanning attribute text",
	}, {
		in:  `"\ "`,
		err: "error scanning attribute text",
	}, {
		in:  `# `,
		out: keyVals{{"", "#", "# "}},
	}}
	for i, tc := range testdata {
		t.Run(fmt.Sprintf("%d-%s", i, tc.in), func(t *testing.T) {
			f := token.NewFile("test", -1, len(tc.in))
			pos := f.Pos(0, token.NoRelPos)
			pa := ParseAttrBody(pos, tc.in)
			err := pa.Err

			if tc.err != "" {
				if err == nil {
					t.Fatalf("unexpected success when error was expected (%#v)", pa.Fields)
				}
				if !strings.Contains(err.Error(), tc.err) {
					t.Errorf("error was %v; want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var kvs keyVals
			for _, kv := range pa.Fields {
				kvs = append(kvs, [3]string{kv.Key(), kv.Value(), kv.Text()})
			}
			if diff := cmp.Diff(tc.out, kvs); diff != "" {
				t.Errorf("unexpected result; diff (-want +got)\n%s", diff)
			}
		})
	}
}
