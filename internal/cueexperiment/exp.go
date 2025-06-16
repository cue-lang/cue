package cueexperiment

import (
	"sync"

	"cuelang.org/go/internal/envflag"
)

// Flags holds the set of global CUE_EXPERIMENT flags. It is initialized by Init.
var Flags Config

// Config holds the set of known CUE_EXPERIMENT flags.
//
// When adding, deleting, or modifying entries below,
// update cmd/cue/cmd/help.go as well for `cue help environment`.
type Config struct {
	// EvalV3 enables the new CUE evaluator, addressing performance issues
	// and bringing better algorithms for disjunctions, closedness, and cycles.
	//
	// This experiment was introduced in v0.9.0 (2024-06),
	// and enabled by default in v0.13.0 (2025-05).
	EvalV3 bool `envflag:"default:true"`

	// CmdReferencePkg requires referencing an imported tool package to declare tasks.
	// Otherwise, declaring tasks via "$id" or "kind" string fields is allowed.
	//
	// This experiment was introduced in v0.13.0 (2025-05),
	// and enabled by default in the upcoming v0.14 release.
	CmdReferencePkg bool `envflag:"default:true"`

	// KeepValidators prevents validators from simplifying into concrete values,
	// even if their concrete value could be derived, such as `>=1 & <=1` to `1`.
	// See the proposal at https://cuelang.org/discussion/3775.
	//
	// This experiment is introduced in the upcoming v0.14 release, already on by default.
	KeepValidators bool `envflag:"default:true"`

	// The flags below describe completed experiments; they can still be set
	// as long as the value aligns with the final behavior once the experiment finished.
	// Breaking users who set such a flag seems unnecessary,
	// and it simplifies using the same experiment flags across a range of CUE versions.

	// Modules enables support for the modules and package management proposal
	// as described in https://cuelang.org/discussion/2939.
	//
	// This experiment was introduced in v0.8.0 (2024-03),
	// enabled by default in v0.9.0 (2024-06),
	// and deprecated in v0.11.0 (2024-11).
	Modules bool `envflag:"deprecated,default:true"`

	// YAMLV3Decoder swaps the old internal/third_party/yaml decoder with the new
	// decoder implemented in internal/encoding/yaml on top of yaml.v3.
	//
	// This experiment was introduced in v0.9.0 (2024-06), already on by default,
	// and was deprecated in v0.11.0 (2024-11).
	YAMLV3Decoder bool `envflag:"deprecated,default:true"`

	// DecodeInt64 changes [cuelang.org/go/cue.Value.Decode] to choose
	// `int64` rather than `int` as the default type for CUE integer values
	// to ensure consistency with 32-bit platforms.
	//
	// This experiment was introduced in v0.11.0 (2024-11),
	// enabled by default in v0.12.0 (2025-01),
	// and was deprecated in v0.13.0 (2025-05).
	DecodeInt64 bool `envflag:"deprecated,default:true"`

	// Embed enables support for embedded data files as described in
	// https://cuelang.org/discussion/3264.
	//
	// This experiment was introduced in v0.10.0 (2024-08),
	// enabled by default in v0.12.0 (2025-01),
	// and deprecated in the upcoming v0.14 release.
	Embed bool `envflag:"deprecated,default:true"`

	// TopoSort enables topological sorting of struct fields.
	// Provide feedback via https://cuelang.org/issue/3558.
	//
	// This experiment was introduced in v0.11.0 (2024-11)
	// enabled by default in v0.12.0 (2025-01),
	// and deprecated in the upcoming v0.14 release.
	TopoSort bool `envflag:"deprecated,default:true"`
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
