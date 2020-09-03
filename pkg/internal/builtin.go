// Copyright 2020 CUE Authors
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

package internal

import (
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/convert"
)

// A Builtin is a Builtin function or constant.
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
		obj.AddConjunct(adt.MakeRootConjunct(nil, st))
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
		// call, _ := ctx.Source().(*ast.CallExpr)
		c := &CallCtxt{
			// src:  call,
			ctx:     ctx,
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
		case bottomer:
			return v.Bottom()
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
func mustParseConstBuiltin(ctx adt.Runtime, name, val string) adt.Expr {
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

func (x *Builtin) name(ctx *adt.OpContext) string {
	if x.Pkg == 0 {
		return x.Name
	}
	return fmt.Sprintf("%s.%s", x.Pkg.StringValue(ctx), x.Name)
}

func (x *Builtin) isValidator() bool {
	return len(x.Params) == 1 && x.Result == adt.BoolKind
}

func processErr(call *CallCtxt, errVal interface{}, ret adt.Expr) adt.Expr {
	ctx := call.ctx
	switch err := errVal.(type) {
	case nil:
	case *callError:
		ret = err.b
	case *json.MarshalerError:
		if err, ok := err.Err.(bottomer); ok {
			if b := err.Bottom(); b != nil {
				ret = b
			}
		}
	case bottomer:
		ret = wrapCallErr(call, err.Bottom())
	case errors.Error:
		ret = wrapCallErr(call, &adt.Bottom{Err: err})
	case error:
		if call.Err == internal.ErrIncomplete {
			err := ctx.NewErrf("incomplete value")
			err.Code = adt.IncompleteError
			ret = err
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
