package cueexperiment

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestInit(t *testing.T) {
	// This is just a smoke test to make sure it's all wired up OK.
	t.Setenv("CUE_EXPERIMENT", "cmdreferencepkg,keepvalidators=0")
	err := Init()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(Flags.CmdReferencePkg))
	qt.Assert(t, qt.IsFalse(Flags.KeepValidators))
}
