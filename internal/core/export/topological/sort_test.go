package topological_test

import (
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
)

func TestTopologicalSort(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "testdata",
		Name:   "topological",
		Matrix: cuetdtest.SmallMatrix,
	}

	test.Run(t, func(t *cuetxtar.Test) {
		run := t.Runtime()
		inst := t.Instance()

		v, err := run.Build(nil, inst)
		if err != nil {
			t.Fatal(err)
		}

		v.Finalize(eval.NewContext(run, v))

		evalWithOptions := export.Profile{
			TakeDefaults:    true,
			ShowOptional:    true,
			ShowDefinitions: true,
			ShowAttributes:  true,
		}

		expr, err := evalWithOptions.Value(run, inst.ID(), v)
		if err != nil {
			t.Fatal(err)
		}

		{
			b, err := format.Node(expr)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = t.Write(b)
		}
	})
}
