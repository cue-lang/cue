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
	qt.Assert(t, qt.IsTrue(Flags.Modules))
	qt.Assert(t, qt.IsTrue(Flags.YAMLV3Decoder))

	// Check that we can enable all experiments.
	t.Setenv("CUE_EXPERIMENT", "modules,yamlv3decoder")
	err = initAlways()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(Flags.Modules))
	qt.Assert(t, qt.IsTrue(Flags.YAMLV3Decoder))

	// Check that we cannot disable the YAML v3 experiment.
	t.Setenv("CUE_EXPERIMENT", "yamlv3decoder=0")
	err = initAlways()
	qt.Assert(t, qt.ErrorMatches(err, `cannot parse CUE_EXPERIMENT: cannot change default value of deprecated flag "yamlv3decoder"`))

	// Check that we cannot disable the modules experiment.
	t.Setenv("CUE_EXPERIMENT", "modules=0")
	err = initAlways()
	qt.Assert(t, qt.ErrorMatches(err, `cannot parse CUE_EXPERIMENT: cannot change default value of deprecated flag "modules"`))
}
