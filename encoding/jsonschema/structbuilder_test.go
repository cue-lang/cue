package jsonschema

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
	"cuelang.org/go/internal"
)

func TestStructBuilderShadowedRef(t *testing.T) {
	var b structBuilder
	ref, err := b.getRef(cue.ParsePath("#foo.bar.baz"))
	qt.Assert(t, qt.IsNil(err))
	ok := b.put(cue.ParsePath("#foo.bar.baz"), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath("#bar.#foo.xxx"), ref, nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `#bar: #foo: xxx: #foo_9.bar.baz

#foo_9=#foo: bar: baz: "hello"
`)
}

func TestStructBuilderSelfRef(t *testing.T) {
	var b structBuilder
	ref, err := b.getRef(cue.Path{})
	qt.Assert(t, qt.IsNil(err))
	ok := b.put(cue.Path{}, ast.NewStruct(ast.NewIdent("next"), token.OPTION, ref), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
_schema
_schema: next?: _schema
`)
}

func TestStructBuilderEntryInsideValue(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath("#foo"), ast.NewString("hello"), internal.NewComment(true, "foo comment"))
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath("#foo.#bar.#baz"), ast.NewString("goodbye"), internal.NewComment(true, "baz comment"))
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
	var b structBuilder
	ref, err := b.getRef(cue.ParsePath(`#foo."a b".baz`))
	qt.Assert(t, qt.IsNil(err))
	ok := b.put(cue.ParsePath(`#foo."a b".baz`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath("#bar.#foo.xxx"), ref, nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
#bar: #foo: xxx: #foo_9."a b".baz

#foo_9=#foo: "a b": baz: "hello"
`)
}

func TestStructBuilderNonIdentifierStringNodeAtRoot(t *testing.T) {
	var b structBuilder
	_, err := b.getRef(cue.ParsePath(`"a b".baz`))
	qt.Assert(t, qt.ErrorMatches(err, `initial element of path "\\"a b\\"\.baz" must be expressed as an identifier`))
}

func TestStructBuilderRedefinition(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath(`a.b.c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath(`a.b.c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsFalse(ok))
}

func TestStructBuilderNonPresentNodeOmittedFromSyntax(t *testing.T) {
	var b structBuilder
	_, err := b.getRef(cue.ParsePath(`b.c`))
	qt.Assert(t, qt.IsNil(err))
	_, err = b.getRef(cue.ParsePath(`a.c.d`))
	qt.Assert(t, qt.IsNil(err))
	ok := b.put(cue.ParsePath(`a.b`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: b: "hello"`)
}

func TestStructBuilderListIndex(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath(`a.b[1].c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: b: [_, {c: "hello"}]`)
}

func TestStructBuilderListIndexMultipleElements(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath(`a[2]`), ast.NewString("two"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath(`a[0].x`), ast.NewString("zero"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: [{x: "zero"}, _, "two"]`)
}

func TestStructBuilderListIndexNested(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath(`a[1][0]`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: [_, ["hello"]]`)
}

func TestStructBuilderListIndexRef(t *testing.T) {
	var b structBuilder
	ref, err := b.getRef(cue.ParsePath(`a.b[1].c`))
	qt.Assert(t, qt.IsNil(err))
	ok := b.put(cue.ParsePath(`a.b[1].c`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath(`#x.y`), ref, nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `a: b: [_, {c: "hello"}]

#x: y: a.b[1].c
`)
}

func TestStructBuilderListIndexMixedWithField(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath(`a[0]`), ast.NewString("zero"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath(`a.b`), ast.NewString("bee"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	_, err := b.syntax()
	qt.Assert(t, qt.ErrorMatches(err, `at path a: cannot mix list-index and field entries`))
}

func TestStructBuilderListIndexUnderValue(t *testing.T) {
	var b structBuilder
	ok := b.put(cue.ParsePath(`a`), ast.NewString("hello"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	ok = b.put(cue.ParsePath(`a[0]`), ast.NewString("zero"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	_, err := b.syntax()
	qt.Assert(t, qt.ErrorMatches(err, `cannot combine value and list elements at path a`))
}

func TestStructBuilderBase(t *testing.T) {
	var b structBuilder
	addStructBuilderBase(t, &b, `
zed: "z"
alpha: {
	beta: 1
	gamma: [{delta: true, epsilon: _}, "x"]
}
empty: {}
`)
	ok := b.put(cue.ParsePath(`alpha.gamma[0].epsilon.#`), ast.NewString("filled"), nil)
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
	var b structBuilder
	addStructBuilderBase(t, &b, `
zebra:    1
aardvark: 2
`)
	ok := b.put(cue.ParsePath(`#def`), ast.NewString("v"), nil)
	qt.Assert(t, qt.IsTrue(ok))
	assertStructBuilderSyntax(t, &b, `
zebra: 1

aardvark: 2

#def: "v"
`)
}

func TestStructBuilderBaseUnfilledPlaceholder(t *testing.T) {
	var b structBuilder
	addStructBuilderBase(t, &b, `a: b: _`)
	assertStructBuilderSyntax(t, &b, `a: b: _`)
}

func TestStructBuilderBaseErrors(t *testing.T) {
	for _, test := range []struct {
		testName string
		base     string
		wantErr  string
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
		testName: "AliasLabel",
		base:     `X=a: 5`,
		wantErr:  `unsupported label type \*ast\.Alias in base data`,
	}, {
		testName: "DuplicateValue",
		base: `
a: b: 5
a: b: 6
`,
		wantErr: `duplicate value in base data at a\.b`,
	}} {
		t.Run(test.testName, func(t *testing.T) {
			f, err := parser.ParseFile("base.cue", test.base)
			qt.Assert(t, qt.IsNil(err))
			var b structBuilder
			qt.Assert(t, qt.ErrorMatches(b.addBase(f), test.wantErr))
		})
	}
}

func addStructBuilderBase(t *testing.T, b *structBuilder, src string) {
	f, err := parser.ParseFile("base.cue", src)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(b.addBase(f)))
}

func assertStructBuilderSyntax(t *testing.T, b *structBuilder, want string) {
	f, err := b.syntax()
	qt.Assert(t, qt.IsNil(err))
	err = astutil.Sanitize(f)
	qt.Assert(t, qt.IsNil(err))
	data, err := format.Node(f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(strings.TrimSpace(string(data)), strings.TrimSpace(want)))
}
