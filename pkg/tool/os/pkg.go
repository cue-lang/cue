// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

// Package os defines tasks for retrieving os-related information.
//
// CUE definitions:
//
//	// A Value are all possible values allowed in flags.
//	// A null value unsets an environment variable.
//	Value: bool | number | *string | null
//
//	// Name indicates a valid flag name.
//	Name: !="" & !~"^[$]"
//
//	// Setenv defines a set of command line flags, the values of which will be set
//	// at run time. The doc comment of the flag is presented to the user in help.
//	//
//	// To define a shorthand, define the shorthand as a new flag referring to
//	// the flag of which it is a shorthand.
//	Setenv: {
//		$id: "tool/os.Setenv"
//
//		{[Name]: Value}
//	}
//
//	// Getenv gets and parses the specific command line variables.
//	Getenv: {
//		$id: "tool/os.Getenv"
//
//		{[Name]: Value}
//	}
//
//	// Environ populates a struct with all environment variables.
//	Environ: {
//		$id: "tool/os.Environ"
//
//		// A map of all populated values.
//		// Individual entries may be specified ahead of time to enable
//		// validation and parsing. Values that are marked as required
//		// will fail the task if they are not found.
//		{[Name]: Value}
//	}
//
//	// Clearenv clears all environment variables.
//	Clearenv: {
//		$id: "tool/os.Clearenv"
//	}
package os

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("tool/os", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{},
	CUE: `{
	Value: bool | number | *string | null
	Name:  !="" & !~"^[$]"
	Setenv: {
		{
			[Name]: Value
		}
		$id: "tool/os.Setenv"
	}
	Getenv: {
		{
			[Name]: Value
		}
		$id: "tool/os.Getenv"
	}
	Environ: {
		{
			[Name]: Value
		}
		$id: "tool/os.Environ"
	}
	Clearenv: {
		$id: "tool/os.Clearenv"
	}
}`,
}
