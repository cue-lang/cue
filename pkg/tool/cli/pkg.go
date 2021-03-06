// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

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
