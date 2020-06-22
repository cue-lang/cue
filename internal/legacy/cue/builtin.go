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
//go:generate go run golang.org/x/tools/cmd/goimports -w -local cuelang.org/go builtins.go
//go:generate gofmt -s -w builtins.go

package cue

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"path"
	"sort"
	"strings"

	"github.com/cockroachdb/apd/v2"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/runtime"
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

func mustCompileBuiltins(ctx *context, p *builtinPkg, pkgName string) *adt.Vertex {
	obj := &adt.Vertex{}
	pkgLabel := ctx.Label(pkgName, false)
	st := &adt.StructLit{}
	if len(p.native) > 0 {
		obj.AddConjunct(adt.MakeConjunct(nil, st))
	}
	for _, b := range p.native {
		b.pkg = pkgLabel

		f := ctx.Label(b.Name, false) // never starts with _
		// n := &node{baseValue: newBase(imp.Path)}
		var v adt.Expr = toBuiltin(ctx, b)
		if b.Const != "" {
			v = mustParseConstBuiltin(ctx, b.Name, b.Const)
		}
		st.Decls = append(st.Decls, &adt.Field{
			Label: f,
			Value: v,
		})
	}

	// Parse builtin CUE
	if p.cue != "" {
		expr, err := parser.ParseExpr(pkgName, p.cue)
		if err != nil {
			panic(fmt.Errorf("could not parse %v: %v", p.cue, err))
		}
		c, err := compile.Expr(nil, ctx.opCtx.Runtime, expr)
		if err != nil {
			panic(fmt.Errorf("could compile parse %v: %v", p.cue, err))
		}
		obj.AddConjunct(c)
	}

	// We could compile lazily, but this is easier for debugging.
	obj.Finalize(ctx.opCtx)
	if err := obj.Err(ctx.opCtx, adt.Finalized); err != nil {
		panic(err.Err)
	}

	return obj
}

func toBuiltin(ctx *context, b *builtin) *adt.Builtin {
	x := &adt.Builtin{
		Params:  b.Params,
		Result:  b.Result,
		Package: b.pkg,
		Name:    b.Name,
	}
	x.Func = func(ctx *adt.OpContext, args []adt.Value) (ret adt.Expr) {
		runtime := ctx.Impl().(*runtime.Runtime)
		index := runtime.Data.(*index)

		// call, _ := ctx.Source().(*ast.CallExpr)
		c := &callCtxt{
			idx: index,
			// src:  call,
			ctx:     index.newContext(),
			args:    args,
			builtin: b,
		}
		defer func() {
			var errVal interface{} = c.err
			if err := recover(); err != nil {
				errVal = err
			}
			ret = processErr(c, errVal, ret)
		}()
		b.Func(c)
		switch v := c.ret.(type) {
		case adt.Value:
			return v
		case *valueError:
			return v.err
		}
		if c.err != nil {
			return nil
		}
		return convert.GoValueToValue(ctx, c.ret, true)
	}
	return x
}

// newConstBuiltin parses and creates any CUE expression that does not have
// fields.
func mustParseConstBuiltin(ctx *context, name, val string) adt.Expr {
	expr, err := parser.ParseExpr("<builtin:"+name+">", val)
	if err != nil {
		panic(err)
	}
	c, err := compile.Expr(nil, ctx.Runtime, expr)
	if err != nil {
		panic(err)
	}
	return c.Expr()

}

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

func pos(n adt.Node) (p token.Pos) {
	if n == nil {
		return
	}
	src := n.Source()
	if src == nil {
		return
	}
	return src.Pos()
}

var closeBuiltin = &builtin{
	Name:   "close",
	Params: []kind{structKind},
	Result: structKind,
	Func: func(c *callCtxt) {
		s, ok := c.args[0].(*adt.Vertex)
		if !ok {
			c.ret = errors.Newf(pos(c.args[0]), "struct argument must be concrete")
			return
		}
		if s.IsClosed(c.ctx.opCtx) {
			c.ret = s
		} else {
			v := *s
			v.Closed = nil // TODO: set dedicated Closer.
			c.ret = &v
		}
	},
}

var andBuiltin = &builtin{
	Name:   "and",
	Params: []kind{listKind},
	Result: intKind,
	Func: func(c *callCtxt) {
		iter := c.iter(0)
		if !iter.Next() {
			c.ret = &top{}
			return
		}
		var u adt.Expr = iter.Value().v
		for iter.Next() {
			u = &adt.BinaryExpr{Op: adt.AndOp, X: u, Y: iter.Value().v}
		}
		c.ret = u
	},
}

var orBuiltin = &builtin{
	Name:   "or",
	Params: []kind{listKind},
	Result: intKind,
	Func: func(c *callCtxt) {
		iter := c.iter(0)
		d := []adt.Disjunct{}
		for iter.Next() {
			d = append(d, adt.Disjunct{iter.Value().v, false})
		}
		c.ret = &adt.DisjunctionExpr{nil, d, false}
		if len(d) == 0 {
			// TODO(manifest): This should not be unconditionally incomplete,
			// but it requires results from comprehensions and all to have
			// some special status. Maybe this can be solved by having results
			// of list comprehensions be open if they result from iterating over
			// an open list or struct. This would actually be exactly what
			// that means. The error here could then only add an incomplete
			// status if the source is open.
			c.ret = &adt.Bottom{
				Code: adt.IncompleteError,
				Err:  errors.Newf(c.Pos(), "empty list in call to or"),
			}
		}
	},
}

