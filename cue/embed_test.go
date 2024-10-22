package cue_test

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/interpreter/embed"
	"github.com/go-quicktest/qt"
)

func TestEmbedFailsWhenNotInModule(t *testing.T) {
	ctx := cuecontext.New(cuecontext.Interpreter(embed.New()))
	v := ctx.CompileString(`
@extern(embed)

package foo
x: _ 	@embed(file="testdata/readme.md",type=text)
`)
	qt.Assert(t, qt.IsNil(v.Err()))
	// TODO qt.Assert(t, qt.ErrorMatches(v.Err(), `xxx`))
}
