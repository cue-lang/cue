package cuedebug

import (
	"sync"

	"cuelang.org/go/internal/envflag"
)

// Flags holds the set of CUE_DEBUG flags. It is initialized by Init.
var Flags Config

type Config struct {
	HTTP bool

	// TODO: consider moving these evaluator-related options into a separate
	// struct, so that it can be used in an API. We should use embedding,
	// or some other mechanism, in that case to allow for the full set of
	// allowed environment variables to be known.

	// Strict sets whether extra aggressive checking should be done.
	// This should typically default to true for pre-releases and default to
	// false otherwise.
	Strict bool

	// LogEval sets the log level for the evaluator.
	// There are currently only two levels:
	//
	//	0: no logging
	//	1: logging
	LogEval int

	// Sharing enables structure sharing.
	Sharing bool `envflag:"default:true"`

	// SortFields forces fields in a struct to be sorted
	// lexicographically.
	SortFields bool

	// OpenInline permits disallowed fields to be selected into literal structs
	// that would normally result in a close error. For instance,
	//
	//    #D: {a: 1}
	//    x: (#D & {b: 2}).b // allow this
	//
	// This behavior was erroneously permitted in the v2 evaluator and was fixed
	// in v3. This allows users that rely on this behavior to use v3.
	// To aid the transition to v3, this is enabled by default for now.
	OpenInline bool `envflag:"default:true"`
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
	return envflag.Init(&Flags, "CUE_DEBUG")
})
