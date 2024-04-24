package cueexperiment

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestInit(t *testing.T) {
	// This is just a smoke test to make sure it's all wired up OK.

	// Check the default values.
	t.Setenv("CUE_EXPERIMENT", "")
	err := initAlways()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsFalse(Flags.Modules))
	qt.Assert(t, qt.IsTrue(Flags.YAMLV3Decoder))

	// Check that we can enable all experiments.
	t.Setenv("CUE_EXPERIMENT", "modules,yamlv3decoder")
	err = initAlways()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(Flags.Modules))
	qt.Assert(t, qt.IsTrue(Flags.YAMLV3Decoder))

	// Check that we can disable all experiments.
	t.Setenv("CUE_EXPERIMENT", "modules=0,yamlv3decoder=0")
	err = initAlways()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsFalse(Flags.Modules))
	qt.Assert(t, qt.IsFalse(Flags.YAMLV3Decoder))
}
