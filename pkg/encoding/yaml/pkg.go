// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package yaml

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("encoding/yaml", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{{
		Name: "Marshal",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			v := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = Marshal(v)
			}
		},
	}, {
		Name: "MarshalStream",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			v := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = MarshalStream(v)
			}
		},
	}, {
		Name: "Unmarshal",
		Params: []pkg.Param{
			{Kind: adt.BytesKind | adt.StringKind},
		},
		Result: adt.TopKind,
		Func: func(c *pkg.CallCtxt) {
			data := c.Bytes(0)
			if c.Do() {
				c.Ret, c.Err = Unmarshal(data)
			}
		},
	}, {
		Name: "UnmarshalStream",
		Params: []pkg.Param{
			{Kind: adt.BytesKind | adt.StringKind},
		},
		Result: adt.TopKind,
		Func: func(c *pkg.CallCtxt) {
			data := c.Bytes(0)
			if c.Do() {
				c.Ret, c.Err = UnmarshalStream(data)
			}
		},
	}, {
		Name: "Validate",
		Params: []pkg.Param{
			{Kind: adt.BytesKind | adt.StringKind},
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			b, v := c.Bytes(0), c.Schema(1)
			if c.Do() {
				c.Ret, c.Err = Validate(b, v)
			}
		},
	}, {
		Name: "ValidatePartial",
		Params: []pkg.Param{
			{Kind: adt.BytesKind | adt.StringKind},
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			b, v := c.Bytes(0), c.Value(1)
			if c.Do() {
				c.Ret, c.Err = ValidatePartial(b, v)
			}
		},
	}},
}
