// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package base64

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("encoding/base64", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{{
		Name: "EncodedLen",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
			{Kind: adt.IntKind},
		},
		Result: adt.IntKind,
		Func: func(c *pkg.CallCtxt) {
			encoding, n := c.Value(0), c.Int(1)
			if c.Do() {
				c.Ret, c.Err = EncodedLen(encoding, n)
			}
		},
	}, {
		Name: "DecodedLen",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
			{Kind: adt.IntKind},
		},
		Result: adt.IntKind,
		Func: func(c *pkg.CallCtxt) {
			encoding, x := c.Value(0), c.Int(1)
			if c.Do() {
				c.Ret, c.Err = DecodedLen(encoding, x)
			}
		},
	}, {
		Name: "Encode",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
			{Kind: adt.BytesKind | adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			encoding, src := c.Value(0), c.Bytes(1)
			if c.Do() {
				c.Ret, c.Err = Encode(encoding, src)
			}
		},
	}, {
		Name: "Decode",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
			{Kind: adt.StringKind},
		},
		Result: adt.BytesKind | adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			encoding, s := c.Value(0), c.String(1)
			if c.Do() {
				c.Ret, c.Err = Decode(encoding, s)
			}
		},
	}},
}