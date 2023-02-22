// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

package net

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("net", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{{
		Name: "SplitHostPort",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.ListKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret, c.Err = SplitHostPort(s)
			}
		},
	}, {
		Name: "JoinHostPort",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
			{Kind: adt.TopKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			host, port := c.Value(0), c.Value(1)
			if c.Do() {
				c.Ret, c.Err = JoinHostPort(host, port)
			}
		},
	}, {
		Name: "FQDN",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = FQDN(s)
			}
		},
	}, {
		Name:  "IPv4len",
		Const: "4",
	}, {
		Name:  "IPv6len",
		Const: "16",
	}, {
		Name: "ParseIP",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.ListKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret, c.Err = ParseIP(s)
			}
		},
	}, {
		Name: "IPv4",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = IPv4(ip)
			}
		},
	}, {
		Name: "IP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = IP(ip)
			}
		},
	}, {
		Name: "IPCIDR",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = IPCIDR(ip)
			}
		},
	}, {
		Name: "LoopbackIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = LoopbackIP(ip)
			}
		},
	}, {
		Name: "MulticastIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = MulticastIP(ip)
			}
		},
	}, {
		Name: "InterfaceLocalMulticastIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = InterfaceLocalMulticastIP(ip)
			}
		},
	}, {
		Name: "LinkLocalMulticastIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = LinkLocalMulticastIP(ip)
			}
		},
	}, {
		Name: "LinkLocalUnicastIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = LinkLocalUnicastIP(ip)
			}
		},
	}, {
		Name: "GlobalUnicastIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = GlobalUnicastIP(ip)
			}
		},
	}, {
		Name: "UnspecifiedIP",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.BoolKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret = UnspecifiedIP(ip)
			}
		},
	}, {
		Name: "ToIP4",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.ListKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = ToIP4(ip)
			}
		},
	}, {
		Name: "ToIP16",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.ListKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = ToIP16(ip)
			}
		},
	}, {
		Name: "IPString",
		Params: []pkg.Param{
			{Kind: adt.TopKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			ip := c.Value(0)
			if c.Do() {
				c.Ret, c.Err = IPString(ip)
			}
		},
	}, {
		Name: "PathEscape",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = PathEscape(s)
			}
		},
	}, {
		Name: "PathUnescape",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret, c.Err = PathUnescape(s)
			}
		},
	}, {
		Name: "QueryEscape",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret = QueryEscape(s)
			}
		},
	}, {
		Name: "QueryUnescape",
		Params: []pkg.Param{
			{Kind: adt.StringKind},
		},
		Result: adt.StringKind,
		Func: func(c *pkg.CallCtxt) {
			s := c.String(0)
			if c.Do() {
				c.Ret, c.Err = QueryUnescape(s)
			}
		},
	}},
}
