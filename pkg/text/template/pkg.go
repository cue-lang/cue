// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package template

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal"
)

func init() {
	internal.Register("text/template", pkg)
}

var _ = adt.TopKind // in case the adt package isn't used

var pkg = &internal.Package{
	Native: []*internal.Builtin{{
		Name: "Execute",
		Params: []internal.Param{
			{Kind: adt.StringKind},
			{Kind: adt.TopKind},
		},
		Result: adt.StringKind,
		Func: func(c *internal.CallCtxt) {
			templ, data := c.String(0), c.Value(1)
			if c.Do() {
				c.Ret, c.Err = Execute(templ, data)
			}
		},
	}, {
		Name: "HTMLEscape",
		Params: []internal.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *internal.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = HTMLEscape(s)
			}
		},
	}, {
		Name: "JSEscape",
		Params: []internal.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *internal.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = JSEscape(s)
			}
		},
	}},
}
