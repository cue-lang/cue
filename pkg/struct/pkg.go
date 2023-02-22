// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package structs

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("struct", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{{
		Name: "MinFields",
		Params: []pkg.Param{
			{Kind: adt.StructKind},
			{Kind: adt.IntKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			object, n := c.Struct(0), c.Int(1)
			if c.Do() {
				c.Ret, c.Err = MinFields(object, n)
			}
		},
	}, {
		Name: "MaxFields",
		Params: []pkg.Param{
			{Kind: adt.StructKind},
			{Kind: adt.IntKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			object, n := c.Struct(0), c.Int(1)
			if c.Do() {
				c.Ret, c.Err = MaxFields(object, n)
			}
		},
	}},
}