func (x *builtin) name(ctx *context) string {
	if x.pkg == 0 {
		return x.Name
	}
	return fmt.Sprintf("%s.%s", ctx.LabelStr(x.pkg), x.Name)
}

func (x *builtin) isValidator() bool {
	return len(x.Params) == 1 && x.Result == boolKind
}

func processErr(call *callCtxt, errVal interface{}, ret adt.Expr) adt.Expr {
	ctx := call.ctx
	src := call.src
	switch err := errVal.(type) {
	case nil:
	case *callError:
		ret = err.b
	case *json.MarshalerError:
		if err, ok := err.Err.(*marshalError); ok && err.b != nil {
			ret = err.b
		}
	case *marshalError:
		ret = wrapCallErr(call, err.b)
	case *valueError:
		ret = wrapCallErr(call, err.err)
	case errors.Error:
		ret = wrapCallErr(call, &adt.Bottom{Err: err})
	case error:
		if call.err == internal.ErrIncomplete {
			ret = ctx.mkErr(src, codeIncomplete, "incomplete value")
		} else {
			// TODO: store the underlying error explicitly
			ret = wrapCallErr(call, &adt.Bottom{Err: errors.Promote(err, "")})
		}
	default:
		// Likely a string passed to panic.
		ret = wrapCallErr(call, &adt.Bottom{
			Err: errors.Newf(call.Pos(), "%s", err),
		})
	}
	return ret
}

func wrapCallErr(c *callCtxt, b *adt.Bottom) *adt.Bottom {
	pos := token.NoPos
	if c.src != nil {
		if src := c.src.Source(); src != nil {
			pos = src.Pos()
		}
	}
	const msg = "error in call to %s"
	return &adt.Bottom{
		Code: b.Code,
		Err:  errors.Wrapf(b.Err, pos, msg, c.builtin.name(c.ctx)),
	}
}

func (c *callCtxt) convertError(x interface{}, name string) *adt.Bottom {
	var err errors.Error
	switch v := x.(type) {
	case nil:
		return nil

	case *adt.Bottom:
		return v

	case *json.MarshalerError:
		err = errors.Promote(v, "marshal error")

	case errors.Error:
		err = v

	case error:
		if name != "" {
			err = errors.Newf(c.Pos(), "%s: %v", name, v)
		} else {
			err = errors.Newf(c.Pos(), "error in call to %s: %v", c.name(), v)
		}

	default:
		err = errors.Newf(token.NoPos, "%s", name)
	}
	if err != internal.ErrIncomplete {
		return &adt.Bottom{
			// Wrap to preserve position information.
			Err: errors.Wrapf(err, c.Pos(), "error in call to %s", c.name()),
		}
	}
	return &adt.Bottom{
		Code: adt.IncompleteError,
		Err:  errors.Newf(c.Pos(), "incomplete values in call to %s", c.name()),
	}
}

// callCtxt is passed to builtin implementations.
type callCtxt struct {
	idx     *index
	src     adt.Expr // *adt.CallExpr
	ctx     *context
	builtin *builtin
	err     interface{}
	ret     interface{}

	args []adt.Value
}

func (c *callCtxt) Pos() token.Pos {
	return c.ctx.opCtx.Pos()
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
			PkgName:    path.Base(k),
			root:       e,
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
	return p.root
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
		a := s.Lookup(ctx.Label(name, false))
		if a == nil {
			return v
		}

		return v.Unify(makeValue(v.idx, a))
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

func (c *callCtxt) errcf(src source, code adt.ErrorCode, format string, args ...interface{}) {
	a := make([]interface{}, 0, 2+len(args))
	a = append(a, code)
	a = append(a, format)
	a = append(a, args...)
	err := c.ctx.mkErr(src, a...)
	c.err = &callError{err}
}

func (c *callCtxt) value(i int) Value {
	v := newValueRoot(c.ctx, c.args[i])
	// TODO: remove default
	// v, _ = v.Default()
	if !v.IsConcrete() {
		c.errcf(c.src, adt.IncompleteError, "non-concrete argument %d", i)
	}
	return v
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
	if ve, ok := err.(*valueError); ok && ve.err.IsIncomplete() {
		c.err = ve
		return
	}
	v, ok := arg.(adt.Value)
	// TODO: make these permanent errors if the value did not originate from
	// a reference.
	if !ok {
		c.errf(c.src, nil,
			"cannot use incomplete value %s as %s in argument %d to %s: %v",
			c.ctx.str(arg), typ, i, c.name(), err)
	}
	if err != nil {
		c.errf(c.src, err,
			"cannot use %s (type %s) as %s in argument %d to %s: %v",
			c.ctx.str(arg), v.Kind(), typ, i, c.name(), err)
	} else {
		c.errf(c.src, err,
			"cannot use %s (type %s) as %s in argument %d to %s",
			c.ctx.str(arg), v.Kind(), typ, i, c.name())
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
	return &c.args[i].(*numLit).X
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

var ten = big.NewInt(10)

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
		a = append(a, &num.X)
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
			c.err = errors.Wrapf(err, c.Pos(),
				"element %d of list argument %d", j, i)
			break
		}
		a = append(a, str)
	}
	return a
}
