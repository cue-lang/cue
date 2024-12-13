package jsonschema

import (
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
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
	assertStructBuilderSyntax(t, &b, `#bar: #foo: xxx: #foo_1.bar.baz

#foo_1=#foo: bar: baz: "hello"
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
_schema: {
	next?: _schema
}
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
#bar: #foo: xxx: #foo_1."a b".baz

#foo_1=#foo: "a b": baz: "hello"
`)
}

func TestStructBuilderNonIdentifierStringNodeAtRoot(t *testing.T) {
	var b structBuilder
	_, err := b.getRef(cue.ParsePath(`"a b".baz`))
	qt.Assert(t, qt.ErrorMatches(err, `initial element of path "\\"a b\\"\.baz" cannot be expressed as an identifier`))
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

func assertStructBuilderSyntax(t *testing.T, b *structBuilder, want string) {
	f, err := b.syntax()
	qt.Assert(t, qt.IsNil(err))
	err = astutil.Sanitize(f)
	qt.Assert(t, qt.IsNil(err))
	data, err := format.Node(f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(strings.TrimSpace(string(data)), strings.TrimSpace(want)))
}
