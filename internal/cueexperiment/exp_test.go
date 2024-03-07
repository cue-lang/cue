package cueexperiment

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var tests = []struct {
	testName      string
	cueExperiment string
	flagVal       *bool
	want          bool
	wantError     string
}{{
	testName:      "Empty",
	cueExperiment: "",
	flagVal:       &Flags.Modules,
	want:          false,
}, {
	testName:      "Unknown",
	cueExperiment: "foo",
	flagVal:       &Flags.Modules,
	wantError:     "unknown CUE_EXPERIMENT foo",
}, {
	testName:      "Set",
	cueExperiment: "modules",
	flagVal:       &Flags.Modules,
	want:          true,
}, {
	testName:      "SetTwice",
	cueExperiment: "modules,modules",
	flagVal:       &Flags.Modules,
	want:          true,
}, {
	testName:      "SetWithUnknown",
	cueExperiment: "modules,other",
	flagVal:       &Flags.Modules,
	wantError:     "unknown CUE_EXPERIMENT other",
}}

func TestInit(t *testing.T) {
	// This is just a smoke test to make sure it's all wired up OK.
	t.Setenv("CUE_EXPERIMENT", "modules")
	err := Init()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(Flags.Modules))
}
