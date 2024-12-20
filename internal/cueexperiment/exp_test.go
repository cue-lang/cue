package cueexperiment

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestFlags(t *testing.T) {
	// This is just a smoke test to make sure it's all wired up OK.
	t.Setenv("CUE_EXPERIMENT", "evalv3,embed=0")
	experiment, err := Flags()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(experiment.EvalV3))
	qt.Assert(t, qt.IsFalse(experiment.Embed))
}
