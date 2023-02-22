// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package html

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("html", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{{
		Name: "Escape",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = Escape(s)
			}
		},
	}, {
		Name: "Unescape",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = Unescape(s)
			}
		},
	}},
}
