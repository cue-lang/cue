package cueexperiment

import (
	"sync"

	"cuelang.org/go/internal/envflag"
)

// Flags holds the set of CUE_EXPERIMENT flags. It is initialized
// by Init.
var Flags struct {
	Modules bool
	EvalV3  bool
}

// Init initializes Flags. Note: this isn't named "init" because we
// don't always want it to be called (for example we don't want it to be
// called when running "cue help"), and also because we want the failure
// mode to be one of error not panic, which would be the only option if
// it was a top level init function.
func Init() error {
	return initOnce()
}

var initOnce = sync.OnceValue(func() error {
	return envflag.Init(&Flags, "CUE_EXPERIMENT")
})
