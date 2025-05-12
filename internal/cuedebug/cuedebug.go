package cuedebug

import (
	"sync"

	"cuelang.org/go/internal/envflag"
)

// Flags holds the set of global CUE_DEBUG flags. It is initialized by Init.
var Flags Config

// Config holds the set of known CUE_DEBUG flags.
//
// When adding, deleting, or modifying entries below,
// update cmd/cue/cmd/help.go as well for `cue help environment`.
type Config struct {
	// HTTP enables JSON logging per HTTP request and response made
	// when interacting with module registries.
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

	// OpenDef disables the check for closedness of definitions.
	OpenDef bool

	// ToolsFlow causes [cuelang.org/go/tools/flow] to print a task dependency mermaid graph.
	ToolsFlow bool

	// ParserTrace causes [cuelang.org/go/cue/parser] to print a trace of parsed productions.
	ParserTrace bool
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
