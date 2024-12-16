package cueexperiment

import (
	"sync"

	"cuelang.org/go/internal/envflag"
)

// Flags holds the set of global CUE_EXPERIMENT flags. It is initialized by Init.
//
// When adding, deleting, or modifying entries below,
// update cmd/cue/cmd/help.go as well for `cue help environment`.
var Flags struct {
	// TODO(mvdan): remove in December 2025; leaving it around for now
	// so that we delay breaking any users enabling this experiment.
	Modules bool `envflag:"deprecated,default:true"`

	// EvalV3 enables the new evaluator. The new evaluator addresses various
	// performance concerns.
	EvalV3 bool

	// Embed enables file embedding.
	Embed bool `envflag:"default:true"`

	// DecodeInt64 changes [cuelang.org/go/cue.Value.Decode] to choose
	// `int64` rather than `int` as the default type for CUE integer values
	// to ensure consistency with 32-bit platforms.
	DecodeInt64 bool `envflag:"default:true"`

	// Enable topological sorting of struct fields.
	TopoSort bool
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
