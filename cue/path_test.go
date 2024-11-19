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

package cue_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/cuetdtest"
)

func TestPaths(t *testing.T) {
	type testCase struct {
		path cue.Path
		out  string
		str  string
		err  bool
	}
	testCases := []testCase{{
		path: cue.MakePath(cue.Str("list"), cue.AnyIndex),
		out:  "int",
		str:  "list.[_]",
	}, {

		path: cue.MakePath(cue.Def("#Foo"), cue.Str("a"), cue.Str("b")),
		out:  "1",
		str:  "#Foo.a.b",
	}, {
		path: cue.ParsePath(`#Foo.a.b`),
		out:  "1",
		str:  "#Foo.a.b",
	}, {
		path: cue.ParsePath(`"#Foo".c.d`),
		out:  "2",
		str:  `"#Foo".c.d`,
	}, {
		// fallback Def(Foo) -> Def(#Foo)
		path: cue.MakePath(cue.Def("Foo"), cue.Str("a"), cue.Str("b")),
		out:  "1",
		str:  "#Foo.a.b",
	}, {
		path: cue.MakePath(cue.Str("b"), cue.Index(2)),
		out:  "6",
		str:  "b[2]", // #Foo.b.2
	}, {
		path: cue.MakePath(cue.Str("c"), cue.Str("#Foo")),
		out:  "7",
		str:  `c."#Foo"`,
	}, {
		path: cue.MakePath(cue.Hid("_foo", "_"), cue.Str("b")),
		out:  "5",
		str:  `_foo.b`,
	}, {
		path: cue.ParsePath("#Foo.a.b"),
		str:  "#Foo.a.b",
		out:  "1",
	}, {
		path: cue.ParsePath("#Foo.a.c"),
		str:  "#Foo.a.c",
		out:  `_|_ // field not found: c`,
	}, {
		path: cue.ParsePath(`b[2]`),
		str:  `b[2]`,
		out:  "6",
	}, {
		path: cue.ParsePath(`c."#Foo"`),
		str:  `c."#Foo"`,
		out:  "7",
	}, {
		path: cue.ParsePath("foo._foo"),
		str:  "_|_",
		err:  true,
		out:  `_|_ // invalid path: hidden label _foo not allowed`,
	}, {
		path: cue.ParsePath(`c."#Foo`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // string literal not terminated`,
	}, {
		path: cue.ParsePath(`b[a]`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // non-constant expression a`,
	}, {
		path: cue.ParsePath(`b['1']`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // invalid string index '1'`,
	}, {
		path: cue.ParsePath(`b[3T]`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // int label out of range (3000000000000 not >=0 and <= 268435454)`,
	}, {
		path: cue.ParsePath(`b[3.3]`),
		str:  "_|_",
		err:  true,
		out:  `_|_ // invalid literal 3.3`,
	}, {
		path: cue.MakePath(cue.Str("map"), cue.AnyString),
		out:  "int",
		str:  "map.[_]",
	}, {
		path: cue.MakePath(cue.Str("list"), cue.AnyIndex),
		out:  "int",
		str:  "list.[_]",
	}, {
		path: cue.ParsePath("x.y"),
		out:  "{\n\tb: 0\n}",
		str:  "x.y",
	}, {
		path: cue.ParsePath("x.y.b"),
		out:  "0",
		str:  "x.y.b",
	}, {
		// Issue 3577
		path: cue.ParsePath("pkg.y"),
		out:  `"hello"`,
		str:  "pkg.y", // show original path, not structure shared path.
	}}

	cuetdtest.Run(t, testCases, func(t *cuetdtest.T, tc *testCase) {
		ctx := t.M.CueContext()
		val := mustCompile(t, ctx, `
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

			// Issue 3577
			pkg: z
			z: y: "hello"
		`)

		t.Equal(tc.path.Err() != nil, tc.err)

		w := val.LookupPath(tc.path)

		t.Equal(fmt.Sprint(w), tc.out)

		if w.Err() != nil {
			return
		}

		t.Equal(w.Path().String(), tc.str)
	})
}

var selectorTests = []struct {
	sel          cue.Selector
	stype        cue.SelectorType
	string       string
	unquoted     string
	index        int
	isHidden     bool
	isConstraint bool
	isDefinition bool
	isString     bool
	pkgPath      string
}{{
	sel:      cue.Str("foo"),
	stype:    cue.StringLabel,
	string:   "foo",
	unquoted: "foo",
	isString: true,
}, {
	sel:      cue.Str("_foo"),
	stype:    cue.StringLabel,
	string:   `"_foo"`,
	unquoted: "_foo",
	isString: true,
}, {
	sel:      cue.Str(`a "b`),
	stype:    cue.StringLabel,
	string:   `"a \"b"`,
	unquoted: `a "b`,
	isString: true,
}, {
	sel:    cue.Index(5),
	stype:  cue.IndexLabel,
	string: "5",
	index:  5,
}, {
	sel:          cue.Def("foo"),
	stype:        cue.DefinitionLabel,
	string:       "#foo",
	isDefinition: true,
}, {
	sel:          cue.Str("foo").Optional(),
	stype:        cue.StringLabel | cue.OptionalConstraint,
	string:       "foo?",
	unquoted:     "foo",
	isString:     true,
	isConstraint: true,
}, {
	sel:          cue.Str("foo").Required(),
	stype:        cue.StringLabel | cue.RequiredConstraint,
	string:       "foo!",
	unquoted:     "foo",
	isString:     true,
	isConstraint: true,
}, {
	sel:          cue.Def("foo").Required().Optional(),
	stype:        cue.DefinitionLabel | cue.OptionalConstraint,
	string:       "#foo?",
	isDefinition: true,
	isConstraint: true,
}, {
	sel:          cue.Def("foo").Optional().Required(),
	stype:        cue.DefinitionLabel | cue.RequiredConstraint,
	string:       "#foo!",
	isDefinition: true,
	isConstraint: true,
}, {
	sel:          cue.AnyString,
	stype:        cue.StringLabel | cue.PatternConstraint,
	string:       "[_]",
	isConstraint: true,
}, {
	sel:          cue.AnyIndex,
	stype:        cue.IndexLabel | cue.PatternConstraint,
	string:       "[_]",
	isConstraint: true,
}, {
	sel:      cue.Hid("_foo", "example.com"),
	stype:    cue.HiddenLabel,
	string:   "_foo",
	isHidden: true,
	pkgPath:  "example.com",
}, {
	sel:          cue.Hid("_#foo", "example.com"),
	stype:        cue.HiddenDefinitionLabel,
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
			if sel.Type() != cue.IndexLabel {
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
	if got, want := cue.InvalidSelectorType.String(), "NoLabels"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
	if got, want := cue.PatternConstraint.String(), "PatternConstraint"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
	if got, want := (cue.StringLabel | cue.OptionalConstraint).String(), "StringLabel|OptionalConstraint"; got != want {
		t.Errorf("unexpected SelectorType.String result; got %q want %q", got, want)
	}
	if got, want := cue.SelectorType(255).String(), "StringLabel|IndexLabel|DefinitionLabel|HiddenLabel|HiddenDefinitionLabel|OptionalConstraint|RequiredConstraint|PatternConstraint"; got != want {
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
