// Copyright 2024 CUE Authors
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

package internal_test

import (
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetest"
	"github.com/go-quicktest/qt"
)

type closednessTest struct {
	name string
	src  string
	want string
}

var closednessTests = []closednessTest{{
	name: "EllipsisAtTopLevel",
	src:  `a: 5, ...`,
	want: `a: 5, ...`,
}, {
	name: "EllipsisInEmbeddedStructLiteral",
	src:  `{a: 5, ...}`,
	want: `{a: 5, ...}`,
}, {
	name: "ReferenceIntoStructLiteral",
	src:  `{x: {a: {b: 5, ...}}.a}`,
	want: `{x: {a: {b: 5, ...}}.a}`,
}, {
	name: "ReferenceIntoClosedFieldOfStructLiteralInDefinition",
	src: `#x: {
        {a: close({b: 5, ...})}.a
    }`,
	want: `#x: {{a: {b: 5, ...}}.a}`,
}, {
	name: "UnificationWithReferenceIntoClosedFieldOfStructLiteralInDefinition",
	src: `#x: {
		{a: close({b: 5, ...})}.a & {a:1}
    }`,
	want: `#x: {{a: close({b: 5, ...})}.a&{a: 1}}`,
}, {
	name: "EllipsisInMatchNArg",
	src:  `matchN(1, [{a: 5, ...}])`,
	want: `matchN(1, [{a: 5}])`,
}, {
	name: "CloseStructWithEllipsis",
	src:  `a: close({a: 5, ...})`,
	want: `a: close({a: 5, ...})`,
}, {
	name: "DoubleCloseInStructWithEllipsis",
	src:  `a: close(close({a: 5, ...}))`,
	want: `a: close({a: 5, ...})`,
}, {
	name: "TribleCloseInStructWithEllipsis",
	src:  `a: close(close(close({a: 5, ...})))`,
	want: `a: close({a: 5, ...})`,
}, {
	name: "CloseInStruct",
	src:  `a: close({a: 5})`,
	want: `a: close({a: 5})`,
}, {
	name: "DoubleCloseInStruct",
	src:  `a: close(close({a: 5}))`,
	want: `a: close({a: 5})`,
}, {
	name: "TribleCloseInStruct",
	src:  `a: close(close(close({a: 5})))`,
	want: `a: close({a: 5})`,
}, {
	name: "CloseInDefinition",
	src:  `#a: close({a: 5})`,
	want: `#a: {a: 5}`,
}, {
	name: "DoubleCloseInDefinition",
	src:  `#a: close(close({a: 5}))`,
	want: `#a: {a: 5}`,
}, {
	name: "TribleCloseInDefinition",
	src:  `#a: close(close(close({a: 5})))`,
	want: `#a: {a: 5}`,
}, {
	name: "CloseInDefinitionWithEllipsis",
	src:  `#a: close({a: 5, ...})`,
	want: `#a: {a: 5, ...}`,
}, {
	name: "CloseInNestedFieldInDefinition",
	src:  `a: #b: c: close({a: 5})`,
	want: `a: {#b: {c: {a: 5}}}`,
}, {
	name: "UnificationWithEllipsisInDefinition",
	src:  `#a: {a: 5, ...} & {b: 5}`,
	want: `#a: {a: 5, ...}&{b: 5}`,
}, {
	name: "UnificationWithCloseInDefinition",
	src:  `#a: close({a: 5}) & {b: 5}`,
	want: `#a: close({a: 5})&{b: 5}`,
}, {
	name: "DisjunctionWithCloseInDefinition",
	src:  `#a: null | close({a: 1})`,
	want: `#a: null|close({a: 1})`,
}, {
	name: "CloseInNestedExpressionInDefinition",
	// We can't elide the close call when the
	// literal is in a disjunction that features in some
	// other expression because that would change
	// semantics.
	src:  `#a: ({b: 1} | close({a: 1})) & {c: 1}`,
	want: `#a: ({b: 1}|close({a: 1}))&{c: 1}`,
}, {
	name: "ListTake",
	// Usually, the struct literals passed into a function call
	// do not appear in the return from the function, so we
	// can elide redundant ellipses, but there are some counter-examples.
	// We should probably have a list of functions we know are OK
	// to apply this simplification to (e.g. matchN).
	src:  `a: list.Take([{x: 5, ...}])[0]`,
	want: `a: list.Take([{x: 5, ...}])[0]`,
}, {
	name: "ListTakeInDefinition",
	// Usually, the struct literals passed into a function call
	// do not appear in the return from the function, so we
	// can elide redundant ellipses, but there are some counter-examples.
	// We should probably have a list of functions we know are OK
	// to apply this simplification to (e.g. matchN).
	src:  `#a: list.Take([{x: 5, ...}])[0]`,
	want: `#a: list.Take([{x: 5, ...}])[0]`,
}, {
	name: "FuncReturnsArgumentRenamedToMatchN",
	src: `
import "list"

_a: {
    matchN: list.Take
    b: matchN([{x: 5, ...}])[0]
}
#a: _a.b
x: #a
x: y: 10
`,
	// Note that eliding the ellipsis here changes semantics.
	// Although functions in CUE can be renamed, making it hard to tell if
	// omitting ellipses is safe, eliding the ellipsis from `matchN`'s parameters
	// for a JSONSchema encoder's output is considered safe.
	// Keep this test case for the record.
	want: `import "list", _a: {matchN: list.Take, b: matchN([{x: 5}])[0]}, #a: _a.b, x: #a, x: {y: 10}`,
}, {
	name: "MatchNWithMultipleArgs",
	src:  `matchN(1, [{a?: bool, ...}, {b?: string, ...}, close({})])`,
	want: `matchN(1, [{a?: bool}, {b?: string}, close({})])`,
}, {
	name: "MatchNWithArgsContainingDefinitions",
	src:  `matchN(1, [{a?: bool, #b: {...}, ...}, {#a: close({...}), b?: string, ...}, close({})])`,
	want: `matchN(1, [{a?: bool, #b: {...}}, {#a: {...}, b?: string}, close({})])`,
}, {
	name: "CloseInFieldInDefinition",
	src:  `#foo: bar: close({a?: int})`,
	want: `#foo: {bar: {a?: int}}`,
}, {
	name: "CloseWithEllipsis",
	src:  `close({a: {...}})`,
	want: `close({a: {...}})`,
}, {
	name: "CloseWithEllipsisInDefinition",
	src:  `close({a: {#b: {...}}})`,
	want: `close({a: {#b: {...}}})`,
}}

func TestSimplifyClosedness(t *testing.T) {
	cuetest.Run(t, closednessTests, func(t *cuetest.T, test *closednessTest) {
		gotf, err := parser.ParseFile("src", test.src, parser.ParseComments)
		qt.Assert(t, qt.IsNil(err))

		gotn := internal.SimplifyClosedness(gotf)
		t.Equal(astinternal.DebugStr(gotn), test.want)
	})
}
