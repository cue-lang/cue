// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

// Package cli provides tasks dealing with a console.
//
// These are the supported tasks:
//
//     // Print sends text to the stdout of the current process.
//     Print: {
//     	$id: *"tool/cli.Print" | "print" // for backwards compatibility
//
//     	// text is the text to be printed.
//     	text: string
//     }
//
//     // Ask prompts the current console with a message and waits for input.
//     //
//     // Example:
//     //     task: ask: cli.Ask({
//     //         prompt:   "Are you okay?"
//     //         response: bool
//     //     })
//     Ask: {
//     	$id: "tool/cli.Ask"
//
//     	// prompt sends this message to the output.
//     	prompt: string
//
//     	// response holds the user's response. If it is a boolean expression it
//     	// will interpret the answer using textual yes/ no.
//     	response: string | bool
//     }
//
package cli

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal"
)

func init() {
	internal.Register("tool/cli", pkg)
}

var _ = adt.TopKind // in case the adt package isn't used

var pkg = &internal.Package{
	Native: []*internal.Builtin{},
	CUE: `{
	Print: {
		$id:  *"tool/cli.Print" | "print"
		text: string
	}
	Ask: {
		$id:      "tool/cli.Ask"
		prompt:   string
		response: string | bool
	}
}`,
}
