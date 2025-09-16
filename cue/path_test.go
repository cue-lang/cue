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
	"reflect"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/cuetdtest"
)

func TestPaths(t *testing.T) {
	type testCase struct {
		name      string
		cue       string
		path      cue.Path
		transform func(cue.Value) cue.Value
		wantErr   bool
		wantVal   string
		wantPath  string
		wantInst  string
	}
	testCases := []testCase{{
		name: "Definition",
		cue: `
			#Foo:   a: b: 1
			"#Foo": c: d: 2
		`,
		path:     cue.MakePath(cue.Def("#Foo"), cue.Str("a"), cue.Str("b")),
		wantVal:  "1",
		wantPath: "#Foo.a.b",
	}, {
		name: "DefinitionWithParsePath",
		cue: `
			#Foo:   a: b: 1
			"#Foo": c: d: 2
		`,
		path:     cue.ParsePath(`#Foo.a.b`),
		wantVal:  "1",
		wantPath: "#Foo.a.b",
	}, {
		name: "RegularFieldLookingLikeDefinition",
		cue: `
			#Foo:   a: b: 1
			"#Foo": c: d: 2
		`,
		path:     cue.ParsePath(`"#Foo".c.d`),
		wantVal:  "2",
		wantPath: `"#Foo".c.d`,
	}, {
		name: "DefWithoutLeadingHash",
		cue: `
			#Foo:   a: b: 1
			"#Foo": c: d: 2
		`,
		// fallback Def(Foo) -> Def(#Foo)
		path:     cue.MakePath(cue.Def("Foo"), cue.Str("a"), cue.Str("b")),
		wantVal:  "1",
		wantPath: "#Foo.a.b",
	}, {
		name: "FieldNotFound",
		cue: `
			#Foo:   a: b: 1
			"#Foo": c: d: 2
		`,
		path:     cue.ParsePath("#Foo.a.c"),
		wantPath: "#Foo.a.c",
		wantVal:  `_|_ // field not found: c`,
	}, {
		name: "ListIndexWithMakePath",
		cue: `
			b: [4, 5, 6]
		`,
		path:     cue.MakePath(cue.Str("b"), cue.Index(2)),
		wantVal:  "6",
		wantPath: "b[2]", // #Foo.b.2
	}, {
		name: "ListIndexWithParsePath",
		cue: `
			b: [4, 5, 6]
		`,
		path:     cue.ParsePath(`b[2]`),
		wantPath: `b[2]`,
		wantVal:  "6",
	}, {
		name: "StrLookingLikeDefinition",
		cue: `
			c: "#Foo": 7
		`,
		path:     cue.MakePath(cue.Str("c"), cue.Str("#Foo")),
		wantVal:  "7",
		wantPath: `c."#Foo"`,
	}, {
		name: "ParsePathWithQuotedStringLookingLikeDefinition",
		cue: `
			c: "#Foo": 7
		`,
		path:     cue.ParsePath(`c."#Foo"`),
		wantPath: `c."#Foo"`,
		wantVal:  "7",
	}, {
		name: "AnonHiddenField",
		cue: `
			_foo: b: 5
		`,
		path:     cue.MakePath(cue.Hid("_foo", "_"), cue.Str("b")),
		wantVal:  "5",
		wantPath: `_foo.b`,
	}, {
		name: "ParsePathWithHiddenLabel",
		cue: `
			_foo: b: 5
		`,
		path:     cue.ParsePath("foo._foo"),
		wantPath: "_|_",
		wantErr:  true,
		wantVal:  `_|_ // invalid path: hidden label _foo not allowed`,
	}, {
		name: "ParsePathWithUnterminatedStringLiteral",
		cue: `
			c: "#Foo": 7
		`,
		path:     cue.ParsePath(`c."#Foo`),
		wantPath: "_|_",
		wantErr:  true,
		wantVal:  `_|_ // string literal not terminated`,
	}, {
		name: "ParsePathWithNonConstantIndex",
		cue: `
			a: 3
			b: [4, 5, 6]
		`,
		path:     cue.ParsePath(`b[a]`),
		wantPath: "_|_",
		wantErr:  true,
		wantVal:  `_|_ // non-constant expression a`,
	}, {
		name: "ParsePathWithInvalidStringIndex",
		cue: `
			b: [4, 5, 6]
		`,
		path:     cue.ParsePath(`b['1']`),
		wantPath: "_|_",
		wantErr:  true,
		wantVal:  `_|_ // invalid string index '1'`,
	}, {
		name: "ParsePathWithOutOfRangeIndex",
		cue: `
			b: [4, 5, 6]
		`,
		path:     cue.ParsePath(`b[3T]`),
		wantPath: "_|_",
		wantErr:  true,
		wantVal:  `_|_ // int label out of range (3000000000000 not >=0 and <= 268435454)`,
	}, {
		name: "ParsePathWithFloatIndex",
		cue: `
			b: [4, 5, 6]
		`,
		path:     cue.ParsePath(`b[3.3]`),
		wantPath: "_|_",
		wantErr:  true,
		wantVal:  `_|_ // invalid literal 3.3`,
	}, {
		name: "MapWithAnyString",
		cue: `
			map: [string]: int
		`,
		path:     cue.MakePath(cue.Str("map"), cue.AnyString),
		wantVal:  "int",
		wantPath: "map.[_]",
	}, {
		name:     "ListWithAnyIndex",
		cue:      `list: [...int]`,
		path:     cue.MakePath(cue.Str("list"), cue.AnyIndex),
		wantVal:  "int",
		wantPath: "list.[_]",
	}, {
		name: "Issue2060_1",
		cue: `
			let X = {a: b: 0}
			x: y: X.a
		`,
		path:     cue.ParsePath("x.y"),
		wantVal:  "{\n\tb: 0\n}",
		wantPath: "x.y",
	}, {
		name: "Issue2060_2",
		cue: `
			let X = {a: b: 0}
			x: y: X.a
		`,
		path:     cue.ParsePath("x.y.b"),
		wantVal:  "0",
		wantPath: "x.y.b",
	}, {
		name: "Issue3577",
		cue: `
			pkg: z
			z: y: "hello"
		`,
		path:     cue.ParsePath("pkg.y"),
		wantVal:  `"hello"`,
		wantPath: "pkg.y", // show original path, not structure shared path.
	}, {
		name: "Issue3922",
		cue: `
			out: #Output
			#Output: name: _data.name
			_data: name: "one"
		`,
		path:     cue.ParsePath("out.name"),
		wantVal:  `"one"`,
		wantPath: "out.name",
	}, {
		name: "UnrootedValue",
		cue: `
			{a: b: 1}.a
		`,
		path: cue.Path{},
		transform: func(v cue.Value) cue.Value {
			op, args := v.Expr()
			if op != cue.SelectorOp {
				panic(fmt.Errorf("unexpected operation %v", op))
			}
			return args[0].LookupPath(cue.ParsePath("a.b"))
		},
		wantVal:  "1",
		wantInst: "nil",
		wantPath: "a.b",
	}, {
		name: "ValueInPackage",
		cue: `
			import "strings"
			strings.ToUpper("foo")
		`,
		path: cue.Path{},
		transform: func(v cue.Value) cue.Value {
			op, args := v.Expr()
			if op != cue.CallOp {
				panic(fmt.Errorf("unexpected operation %v", op))
			}
			return args[0]
		},
		wantVal:  `strings.ToUpper`,
		wantPath: "ToUpper",
	}}

	cuetdtest.Run(t, testCases, func(t *cuetdtest.T, tc *testCase) {
		ctx := t.M.CueContext()
		val := mustCompile(t, ctx, tc.cue)

		t.Equal(tc.path.Err() != nil, tc.wantErr)

		w := val.LookupPath(tc.path)
		if tc.transform != nil {
			w = tc.transform(w)
		}

		t.Equal(fmt.Sprint(w), tc.wantVal)

		if w.Err() != nil {
			return
		}

		root, path := w.RootPath()
		t.Logf("root %v", root)
		t.Equal(path.String(), tc.wantPath)

		// Sanity check that we can get to the
		// value from the root.
		if !root.LookupPath(path).Exists() {
			t.Errorf("root path does not resolve")
		}

		inst := root.BuildInstance()
		importPath := ""
		switch {
		case inst == nil:
			importPath = "nil"
		case inst.ImportPath == "":
			if inst != val.BuildInstance() {
				t.Errorf("unexpected instance with empty import path")
			}
		default:
			importPath = inst.ImportPath
		}
		t.Equal(importPath, tc.wantInst)
	})
}

