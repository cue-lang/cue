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

package structbuilder_test

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/jsonschema/internal/structbuilder"
	"cuelang.org/go/internal"
)

func TestStructBuilderShadowedRef(t *testing.T) {
	var b structbuilder.Builder
	ref, err := b.GetRef(cue.ParsePath("#foo.bar.baz"))
	qt.Assert(t, qt.IsNil(err))
	ok := b.Put(cue.ParsePath("#foo.bar.baz"), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath("#bar.#foo.xxx"), ref, nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `#bar: #foo: xxx: #foo_1.bar.baz

#foo~(#foo_1): bar: baz: "hello"
`)
}

func TestStructBuilderSelfRef(t *testing.T) {
	var b structbuilder.Builder
	ref, err := b.GetRef(cue.Path{})
	qt.Assert(t, qt.IsNil(err))
	ok := b.Put(cue.Path{}, ast.NewStruct(ast.NewIdent("next"), token.OPTION, ref), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
_schema
_schema: next?: _schema
`)
}

func TestStructBuilderEntryInsideValue(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath("#foo"), ast.NewString("hello"), internal.NewComment(true, "foo comment"))
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath("#foo.#bar.#baz"), ast.NewString("goodbye"), internal.NewComment(true, "baz comment"))
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
// foo comment
#foo: {

	"hello"

	// baz comment
	#bar: #baz: "goodbye"
}
`)
}

