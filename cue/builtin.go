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
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"github.com/cockroachdb/apd/v2"
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
	pkg    label
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

func mustCompileBuiltins(ctx *context, p *builtinPkg, pkgName string) *structLit {
	obj := &structLit{}
	pkgLabel := ctx.label(pkgName, false)
	for _, b := range p.native {
		b.pkg = pkgLabel

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
		expr, err := parser.ParseExpr(pkgName, p.cue)
		if err != nil {
			panic(fmt.Errorf("could not parse %v: %v", p.cue, err))
		}
		pkg := evalExpr(ctx.index, obj, expr).(*structLit)
		for _, a := range pkg.arcs {
			// Discard option status and attributes at top level.
			// TODO: filter on capitalized fields?
			obj.insertValue(ctx, a.feature, false, false, a.v, nil, a.docs)
		}
	}

	return obj
}

// newConstBuiltin parses and creates any CUE expression that does not have
// fields.
func mustParseConstBuiltin(ctx *context, name, val string) evaluated {
	expr, err := parser.ParseExpr("<builtin:"+name+">", val)
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
		switch k := v.IncompleteKind(); k {
		case StructKind:
			s, err := v.structValData(c.ctx)
			if err != nil {
				c.ret = err
				break
			}
			c.ret = s.Len()
		case ListKind:
			i := 0
			iter, err := v.List()
			if err != nil {
				c.ret = err
				break
			}
			for ; iter.Next(); i++ {
			}
			c.ret = i
		case BytesKind:
			b, err := v.Bytes()
			if err != nil {
				c.ret = err
				break
			}
			c.ret = len(b)
		case StringKind:
			s, err := v.String()
			if err != nil {
				c.ret = err
				break
			}
			c.ret = len(s)
		default:
			c.ret = errors.Newf(token.NoPos,
				"invalid argument type %v", k)
		}
	},
}

var closeBuiltin = &builtin{
	Name:   "close",
	Params: []kind{structKind},
	Result: structKind,
	Func: func(c *callCtxt) {
		s, ok := c.args[0].(*structLit)
		if !ok {
			c.ret = errors.Newf(c.args[0].Pos(), "struct argument must be concrete")
			return
		}
		c.ret = s.close()
	},
}

var andBuiltin = &builtin{
	Name:   "and",
	Params: []kind{listKind},
	Result: intKind,
	Func: func(c *callCtxt) {
		iter := c.iter(0)
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
		iter := c.iter(0)
		d := []dValue{}
		for iter.Next() {
			d = append(d, dValue{iter.Value().path.v, false})
		}
		c.ret = &disjunction{baseValue{c.src}, d, nil, false}
		if len(d) == 0 {
			c.ret = errors.New("empty list in call to or")
		}
	},
}

