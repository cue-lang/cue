package cueexperiment

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestInit(t *testing.T) {
	// This is just a smoke test to make sure it's all wired up OK.
	t.Setenv("CUE_EXPERIMENT", "evalv3,embed=0")
	err := Init()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(Flags.EvalV3))
	qt.Assert(t, qt.IsFalse(Flags.Embed))
}
