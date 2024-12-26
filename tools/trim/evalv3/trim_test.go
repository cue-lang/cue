package evalv3_test

import (
	"bytes"
	"testing"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	trimv3 "cuelang.org/go/tools/trim/evalv3"
	"github.com/go-quicktest/qt"
)

var trace = true

func TestTrim(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "trim",
		Matrix: cuetdtest.DevOnlyMatrix,
	}

	test.Run(t, func(t *cuetxtar.Test) {
		ctx := t.CueContext()
		inst := t.Instance()
		files := inst.Files
		val := ctx.BuildInstance(inst)

		cfg := &trimv3.Config{}
		var buf *bytes.Buffer
		if trace && testing.Verbose() {
			buf = new(bytes.Buffer)
			cfg.TraceWriter = buf
			// Uncomment if you find a test is panicking
			// cfg.TraceWriter = os.Stderr
		}

		err := trimv3.Files(files, inst.Dir, val, cfg)
		if buf != nil {
			t.Log("Trace:\n" + buf.String())
		}
		if err != nil {
			t.Fatal(err)
		}

		{
			a := build.NewContext().NewInstance("", nil)
			for _, file := range files {
				a.AddSyntax(file)
			}
			val = ctx.BuildInstance(a)
			qt.Check(t, qt.IsNil(val.Err()))
		}

		for _, f := range files {
			t.WriteFile(f)
		}
	})
}
