// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package sh

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("sh", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{{
		Name: "Format",
		Params: []pkg.Param{
			{Kind: adt.ListKind},
			{Kind: adt.ListKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			strs, args := c.List(0), c.List(1)
			if c.Do() {
				c.Ret, c.Err = Format(strs, args)
			}
		},
	}},
}