func (x *builtin) representedKind() kind {
	if x.isValidator() {
		return x.Params[0]
	}
	return x.kind()
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

func (x *builtin) name(ctx *context) string {
	if x.pkg == 0 {
		return x.Name
	}
	return fmt.Sprintf("%s.%s", ctx.labelStr(x.pkg), x.Name)
}

func (x *builtin) isValidator() bool {
	return len(x.Params) == 1 && x.Result == boolKind
}

func convertBuiltin(v evaluated) evaluated {
	x, ok := v.(*builtin)
	if ok && x.isValidator() {
		return &customValidator{v.base(), []evaluated{}, x}
	}
	return v
}

func (x *builtin) call(ctx *context, src source, args ...evaluated) (ret value) {
	if x.Func == nil {
		return ctx.mkErr(x, "builtin %s is not a function", x.name(ctx))
	}
	if len(x.Params)-1 == len(args) && x.Result == boolKind {
		// We have a custom builtin
		return &customValidator{src.base(), args, x}
	}
	switch {
	case len(x.Params) < len(args):
		return ctx.mkErr(src, x, "too many arguments in call to %s (have %d, want %d)",
			x.name(ctx), len(args), len(x.Params))
	case len(x.Params) > len(args):
		return ctx.mkErr(src, x, "not enough arguments in call to %s (have %d, want %d)",
			x.name(ctx), len(args), len(x.Params))
	}
	for i, a := range args {
		if x.Params[i] != bottomKind {
			if unifyType(x.Params[i], a.kind()) == bottomKind {
				const msg = "cannot use %s (type %s) as %s in argument %d to %s"
				return ctx.mkErr(src, x, msg, ctx.str(a), a.kind(), x.Params[i], i+1, x.name(ctx))
			}
		}
	}
	call := callCtxt{src: src, ctx: ctx, builtin: x, args: args}
	defer func() {
		var errVal interface{} = call.err
		if err := recover(); err != nil {
			errVal = err
		}
		switch err := errVal.(type) {
		case nil:
		case *callError:
			ret = err.b
		default:
			// TODO: store the underlying error explicitly
			ret = ctx.mkErr(src, x, "error in call to %s: %v", x.name(ctx), err)
		}
	}()
	x.Func(&call)
	switch v := call.ret.(type) {
	case value:
		return v
	case *valueError:
		return v.err
	}
	return convert(ctx, x, false, call.ret)
}

// callCtxt is passed to builtin implementations.
type callCtxt struct {
	src     source
	ctx     *context
	builtin *builtin
	args    []evaluated
	err     error
	ret     interface{}
}

func (c *callCtxt) name() string {
	return c.builtin.name(c.ctx)
}

var builtins = map[string]*Instance{}

func initBuiltins(pkgs map[string]*builtinPkg) {
	ctx := sharedIndex.newContext()
	keys := []string{}
	for k := range pkgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := pkgs[k]
		e := mustCompileBuiltins(ctx, b, k)

		i := sharedIndex.addInst(&Instance{
			ImportPath: k,
			Name:       path.Base(k),
			rootStruct: e,
			rootValue:  e,
		})

		builtins[k] = i
		builtins["-/"+path.Base(k)] = i
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
	return p.rootStruct
}

func init() {
	internal.UnifyBuiltin = func(val interface{}, kind string) interface{} {
		v := val.(Value)
		ctx := v.ctx()

		p := strings.Split(kind, ".")
		pkg, name := p[0], p[1]
		s := getBuiltinPkg(ctx, pkg)
		if s == nil {
			return v
		}
		a := s.lookup(ctx, ctx.label(name, false))
		if a.v == nil {
			return v
		}

		return v.Unify(newValueRoot(ctx, a.v.evalPartial(ctx)))
	}
}

// do returns whether the call should be done.
func (c *callCtxt) do() bool {
	return c.err == nil
}

type callError struct {
	b *bottom
}

func (e *callError) Error() string {
	return fmt.Sprint(e.b)
}

func (c *callCtxt) errf(src source, underlying error, format string, args ...interface{}) {
	a := make([]interface{}, 0, 2+len(args))
	if err, ok := underlying.(*valueError); ok {
		a = append(a, err.err)
	}
	a = append(a, format)
	a = append(a, args...)
	err := c.ctx.mkErr(src, a...)
	c.err = &callError{err}
}

func (c *callCtxt) value(i int) Value {
	return newValueRoot(c.ctx, c.args[i])
}

func (c *callCtxt) structVal(i int) *Struct {
	v := newValueRoot(c.ctx, c.args[i])
	s, err := v.Struct()
	if err != nil {
		c.invalidArgType(c.args[i], i, "struct", err)
		return nil
	}
	return s
}

func (c *callCtxt) invalidArgType(arg value, i int, typ string, err error) {
	if err != nil {
		c.errf(c.src, err, "cannot use %s (type %s) as %s in argument %d to %s: %v",
			c.ctx.str(arg), arg.kind(), typ, i, c.name(), err)
	} else {
		c.errf(c.src, nil, "cannot use %s (type %s) as %s in argument %d to %s",
			c.ctx.str(arg), arg.kind(), typ, i, c.name())
	}
}

func (c *callCtxt) int(i int) int     { return int(c.intValue(i, 64, "int64")) }
func (c *callCtxt) int8(i int) int8   { return int8(c.intValue(i, 8, "int8")) }
func (c *callCtxt) int16(i int) int16 { return int16(c.intValue(i, 16, "int16")) }
func (c *callCtxt) int32(i int) int32 { return int32(c.intValue(i, 32, "int32")) }
func (c *callCtxt) rune(i int) rune   { return rune(c.intValue(i, 32, "rune")) }
func (c *callCtxt) int64(i int) int64 { return int64(c.intValue(i, 64, "int64")) }

func (c *callCtxt) intValue(i, bits int, typ string) int64 {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	n, err := x.Int(nil)
	if err != nil {
		c.invalidArgType(arg, i, typ, err)
		return 0
	}
	if n.BitLen() > bits {
		c.errf(c.src, err, "int %s overflows %s in argument %d in call to %s",
			n, typ, i, c.name())
	}
	res, _ := x.Int64()
	return res
}

func (c *callCtxt) uint(i int) uint     { return uint(c.uintValue(i, 64, "uint64")) }
func (c *callCtxt) uint8(i int) uint8   { return uint8(c.uintValue(i, 8, "uint8")) }
func (c *callCtxt) byte(i int) uint8    { return byte(c.uintValue(i, 8, "byte")) }
func (c *callCtxt) uint16(i int) uint16 { return uint16(c.uintValue(i, 16, "uint16")) }
func (c *callCtxt) uint32(i int) uint32 { return uint32(c.uintValue(i, 32, "uint32")) }
func (c *callCtxt) uint64(i int) uint64 { return uint64(c.uintValue(i, 64, "uint64")) }

func (c *callCtxt) uintValue(i, bits int, typ string) uint64 {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil || n.Sign() < 0 {
		c.invalidArgType(c.args[i], i, typ, err)
		return 0
	}
	if n.BitLen() > bits {
		c.errf(c.src, err, "int %s overflows %s in argument %d in call to %s",
			n, typ, i, c.name())
	}
	res, _ := x.Uint64()
	return res
}

func (c *callCtxt) decimal(i int) *apd.Decimal {
	x := newValueRoot(c.ctx, c.args[i])
	if _, err := x.MantExp(nil); err != nil {
		c.invalidArgType(c.args[i], i, "Decimal", err)
		return nil
	}
	return &c.args[i].(*numLit).v
}

func (c *callCtxt) float64(i int) float64 {
	x := newValueRoot(c.ctx, c.args[i])
	res, err := x.Float64()
	if err != nil {
		c.invalidArgType(c.args[i], i, "float64", err)
		return 0
	}
	return res
}

func (c *callCtxt) bigInt(i int) *big.Int {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil {
		c.invalidArgType(c.args[i], i, "int", err)
		return nil
	}
	return n
}

func (c *callCtxt) bigFloat(i int) *big.Float {
	x := newValueRoot(c.ctx, c.args[i])
	var mant big.Int
	exp, err := x.MantExp(&mant)
	if err != nil {
		c.invalidArgType(c.args[i], i, "float", err)
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
		c.invalidArgType(c.args[i], i, "string", err)
		return ""
	}
	return v
}

func (c *callCtxt) bytes(i int) []byte {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.Bytes()
	if err != nil {
		c.invalidArgType(c.args[i], i, "bytes", err)
		return nil
	}
	return v
}

func (c *callCtxt) reader(i int) io.Reader {
	x := newValueRoot(c.ctx, c.args[i])
	// TODO: optimize for string and bytes cases
	r, err := x.Reader()
	if err != nil {
		c.invalidArgType(c.args[i], i, "bytes|string", err)
		return nil
	}
	return r
}

func (c *callCtxt) bool(i int) bool {
	x := newValueRoot(c.ctx, c.args[i])
	b, err := x.Bool()
	if err != nil {
		c.invalidArgType(c.args[i], i, "bool", err)
		return false
	}
	return b
}

func (c *callCtxt) list(i int) (a []Value) {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	v, err := x.List()
	if err != nil {
		c.invalidArgType(c.args[i], i, "list", err)
		return a
	}
	for v.Next() {
		a = append(a, v.Value())
	}
	return a
}

func (c *callCtxt) iter(i int) (a Iterator) {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	v, err := x.List()
	if err != nil {
		c.invalidArgType(c.args[i], i, "list", err)
		return Iterator{ctx: c.ctx}
	}
	return v
}

func (c *callCtxt) decimalList(i int) (a []*apd.Decimal) {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	v, err := x.List()
	if err != nil {
		c.invalidArgType(c.args[i], i, "list", err)
		return nil
	}
	for j := 0; v.Next(); j++ {
		num, err := v.Value().getNum(numKind)
		if err != nil {
			c.errf(c.src, err, "invalid list element %d in argument %d to %s: %v",
				j, i, c.name(), err)
			break
		}
		a = append(a, &num.v)
	}
	return a
}

func (c *callCtxt) strList(i int) (a []string) {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	v, err := x.List()
	if err != nil {
		c.invalidArgType(c.args[i], i, "list", err)
		return nil
	}
	for j := 0; v.Next(); j++ {
		str, err := v.Value().String()
		if err != nil {
			c.errf(c.src, err, "invalid list element %d in argument %d to %s: %v",
				j, i, c.name(), err)
			break
		}
		a = append(a, str)
	}
	return a
}
