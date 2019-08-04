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

package cue

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
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
		in:  `bar=str`,
		out: "[{bar=str 3}]",
	}, {
		in:  `bar="str"`,
		out: "[{bar=str 3}]",
	}, {
		in:  `bar=,baz=`,
		out: "[{bar= 3} {baz= 3}]",
	}, {
		in:  `foo=1,bar="str",baz=free form`,
		out: "[{foo=1 3} {bar=str 3} {baz=free form 3}]",
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
		err: "invalid attribute",
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
			pa := &parsedAttr{}
			err := parseAttrBody(&context{}, baseValue{}, tc.in, pa)

			if tc.err != "" {
				if !strings.Contains(debugStr(&context{}, err), tc.err) {
					t.Errorf("error was %v; want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if got := fmt.Sprint(pa.fields); got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
		})
	}
}

func TestCreateAttrs(t *testing.T) {
	testdata := []struct {
		// space-separated lists of attributes
		in, out string
		err     string
	}{{
		in:  "@foo()",
		out: "foo:",
	}, {
		in:  "@b(bb) @aaa(aa,)",
		out: "aaa:aa, b:bb",
	}, {
		in:  "@b(a,",
		err: "invalid attribute",
	}, {
		in:  "@b(foo) @b(foo)",
		err: "attributes",
	}, {
		in:  "@b('' ,b)",
		err: "invalid attribute",
	}, {
		in:  `@foo(,"bar")`,
		out: `foo:,"bar"`,
	}, {
		in:  `@foo("bar",1)`,
		out: `foo:"bar",1`,
	}, {
		in:  `@foo("bar")`,
		out: `foo:"bar"`,
	}, {
		in:  `@foo(,"bar",1)`,
		out: `foo:,"bar",1`,
	}}
	for _, tc := range testdata {
		t.Run(tc.in, func(t *testing.T) {
			a := []*ast.Attribute{}
			for _, s := range strings.Split(tc.in, " ") {
				a = append(a, &ast.Attribute{Text: s})
			}
			attrs, err := createAttrs(&context{}, baseValue{}, a)

			if tc.err != "" {
				if !strings.Contains(debugStr(&context{}, err), tc.err) {
					t.Errorf("error was %v; want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sa := []string{}
			for _, a := range attrs.attr {
				sa = append(sa, a.key()+":"+a.body())
			}
			if got := strings.Join(sa, " "); got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
		})
	}
}

func TestUnifyAttrs(t *testing.T) {
	parse := func(s string) *attributes {
		a := []*ast.Attribute{}
		for _, s := range strings.Split(s, " ") {
			a = append(a, &ast.Attribute{Text: s})
		}
		attrs, _ := createAttrs(&context{}, baseValue{}, a)
		return attrs
	}
	foo := parse("@foo()")

	testdata := []struct {
		// space-separated lists of attributes
		a, b, out *attributes
		err       string
	}{{
		a:   nil,
		b:   nil,
		out: nil,
	}, {
		a:   nil,
		b:   foo,
		out: foo,
	}, {
		a:   foo,
		b:   nil,
		out: foo,
	}, {
		a:   foo,
		b:   foo,
		out: foo,
	}, {
		a:   foo,
		b:   parse("@bar()"),
		out: parse("@bar() @foo()"),
	}, {
		a:   foo,
		b:   parse("@bar() @foo()"),
		out: parse("@bar() @foo()"),
	}, {
		a:   parse("@bar() @foo()"),
		b:   parse("@foo() @bar()"),
		out: parse("@bar() @foo()"),
	}, {
		a:   parse("@bar() @foo()"),
		b:   parse("@foo() @baz()"),
		out: parse("@bar() @baz() @foo()"),
	}, {
		a:   parse("@foo(ab)"),
		b:   parse("@foo(cd)"),
		err: `conflicting attributes for key "foo"`,
	}}
	for _, tc := range testdata {
		t.Run("", func(t *testing.T) {
			attrs, err := unifyAttrs(&context{}, baseValue{}, tc.a, tc.b)
			if tc.err != "" {
				if !strings.Contains(debugStr(&context{}, err), tc.err) {
					t.Errorf("error was %v; want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(attrs, tc.out) {
				t.Errorf("\ngot:  %v;\nwant: %v", attrs, tc.out)
			}
		})
	}
}