// TestWalkPath is a more comprehensive table-driven test.
func TestWalkPath(t *testing.T) {
	ctx := cuecontext.New()

	testCases := []struct {
		name      string
		cueInput  string
		wantPaths []string
	}{
		{
			name: "issue 3922",
			cueInput: `
				out: #Output
				#Output: name: _data.name
				_data: name: "one"
			`,
			wantPaths: []string{
				"", // root
				"out",
				"out.name",
			},
		},
		{
			name: "simple struct",
			cueInput: `
				b: {
					d: 3
					c: 2
				}
				a: 1
			`,
			wantPaths: []string{
				"", // root
				"b",
				"b.d",
				"b.c",
				"a",
			},
		},
		{
			name: "struct with list",
			cueInput: `
				l: [10, {y: 30, x: 20}]
			`,
			wantPaths: []string{
				"", // root
				"l",
				"l[0]",
				"l[1]",
				"l[1].y",
				"l[1].x",
			},
		},
		{
			name:     "root list",
			cueInput: `[10, {x: 20}]`,
			wantPaths: []string{
				"", // root
				"[0]",
				"[1]",
				"[1].x",
			},
		},
		{
			name:     "root literal string",
			cueInput: `"hello"`,
			wantPaths: []string{
				"", // root
			},
		},
		{
			name:     "root literal int",
			cueInput: `123`,
			wantPaths: []string{
				"", // root
			},
		},
		{
			name:     "empty struct",
			cueInput: `{}`,
			wantPaths: []string{
				"", // root
			},
		},
		{
			name: "struct with various field types",
			cueInput: `
				c: _h
				_h: #D
				#D: b: c: 3
				a: _g
				_g: #B
				#B: x: "definition B"
			`,
			// Order: regular (a, c), definitions (#B, #D), hidden (_g, _h)
			wantPaths: []string{
				"", // root
				"c",
				"c.b",
				"c.b.c",
				"a",
				"a.x",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := ctx.CompileString(tc.cueInput)
			if err := v.Err(); err != nil {
				t.Fatalf("CompileString failed for input\n%s\nError: %v", tc.cueInput, err)
			}

			var gotPaths []string
			v.Walk(func(val cue.Value) bool {
				gotPaths = append(gotPaths, val.Path().String())
				return true
			}, nil)

			if !reflect.DeepEqual(gotPaths, tc.wantPaths) {
				t.Errorf("Walk() paths mismatch for input\n%s\ngot:  %#v\nwant: %#v", tc.cueInput, gotPaths, tc.wantPaths)
			}
		})
	}
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

func TestPathAppend(t *testing.T) {
	testCases := []struct {
		name     string
		path     cue.Path
		selector cue.Selector
		want     string
	}{{
		name:     "append string to empty path",
		path:     cue.MakePath(),
		selector: cue.Str("foo"),
		want:     "foo",
	}, {
		name:     "append string to existing path",
		path:     cue.MakePath(cue.Str("a")),
		selector: cue.Str("b"),
		want:     "a.b",
	}, {
		name:     "append index to path",
		path:     cue.MakePath(cue.Str("list")),
		selector: cue.Index(0),
		want:     "list[0]",
	}, {
		name:     "append definition to path",
		path:     cue.MakePath(cue.Str("root")),
		selector: cue.Def("Foo"),
		want:     "root.#Foo",
	}, {
		name:     "append optional selector",
		path:     cue.MakePath(cue.Str("a")),
		selector: cue.Str("b").Optional(),
		want:     "a.b?",
	}, {
		name:     "append required selector",
		path:     cue.MakePath(cue.Str("a")),
		selector: cue.Str("b").Required(),
		want:     "a.b!",
	}, {
		name:     "append AnyString",
		path:     cue.MakePath(cue.Str("map")),
		selector: cue.AnyString,
		want:     "map.[_]",
	}, {
		name:     "append AnyIndex",
		path:     cue.MakePath(cue.Str("list")),
		selector: cue.AnyIndex,
		want:     "list.[_]",
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.path.Append(tc.selector)
			if got := result.String(); got != tc.want {
				t.Errorf("Path.Append().String() = %q, want %q", got, tc.want)
			}
		})
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
