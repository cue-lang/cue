// Copyright 2018 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:generate go run gen.go
//go:generate goimports -w builtins.go

package cue

import (
	"fmt"
	"io"
	"math/big"
	"path"
	"sort"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
)

// A builtin is a builtin function or constant.
//
// A function may return and a constant may be any of the following types:
//
//   error (translates to bottom)
//   nil   (translates to null)
//   bool
//   int*
//   uint*
//   float64
//   string
//   *big.Float
//   *big.Int
//
//   For any of the above, including interface{} and these types recursively:
//   []T
//   map[string]T
//
type builtin struct {
	baseValue
	Name   string
	Params []kind
	Result kind
	Func   func(c *callCtxt)
	// Const  interface{}
	Const string
}

type builtinPkg struct {
	native []*builtin
	cue    string
}

func mustCompileBuiltins(ctx *context, p *builtinPkg, name string) *structLit {
	obj := &structLit{}
	for _, b := range p.native {
		f := ctx.label(b.Name, false) // never starts with _
		// n := &node{baseValue: newBase(imp.Path)}
		var v evaluated = b
		if b.Const != "" {
			v = mustParseConstBuiltin(ctx, b.Name, b.Const)
		}
		obj.arcs = append(obj.arcs, arc{feature: f, v: v})
	}
	sort.Sort(obj)

	// Parse builtin CUE
	if p.cue != "" {
		expr, err := parser.ParseExpr(ctx.index.fset, name, p.cue)
		if err != nil {
			fmt.Println(p.cue)
			panic(err)
		}
		pkg := evalExpr(ctx.index, obj, expr).(*structLit)
		for _, a := range pkg.arcs {
			// Discard option status and attributes at top level.
			// TODO: filter on capitalized fields?
			obj.insertValue(ctx, a.feature, false, a.v, nil)
		}
	}

	return obj
}

// newConstBuiltin parses and creates any CUE expression that does not have
// fields.
func mustParseConstBuiltin(ctx *context, name, val string) evaluated {
	expr, err := parser.ParseExpr(ctx.index.fset, "<builtin:"+name+">", val)
	if err != nil {
		panic(err)
	}
	v := newVisitor(ctx.index, nil, nil, nil)
	value := v.walk(expr)
	return value.evalPartial(ctx)
}

var _ caller = &builtin{}

var lenBuiltin = &builtin{
	Name:   "len",
	Params: []kind{stringKind | bytesKind | listKind | structKind},
	Result: intKind,
	Func: func(c *callCtxt) {
		v := c.value(0)
		switch v.Kind() {
		case StructKind:
			s, _ := v.structVal(c.ctx)
			c.ret = s.Len()
		case ListKind:
			i := 0
			iter, _ := v.List()
			for ; iter.Next(); i++ {
			}
			c.ret = i
		case BytesKind:
			b, _ := v.Bytes()
			c.ret = len(b)
		case StringKind:
			s, _ := v.String()
			c.ret = len(s)
		}
	},
}

var andBuiltin = &builtin{
	Name:   "and",
	Params: []kind{listKind},
	Result: intKind,
	Func: func(c *callCtxt) {
		iter := c.list(0)
		if !iter.Next() {
			c.ret = &top{baseValue{c.src}}
			return
		}
		u := iter.Value().path.v
		for iter.Next() {
			u = mkBin(c.ctx, c.src.Pos(), opUnify, u, iter.Value().path.v)
		}
		c.ret = u
	},
}

var orBuiltin = &builtin{
	Name:   "or",
	Params: []kind{stringKind | bytesKind | listKind | structKind},
	Result: intKind,
	Func: func(c *callCtxt) {
		iter := c.list(0)
		d := []dValue{}
		for iter.Next() {
			d = append(d, dValue{iter.Value().path.v, false})
		}
		c.ret = &disjunction{baseValue{c.src}, d}
		if len(d) == 0 {
			c.ret = errors.New("empty or")
		}
	},
}

func (x *builtin) kind() kind {
	return lambdaKind
}

func (x *builtin) evalPartial(ctx *context) evaluated {
	return x
}

func (x *builtin) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	if y, ok := v.(*builtin); ok {
		return x == y
	}
	return false
}

func (x *builtin) call(ctx *context, src source, args ...evaluated) (ret value) {
	if x.Func == nil {
		return ctx.mkErr(x, "Builtin %q is not a function", x.Name)
	}
	if len(x.Params) != len(args) {
		return ctx.mkErr(src, x, "number of arguments does not match (%d vs %d)",
			len(x.Params), len(args))
	}
	for i, a := range args {
		if x.Params[i] != bottomKind {
			if unifyType(x.Params[i], a.kind()) == bottomKind {
				return ctx.mkErr(src, x, "argument %d requires type %v, found %v", i+1, x.Params[i], a.kind())
			}
		}
	}
	call := callCtxt{src: src, ctx: ctx, args: args}
	defer func() {
		var errVal interface{} = call.err
		if err := recover(); err != nil {
			errVal = err
		}
		switch err := errVal.(type) {
		case nil:
		case *bottom:
			ret = err
		default:
			ret = ctx.mkErr(src, x, "call error: %v", err)
		}
	}()
	x.Func(&call)
	if e, ok := call.ret.(value); ok {
		return e
	}
	return convert(ctx, x, call.ret)
}

