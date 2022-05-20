// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package os

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal"
)

func init() {
	internal.Register("tool/os", pkg)
}

var _ = adt.TopKind // in case the adt package isn't used

var pkg = &internal.Package{
	Native: []*internal.Builtin{},
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
