package cueexperiment

import (
	"sync"

	"cuelang.org/go/internal/envflag"
)

// Flags returns the set of global CUE_EXPERIMENT flags.
// Repeated calls reuse the first result unless [InitAlways] is set.
func Flags() (Config, error) {
	if InitAlways {
		return flagsAlways()
	}
	return flagsOnce()
}

// InitAlways can be set in integration tests to ensure that [Flags]
// always parses the current environment variable.
var InitAlways bool

var flagsOnce = sync.OnceValues(flagsAlways)

func flagsAlways() (Config, error) {
	var cfg Config
	err := envflag.Init(&cfg, "CUE_EXPERIMENT")
	return cfg, err
}

// Config holds the set of known CUE_EXPERIMENT flags.
//
// When adding, deleting, or modifying entries below,
// update cmd/cue/cmd/help.go as well for `cue help environment`.
type Config struct {
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