// callCtxt is passed to builtin implementations.
type callCtxt struct {
	src  source
	ctx  *context
	args []evaluated
	err  error
	ret  interface{}
}

var builtins = map[string]*structLit{}

func initBuiltins(pkgs map[string]*builtinPkg) {
	ctx := sharedIndex.newContext()
	for k, b := range pkgs {
		e := mustCompileBuiltins(ctx, b, k)
		builtins[k] = e
		builtins["-/"+path.Base(k)] = e
	}
}

func getBuiltinShorthandPkg(ctx *context, shorthand string) *structLit {
	return getBuiltinPkg(ctx, "-/"+shorthand)
}

func getBuiltinPkg(ctx *context, path string) *structLit {
	p, ok := builtins[path]
	if !ok {
		return nil
	}
	return p
}

// do returns whether the call should be done.
func (c *callCtxt) do() bool {
	return c.err == nil
}

func (c *callCtxt) value(i int) Value {
	return newValueRoot(c.ctx, c.args[i])
}

func (c *callCtxt) int(i int) int     { return int(c.intValue(i, 64)) }
func (c *callCtxt) int8(i int) int8   { return int8(c.intValue(i, 8)) }
func (c *callCtxt) int16(i int) int16 { return int16(c.intValue(i, 16)) }
func (c *callCtxt) int32(i int) int32 { return int32(c.intValue(i, 32)) }
func (c *callCtxt) rune(i int) rune   { return rune(c.intValue(i, 32)) }
func (c *callCtxt) int64(i int) int64 { return int64(c.intValue(i, 64)) }

func (c *callCtxt) intValue(i, bits int) int64 {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil {
		c.err = c.ctx.mkErr(c.src, "argument %d must be in int, found number", i)
		return 0
	}
	if n.BitLen() > bits {
		c.err = c.ctx.mkErr(c.src, err, "argument %d out of range: has %d > %d bits", n.BitLen(), bits)
	}
	res, _ := x.Int64()
	return res
}

func (c *callCtxt) uint(i int) uint     { return uint(c.uintValue(i, 64)) }
func (c *callCtxt) uint8(i int) uint8   { return uint8(c.uintValue(i, 8)) }
func (c *callCtxt) byte(i int) uint8    { return byte(c.uintValue(i, 8)) }
func (c *callCtxt) uint16(i int) uint16 { return uint16(c.uintValue(i, 16)) }
func (c *callCtxt) uint32(i int) uint32 { return uint32(c.uintValue(i, 32)) }
func (c *callCtxt) uint64(i int) uint64 { return uint64(c.uintValue(i, 64)) }

func (c *callCtxt) uintValue(i, bits int) uint64 {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil {
		c.err = c.ctx.mkErr(c.src, "argument %d must be an integer", i)
		return 0
	}
	if n.Sign() < 0 {
		c.err = c.ctx.mkErr(c.src, "argument %d must be a positive integer", i)
		return 0
	}
	if n.BitLen() > bits {
		c.err = c.ctx.mkErr(c.src, err, "argument %d out of range: has %d > %d bits", i, n.BitLen(), bits)
	}
	res, _ := x.Uint64()
	return res
}

func (c *callCtxt) float64(i int) float64 {
	x := newValueRoot(c.ctx, c.args[i])
	res, err := x.Float64()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return 0
	}
	return res
}

func (c *callCtxt) bigInt(i int) *big.Int {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil {
		c.err = c.ctx.mkErr(c.src, "argument %d must be in int, found number", i)
		return nil
	}
	return n
}

func (c *callCtxt) bigFloat(i int) *big.Float {
	x := newValueRoot(c.ctx, c.args[i])
	var mant big.Int
	exp, err := x.MantExp(&mant)
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return nil
	}
	f := &big.Float{}
	f.SetInt(&mant)
	if exp != 0 {
		var g big.Float
		e := big.NewInt(int64(exp))
		f.Mul(f, g.SetInt(e.Exp(ten, e, nil)))
	}
	return f
}

func (c *callCtxt) string(i int) string {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.String()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return ""
	}
	return v
}

func (c *callCtxt) bytes(i int) []byte {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.Bytes()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return nil
	}
	return v
}

func (c *callCtxt) reader(i int) io.Reader {
	x := newValueRoot(c.ctx, c.args[i])
	// TODO: optimize for string and bytes cases
	r, err := x.Reader()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return nil
	}
	return r
}

func (c *callCtxt) bool(i int) bool {
	x := newValueRoot(c.ctx, c.args[i])
	b, err := x.Bool()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return false
	}
	return b
}

func (c *callCtxt) error(i int) error {
	x := newValueRoot(c.ctx, c.args[i])
	return x.Err()
}

func (c *callCtxt) list(i int) (a Iterator) {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.List()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return Iterator{ctx: c.ctx}
	}
	return v
}

func (c *callCtxt) strList(i int) (a []string) {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.List()
	if err != nil {
		c.err = c.ctx.mkErr(c.src, err, "invalid argument %d: %v", i, err)
		return nil
	}
	for i := 0; v.Next(); i++ {
		str, err := v.Value().String()
		if err != nil {
			c.err = c.ctx.mkErr(c.src, err, "list element %d: %v", i, err)
		}
		a = append(a, str)
	}
	return a
}
