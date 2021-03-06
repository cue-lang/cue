// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package csv

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal"
)

func init() {
	internal.Register("encoding/csv", pkg)
}

var _ = adt.TopKind // in case the adt package isn't used

var pkg = &internal.Package{
	Native: []*internal.Builtin{{
		Name: "Encode",
		Params: []internal.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.StringKind,
		Func: func(c *internal.CallCtxt) {
			x := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = Encode(x)
			}
		},
	}, {
		Name: "Decode",
		Params: []internal.Param{
			{Kind: adt.BytesKind | adt.StringKind},
		},
		Result: adt.ListKind,
		Func: func(c *internal.CallCtxt) {
			r := c.Reader(0)
			if c.Do() {
				c.Ret, c.Err = Decode(r)
			}
		},
	}},
}