func TestStructBuilderNonIdentifierStringNode(t *testing.T) {
	var b structbuilder.Builder
	ref, err := b.GetRef(cue.ParsePath(`#foo."a b".baz`))
	qt.Assert(t, qt.IsNil(err))
	ok := b.Put(cue.ParsePath(`#foo."a b".baz`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath("#bar.#foo.xxx"), ref, nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
#bar: #foo: xxx: #foo_1."a b".baz

#foo~(#foo_1): "a b": baz: "hello"
`)
}

func TestStructBuilderNonIdentifierStringNodeAtRoot(t *testing.T) {
	var b structbuilder.Builder
	_, err := b.GetRef(cue.ParsePath(`"a b".baz`))
	qt.Assert(t, qt.ErrorMatches(err, `initial element of path "\\"a b\\"\.baz" must be expressed as an identifier`))
}

func TestStructBuilderRedefinition(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath(`a.b.c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath(`a.b.c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsFalse(ok))
}

func TestStructBuilderNonPresentNodeOmittedFromSyntax(t *testing.T) {
	var b structbuilder.Builder
	_, err := b.GetRef(cue.ParsePath(`b.c`))
	qt.Assert(t, qt.IsNil(err))
	_, err = b.GetRef(cue.ParsePath(`a.c.d`))
	qt.Assert(t, qt.IsNil(err))
	ok := b.Put(cue.ParsePath(`a.b`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: b: "hello"`)
}

func TestStructBuilderListIndex(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath(`a.b[1].c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: b: [_, {c: "hello"}]`)
}

func TestStructBuilderListIndexMultipleElements(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath(`a[2]`), ast.NewString("two"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath(`a[0].x`), ast.NewString("zero"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: [{x: "zero"}, _, "two"]`)
}

func TestStructBuilderListIndexNested(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath(`a[1][0]`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: [_, ["hello"]]`)
}

func TestStructBuilderListIndexRef(t *testing.T) {
	var b structbuilder.Builder
	ref, err := b.GetRef(cue.ParsePath(`a.b[1].c`))
	qt.Assert(t, qt.IsNil(err))
	ok := b.Put(cue.ParsePath(`a.b[1].c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath(`#x.y`), ref, nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: b: [_, {c: "hello"}]

#x: y: a.b[1].c
`)
}

func TestStructBuilderListIndexMixedWithField(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath(`a[0]`), ast.NewString("zero"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath(`a.b`), ast.NewString("bee"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	_, err := b.Syntax()
	qt.Assert(t, qt.ErrorMatches(err, `at path a: cannot mix list-index and field entries`))
}

func TestStructBuilderListIndexUnderValue(t *testing.T) {
	var b structbuilder.Builder
	ok := b.Put(cue.ParsePath(`a`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.Put(cue.ParsePath(`a[0]`), ast.NewString("zero"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	_, err := b.Syntax()
	qt.Assert(t, qt.ErrorMatches(err, `cannot combine value and list elements at path a`))
}

func TestStructBuilderBase(t *testing.T) {
	var b structbuilder.Builder
	addStructBuilderBase(t, &b, `
zed: "z"
alpha: {
	beta: 1
	gamma: [{delta: true, epsilon: _}, "x"]
}
empty: {}
`)
	ok := b.Put(cue.ParsePath(`alpha.gamma[0].epsilon.#`), ast.NewString("filled"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
zed: "z"

alpha: {
	beta: 1
	gamma: [
		{
			delta: true
			epsilon: #: "filled"
		},
		"x"
	]
}

empty: {}
`)
}

func TestStructBuilderBaseOrderPrecedesUnseeded(t *testing.T) {
	var b structbuilder.Builder
	addStructBuilderBase(t, &b, `
zebra:    1
aardvark: 2
`)
	ok := b.Put(cue.ParsePath(`#def`), ast.NewString("v"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
zebra: 1

aardvark: 2

#def: "v"
`)
}

func TestStructBuilderBaseUnfilledPlaceholder(t *testing.T) {
	var b structbuilder.Builder
	addStructBuilderBase(t, &b, `a: b: _`)
	assertStructBuilderSyntax(t, &b, `a: b: _`)
}

func TestStructBuilderBaseErrors(t *testing.T) {
	for _, test := range []struct {
		testName string
		base     string
		wantErr  string
		// langVersion pins the language version used to parse base, for
		// cases exercising syntax the latest version no longer accepts.
		langVersion string
	}{{
		testName: "Embedding",
		base:     `"just a value"`,
		wantErr:  `unsupported declaration type \*ast\.EmbedDecl in base data`,
	}, {
		testName: "OptionalField",
		base:     `a?: 5`,
		wantErr:  `unsupported field constraint "\?" in base data`,
	}, {
		testName: "DefinitionLabel",
		base:     `#a: 5`,
		wantErr:  `unsupported label "#a" in base data`,
	}, {
		testName: "PostfixAlias",
		base:     `a~(X): 5`,
		wantErr:  `unsupported field alias in base data`,
	}, {
		testName: "AliasLabel",
		base:     `X=a: 5`,
		wantErr:  `unsupported label type \*ast\.Alias in base data`,
		// The old prefix alias syntax is rejected from v0.18.0 on, where
		// @experiment(aliasv2) is stable, but *ast.Alias labels still
		// reach AddBase from modules pinning an earlier version.
		langVersion: "v0.17.0",
	}, {
		testName: "DuplicateValue",
		base: `
a: b: 5
a: b: 6
`,
		wantErr: `duplicate value in base data at a\.b`,
	}} {
		t.Run(test.testName, func(t *testing.T) {
			f, err := parser.ParseFile("base.cue", test.base,
				parser.Version(test.langVersion))
			qt.Assert(t, qt.IsNil(err))
			var b structbuilder.Builder
			qt.Assert(t, qt.ErrorMatches(b.AddBase(f), test.wantErr))
		})
	}
}

func addStructBuilderBase(t *testing.T, b *structbuilder.Builder, src string) {
	t.Helper()
	f, err := parser.ParseFile("base.cue", src)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(b.AddBase(f)))
}

func assertStructBuilderSyntax(t *testing.T, b *structbuilder.Builder, want string) {
	t.Helper()
	f, err := b.Syntax()
	qt.Assert(t, qt.IsNil(err))
	err = astutil.Sanitize(f)
	qt.Assert(t, qt.IsNil(err))
	data, err := format.Node(f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(strings.TrimSpace(string(data)), strings.TrimSpace(want)))
}
