package cueexperiment

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Flags holds the set of global CUE_EXPERIMENT flags. It is initialized by Init.
var Flags Config

// Config holds the set of known CUE_EXPERIMENT flags.
//
// When adding, deleting, or modifying entries below,
// update cmd/cue/cmd/help.go as well for `cue help environment`.
type Config struct {
	// CmdReferencePkg requires referencing an imported tool package to declare tasks.
	// Otherwise, declaring tasks via "$id" or "kind" string fields is allowed.
	CmdReferencePkg bool `experiment:"preview:v0.13.0,default:v0.14.0"`

	// KeepValidators prevents validators from simplifying into concrete values,
	// even if their concrete value could be derived, such as '>=1 & <=1' to '1'.
	// Proposal:     https://cuelang.org/discussion/3775.
	// Spec change:  https://cuelang.org/cl/1217013
	// Spec change:  https://cuelang.org/cl/1217014
	KeepValidators bool `experiment:"preview:v0.14.0,default:v0.14.0,stable:v0.15.0"`

	// The flags below describe completed experiments; they can still be set
	// as long as the value aligns with the final behavior once the experiment finished.
	// Breaking users who set such a flag seems unnecessary,
	// and it simplifies using the same experiment flags across a range of CUE versions.

	// Modules enables support for the modules and package management proposal
	// as described in https://cuelang.org/discussion/2939.
	Modules bool `experiment:"preview:v0.8.0,default:v0.9.0,stable:v0.11.0"`

	// YAMLV3Decoder swaps the old internal/third_party/yaml decoder with the new
	// decoder implemented in internal/encoding/yaml on top of yaml.v3.
	YAMLV3Decoder bool `experiment:"preview:v0.9.0,default:v0.9.0,stable:v0.11.0"`

	// DecodeInt64 changes [cuelang.org/go/cue.Value.Decode] to choose
	// 'int64' rather than 'int' as the default type for CUE integer values
	// to ensure consistency with 32-bit platforms.
	DecodeInt64 bool `experiment:"preview:v0.11.0,default:v0.12.0,stable:v0.13.0"`

	// Embed enables support for embedded data files as described in
	// https://cuelang.org/discussion/3264.
	Embed bool `experiment:"preview:v0.10.0,default:v0.12.0,stable:v0.14.0"`

	// TopoSort enables topological sorting of struct fields.
	// Provide feedback via https://cuelang.org/issue/3558.
	TopoSort bool `experiment:"preview:v0.11.0,default:v0.12.0,stable:v0.14.0"`

	// EvalV3 enables the new CUE evaluator, addressing performance issues
	// and bringing better algorithms for disjunctions, closedness, and cycles.
	EvalV3 bool `experiment:"preview:v0.9.0,default:v0.13.0,stable:v0.15.0"`
}

// initExperimentFlags initializes the experiment flags by processing both
// the experiment lifecycle and environment variable overrides.
func initExperimentFlags() error {
	a := strings.Split(os.Getenv("CUE_EXPERIMENT"), ",")
	experiments, err := parseEnvExperiments(a...)
	if err != nil {
		return err
	}

	// First, set defaults based on experiment lifecycle
	if err := parseConfig(&Flags, "", experiments); err != nil {
		return fmt.Errorf("error in CUE_EXPERIMENT: %w", err)
	}
	return nil
}

// Init initializes Flags. Note: this isn't named "init" because we
// don't always want it to be called (for example we don't want it to be
// called when running "cue help"), and also because we want the failure
// mode to be one of error not panic, which would be the only option if
// it was a top level init function.
func Init() error {
	return initOnce()
}

var initOnce = sync.OnceValue(initExperimentFlags)
