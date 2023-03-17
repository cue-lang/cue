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

		// Issue 2060
		let X = {a: b: 0}
		x: y: X.a
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
	}, {
		path: ParsePath("x.y"),
		out:  "{\n\tb: 0\n}",
		str:  "x.y",
	}, {
		path: ParsePath("x.y.b"),
		out:  "0",
		str:  "x.y.b",
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

var selectorTests = []struct {
	sel          Selector
	stype        SelectorType
	string       string
	unquoted     string
	index        int
	isHidden     bool
	isConstraint bool
	isDefinition bool
	isString     bool
	pkgPath      string
}{{
	sel:      Str("foo"),
	stype:    StringLabel,
	string:   "foo",
	unquoted: "foo",
	isString: true,
}, {
	sel:      Str("_foo"),
	stype:    StringLabel,
	string:   `"_foo"`,
	unquoted: "_foo",
	isString: true,
}, {
	sel:      Str(`a "b`),
	stype:    StringLabel,
	string:   `"a \"b"`,
	unquoted: `a "b`,
	isString: true,
}, {
	sel:    Index(5),
	stype:  IndexLabel,
	string: "5",
	index:  5,
}, {
	sel:          Def("foo"),
	stype:        DefinitionLabel,
	string:       "#foo",
	isDefinition: true,
}, {
	sel:          Str("foo").Optional(),
	stype:        StringLabel | OptionalConstraint,
	string:       "foo?",
	unquoted:     "foo",
	isString:     true,
	isConstraint: true,
}, {
	sel:          Def("foo").Optional(),
	stype:        DefinitionLabel | OptionalConstraint,
	string:       "#foo?",
	isDefinition: true,
	isConstraint: true,
}, {
	sel:          AnyString,
	stype:        StringLabel | PatternConstraint,
	string:       "[_]",
	isConstraint: true,
}, {
	sel:          AnyIndex,
	stype:        IndexLabel | PatternConstraint,
	string:       "[_]",
	isConstraint: true,
}, {
	sel:      Hid("_foo", "example.com"),
	stype:    HiddenLabel,
	string:   "_foo",
	isHidden: true,
	pkgPath:  "example.com",
}, {
	sel:          Hid("_#foo", "example.com"),
	stype:        HiddenDefinitionLabel,
	string:       "_#foo",
	isHidden:     true,
	isDefinition: true,
	pkgPath:      "example.com",
}}

func TestSelector(t *testing.T) {
	for _, tc := range selectorTests {
		t.Run(tc.sel.String(), func(t *testing.T) {
			sel := tc.sel
			if got, want := sel.Type(), tc.stype; got != want {
				t.Errorf("unexpected type; got %v want %v", got, want)
			}
			if got, want := sel.String(), tc.string; got != want {
				t.Errorf("unexpected sel.String result; got %q want %q", got, want)
			}
			if tc.unquoted == "" {
				checkPanic(t, "Selector.Unquoted invoked on non-string label", func() {
					sel.Unquoted()
				})
			} else {
				if got, want := sel.Unquoted(), tc.unquoted; got != want {
					t.Errorf("unexpected sel.Unquoted result; got %q want %q", got, want)
				}
			}
			if sel.Type() != IndexLabel {
				checkPanic(t, "Index called on non-index selector", func() {
					sel.Index()
				})
			} else {
				if got, want := sel.Index(), tc.index; got != want {
					t.Errorf("unexpected sel.Index result; got %v want %v", got, want)
				}
			}
			if got, want := sel.Type().IsHidden(), tc.isHidden; got != want {
				t.Errorf("unexpected sel.IsHidden result; got %v want %v", got, want)
			}
			if got, want := sel.IsConstraint(), tc.isConstraint; got != want {
				t.Errorf("unexpected sel.IsOptional result; got %v want %v", got, want)
			}
			if got, want := sel.IsString(), tc.isString; got != want {
				t.Errorf("unexpected sel.IsString result; got %v want %v", got, want)
			}
			if got, want := sel.IsDefinition(), tc.isDefinition; got != want {
				t.Errorf("unexpected sel.IsDefinition result; got %v want %v", got, want)
			}
			if got, want := sel.PkgPath(), tc.pkgPath; got != want {
				t.Errorf("unexpected sel.PkgPath result; got %v want %v", got, want)
			}
		})
	}
}

func TestSelectorTypeString(t *testing.T) {
	if got, want := InvalidSelectorType.String(), "NoLabels"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
	if got, want := PatternConstraint.String(), "PatternConstraint"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
	if got, want := (StringLabel | OptionalConstraint).String(), "StringLabel|OptionalConstraint"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
	if got, want := SelectorType(255).String(), "StringLabel|IndexLabel|DefinitionLabel|HiddenLabel|HiddenDefinitionLabel|OptionalConstraint|RequiredConstraint|PatternConstraint"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
}

func checkPanic(t *testing.T, wantPanicStr string, f func()) {
	gotPanicStr := ""
	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Errorf("function did not panic")
				return
			}
			gotPanicStr = fmt.Sprint(e)
		}()
		f()
	}()
	if got, want := gotPanicStr, wantPanicStr; got != want {
		t.Errorf("unexpected panic message; got %q want %q", got, want)
	}
}
