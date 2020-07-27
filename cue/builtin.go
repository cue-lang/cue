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

// A Builtin is a builtin function or constant.
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
type Builtin struct {
	Name   string
	Pkg    adt.Feature
	Params []adt.Kind
	Result adt.Kind
	Func   func(c *CallCtxt)
	Const  string
}

type Package struct {
	Native []*Builtin
	CUE    string
}

func (p *Package) MustCompile(ctx *adt.OpContext, pkgName string) *adt.Vertex {
	obj := &adt.Vertex{}
	pkgLabel := ctx.StringLabel(pkgName)
	st := &adt.StructLit{}
	if len(p.Native) > 0 {
		obj.AddConjunct(adt.MakeConjunct(nil, st))
	}
	for _, b := range p.Native {
		b.Pkg = pkgLabel

		f := ctx.StringLabel(b.Name) // never starts with _
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
	if p.CUE != "" {
		expr, err := parser.ParseExpr(pkgName, p.CUE)
		if err != nil {
			panic(fmt.Errorf("could not parse %v: %v", p.CUE, err))
		}
		c, err := compile.Expr(nil, ctx.Runtime, expr)
		if err != nil {
			panic(fmt.Errorf("could compile parse %v: %v", p.CUE, err))
		}
		obj.AddConjunct(c)
	}

	// We could compile lazily, but this is easier for debugging.
	obj.Finalize(ctx)
	if err := obj.Err(ctx, adt.Finalized); err != nil {
		panic(err.Err)
	}

	return obj
}

func toBuiltin(ctx *adt.OpContext, b *Builtin) *adt.Builtin {
	x := &adt.Builtin{
		Params:  b.Params,
		Result:  b.Result,
		Package: b.Pkg,
		Name:    b.Name,
	}
	x.Func = func(ctx *adt.OpContext, args []adt.Value) (ret adt.Expr) {
		runtime := ctx.Impl().(*runtime.Runtime)
		index := runtime.Data.(*index)

		// call, _ := ctx.Source().(*ast.CallExpr)
		c := &CallCtxt{
			// src:  call,
			ctx:     index.newContext(),
			args:    args,
			builtin: b,
		}
		defer func() {
			var errVal interface{} = c.Err
			if err := recover(); err != nil {
				errVal = err
			}
			ret = processErr(c, errVal, ret)
		}()
		b.Func(c)
		switch v := c.Ret.(type) {
		case adt.Value:
			return v
		case *valueError:
			return v.err
		}
		if c.Err != nil {
			return nil
		}
		return convert.GoValueToValue(ctx, c.Ret, true)
	}
	return x
}

// newConstBuiltin parses and creates any CUE expression that does not have
// fields.
func mustParseConstBuiltin(ctx *adt.OpContext, name, val string) adt.Expr {
	expr, err := parser.ParseExpr("<builtin:"+name+">", val)
	if err != nil {
		panic(err)
	}
	c, err := compile.Expr(nil, ctx, expr)
	if err != nil {
		panic(err)
	}
	return c.Expr()

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

func (x *Builtin) name(ctx *context) string {
	if x.Pkg == 0 {
		return x.Name
	}
	return fmt.Sprintf("%s.%s", ctx.LabelStr(x.Pkg), x.Name)
}

func (x *Builtin) isValidator() bool {
	return len(x.Params) == 1 && x.Result == adt.BoolKind
}

func processErr(call *CallCtxt, errVal interface{}, ret adt.Expr) adt.Expr {
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
		if call.Err == internal.ErrIncomplete {
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

func wrapCallErr(c *CallCtxt, b *adt.Bottom) *adt.Bottom {
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

func (c *CallCtxt) convertError(x interface{}, name string) *adt.Bottom {
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
			err = errors.Newf(c.Pos(), "error in call to %s: %v", c.Name(), v)
		}

	default:
		err = errors.Newf(token.NoPos, "%s", name)
	}
	if err != internal.ErrIncomplete {
		return &adt.Bottom{
			// Wrap to preserve position information.
			Err: errors.Wrapf(err, c.Pos(), "error in call to %s", c.Name()),
		}
	}
	return &adt.Bottom{
		Code: adt.IncompleteError,
		Err:  errors.Newf(c.Pos(), "incomplete values in call to %s", c.Name()),
	}
}

// CallCtxt is passed to builtin implementations that need to use a cue.Value. This is an internal type. It's interface may change.
type CallCtxt struct {
	src     adt.Expr // *adt.CallExpr
	ctx     *context
	builtin *Builtin
	Err     interface{}
	Ret     interface{}

	args []adt.Value
}

func (c *CallCtxt) Pos() token.Pos {
	return c.ctx.opCtx.Pos()
}

func (c *CallCtxt) Name() string {
	return c.builtin.name(c.ctx)
}

var builtins = map[string]*Instance{}

func initBuiltins(pkgs map[string]*Package) {
	ctx := sharedIndex.newContext().opCtx
	keys := []string{}
	for k := range pkgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := pkgs[k]
		e := b.MustCompile(ctx, k)

		i := sharedIndex.addInst(&Instance{
			ImportPath: k,
			PkgName:    path.Base(k),
			root:       e,
		})

		builtins[k] = i
		builtins["-/"+path.Base(k)] = i
	}
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

// Do returns whether the call should be done.
func (c *CallCtxt) Do() bool {
	return c.Err == nil
}

type callError struct {
	b *adt.Bottom
}

func (e *callError) Error() string {
	return fmt.Sprint(e.b)
}

func (c *CallCtxt) errf(src adt.Node, underlying error, format string, args ...interface{}) {
	a := make([]interface{}, 0, 2+len(args))
	if err, ok := underlying.(*valueError); ok {
		a = append(a, err.err)
	}
	a = append(a, format)
	a = append(a, args...)
	err := c.ctx.mkErr(src, a...)
	c.Err = &callError{err}
}

func (c *CallCtxt) errcf(src adt.Node, code adt.ErrorCode, format string, args ...interface{}) {
	a := make([]interface{}, 0, 2+len(args))
	a = append(a, code)
	a = append(a, format)
	a = append(a, args...)
	err := c.ctx.mkErr(src, a...)
	c.Err = &callError{err}
}

func (c *CallCtxt) Value(i int) Value {
	v := newValueRoot(c.ctx, c.args[i])
	// TODO: remove default
	// v, _ = v.Default()
	if !v.IsConcrete() {
		c.errcf(c.src, adt.IncompleteError, "non-concrete argument %d", i)
	}
	return v
}

func (c *CallCtxt) Struct(i int) *Struct {
	v := newValueRoot(c.ctx, c.args[i])
	s, err := v.Struct()
	if err != nil {
		c.invalidArgType(c.args[i], i, "struct", err)
		return nil
	}
	return s
}

func (c *CallCtxt) invalidArgType(arg adt.Expr, i int, typ string, err error) {
	if ve, ok := err.(*valueError); ok && ve.err.IsIncomplete() {
		c.Err = ve
		return
	}
	v, ok := arg.(adt.Value)
	// TODO: make these permanent errors if the value did not originate from
	// a reference.
	if !ok {
		c.errf(c.src, nil,
			"cannot use incomplete value %s as %s in argument %d to %s: %v",
			c.ctx.str(arg), typ, i, c.Name(), err)
	}
	if err != nil {
		c.errf(c.src, err,
			"cannot use %s (type %s) as %s in argument %d to %s: %v",
			c.ctx.str(arg), v.Kind(), typ, i, c.Name(), err)
	} else {
		c.errf(c.src, err,
			"cannot use %s (type %s) as %s in argument %d to %s",
			c.ctx.str(arg), v.Kind(), typ, i, c.Name())
	}
}

func (c *CallCtxt) Int(i int) int     { return int(c.intValue(i, 64, "int64")) }
func (c *CallCtxt) Int8(i int) int8   { return int8(c.intValue(i, 8, "int8")) }
func (c *CallCtxt) Int16(i int) int16 { return int16(c.intValue(i, 16, "int16")) }
func (c *CallCtxt) Int32(i int) int32 { return int32(c.intValue(i, 32, "int32")) }
func (c *CallCtxt) Rune(i int) rune   { return rune(c.intValue(i, 32, "rune")) }
func (c *CallCtxt) Int64(i int) int64 { return int64(c.intValue(i, 64, "int64")) }

func (c *CallCtxt) intValue(i, bits int, typ string) int64 {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	n, err := x.Int(nil)
	if err != nil {
		c.invalidArgType(arg, i, typ, err)
		return 0
	}
	if n.BitLen() > bits {
		c.errf(c.src, err, "int %s overflows %s in argument %d in call to %s",
			n, typ, i, c.Name())
	}
	res, _ := x.Int64()
	return res
}

func (c *CallCtxt) Uint(i int) uint     { return uint(c.uintValue(i, 64, "uint64")) }
func (c *CallCtxt) Uint8(i int) uint8   { return uint8(c.uintValue(i, 8, "uint8")) }
func (c *CallCtxt) Byte(i int) uint8    { return byte(c.uintValue(i, 8, "byte")) }
func (c *CallCtxt) Uint16(i int) uint16 { return uint16(c.uintValue(i, 16, "uint16")) }
func (c *CallCtxt) Uint32(i int) uint32 { return uint32(c.uintValue(i, 32, "uint32")) }
func (c *CallCtxt) Uint64(i int) uint64 { return uint64(c.uintValue(i, 64, "uint64")) }

func (c *CallCtxt) uintValue(i, bits int, typ string) uint64 {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil || n.Sign() < 0 {
		c.invalidArgType(c.args[i], i, typ, err)
		return 0
	}
	if n.BitLen() > bits {
		c.errf(c.src, err, "int %s overflows %s in argument %d in call to %s",
			n, typ, i, c.Name())
	}
	res, _ := x.Uint64()
	return res
}

func (c *CallCtxt) Decimal(i int) *apd.Decimal {
	x := newValueRoot(c.ctx, c.args[i])
	if _, err := x.MantExp(nil); err != nil {
		c.invalidArgType(c.args[i], i, "Decimal", err)
		return nil
	}
	return &c.args[i].(*numLit).X
}

func (c *CallCtxt) Float64(i int) float64 {
	x := newValueRoot(c.ctx, c.args[i])
	res, err := x.Float64()
	if err != nil {
		c.invalidArgType(c.args[i], i, "float64", err)
		return 0
	}
	return res
}

func (c *CallCtxt) BigInt(i int) *big.Int {
	x := newValueRoot(c.ctx, c.args[i])
	n, err := x.Int(nil)
	if err != nil {
		c.invalidArgType(c.args[i], i, "int", err)
		return nil
	}
	return n
}

var ten = big.NewInt(10)

func (c *CallCtxt) BigFloat(i int) *big.Float {
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

func (c *CallCtxt) String(i int) string {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.String()
	if err != nil {
		c.invalidArgType(c.args[i], i, "string", err)
		return ""
	}
	return v
}

func (c *CallCtxt) Bytes(i int) []byte {
	x := newValueRoot(c.ctx, c.args[i])
	v, err := x.Bytes()
	if err != nil {
		c.invalidArgType(c.args[i], i, "bytes", err)
		return nil
	}
	return v
}

func (c *CallCtxt) Reader(i int) io.Reader {
	x := newValueRoot(c.ctx, c.args[i])
	// TODO: optimize for string and bytes cases
	r, err := x.Reader()
	if err != nil {
		c.invalidArgType(c.args[i], i, "bytes|string", err)
		return nil
	}
	return r
}

func (c *CallCtxt) Bool(i int) bool {
	x := newValueRoot(c.ctx, c.args[i])
	b, err := x.Bool()
	if err != nil {
		c.invalidArgType(c.args[i], i, "bool", err)
		return false
	}
	return b
}

func (c *CallCtxt) List(i int) (a []Value) {
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

func (c *CallCtxt) Iter(i int) (a Iterator) {
	arg := c.args[i]
	x := newValueRoot(c.ctx, arg)
	v, err := x.List()
	if err != nil {
		c.invalidArgType(c.args[i], i, "list", err)
		return Iterator{ctx: c.ctx}
	}
	return v
}

func (c *CallCtxt) DecimalList(i int) (a []*apd.Decimal) {
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
				j, i, c.Name(), err)
			break
		}
		a = append(a, &num.X)
	}
	return a
}

func (c *CallCtxt) StringList(i int) (a []string) {
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
			c.Err = errors.Wrapf(err, c.Pos(),
				"element %d of list argument %d", j, i)
			break
		}
		a = append(a, str)
	}
	return a
}
