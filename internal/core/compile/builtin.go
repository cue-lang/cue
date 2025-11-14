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

package compile

import (
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/iterutil"
)

// This file contains predeclared builtins.

const supportedByLen = adt.StructKind | adt.BytesKind | adt.StringKind | adt.ListKind

var (
	stringParam = adt.Param{Value: &adt.BasicType{K: adt.StringKind}}
	structParam = adt.Param{Value: &adt.BasicType{K: adt.StructKind}}
	listParam   = adt.Param{Value: &adt.BasicType{K: adt.ListKind}}
	intParam    = adt.Param{Value: &adt.BasicType{K: adt.IntKind}}
	topParam    = adt.Param{Value: &adt.BasicType{K: adt.TopKind}}
)

// error is a special builtin that allows users to create a custom error
// message. If the argument is an interpolation, it will be evaluated and if it
// results in an error, the argument will be inserted as an expression.
var errorBuiltin = &adt.Builtin{
	Name:  "error",
	Added: "v0.14.0",

	Params: []adt.Param{stringParam},
	Result: adt.BottomKind,
	RawFunc: func(call *adt.CallContext) adt.Value {
		ctx := call.OpContext()
		arg := call.Expr(0)

		var b *adt.Bottom

		switch x := arg.(type) {
		case *adt.Interpolation:
			var args []any
			var w strings.Builder
			for i := 0; i < len(x.Parts); i++ {
				v := x.Parts[i]
				w.WriteString(v.(*adt.String).Str)
				if i++; i >= len(x.Parts) {
					break
				}
				w.WriteString("%v")
				y := call.OpContext().EvaluateKeepState(x.Parts[i])
				if err := ctx.Err(); err != nil {
					args = append(args, x.Parts[i])
				} else if y.Concreteness() == adt.Concrete &&
					y.Kind()&adt.NumberKind|adt.StringKind|adt.BytesKind|adt.BoolKind != 0 {
					args = append(args, ctx.ToString(y))
				} else {
					args = append(args, y)
				}
			}
			b = call.Errf(w.String(), args...)
		default:
			msg := ctx.ToString(call.Arg(0))
			b = call.Errf("%s", msg)
		}

		_ = arg
		b.Code = adt.UserError
		return b
	},
}

var lenBuiltin = &adt.Builtin{
	Name:   "len",
	Params: []adt.Param{{Value: &adt.BasicType{K: supportedByLen}}},
	Result: adt.IntKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		v := args[0]
		if x, ok := v.(*adt.Vertex); ok {
			switch x.BaseValue.(type) {
			case nil:
				// This should not happen, but be defensive.
				return c.NewErrf("unevaluated vertex")
			case *adt.ListMarker:
				return c.NewInt64(int64(iterutil.Count(x.Elems())), v)

			case *adt.StructMarker:
				n := 0
				v, _ := v.(*adt.Vertex)
				for _, a := range v.Arcs {
					if a.Label.IsRegular() && a.IsDefined(c) {
						n++
					}
				}
				return c.NewInt64(int64(n), v)

			default:
				v = x.Value()
			}
		}

		switch x := v.(type) {
		case *adt.Bytes:
			return c.NewInt64(int64(len(x.B)), v)
		case *adt.String:
			return c.NewInt64(int64(len(x.Str)), v)
		default:
			k := x.Kind()
			if k&supportedByLen == adt.BottomKind {
				return c.NewErrf("invalid argument type %v", k)
			}
			b := c.NewErrf("incomplete argument %s (type %v)", v, k)
			b.Code = adt.IncompleteError
			return b
		}
	},
}

var closeBuiltin = &adt.Builtin{
	Name:   "close",
	Params: []adt.Param{structParam},
	Result: adt.StructKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		s, ok := args[0].(*adt.Vertex)
		if !ok {
			return c.NewErrf("struct argument must be concrete")
		}
		// TODO(evalv3) this is a rather convoluted and inefficient way to
		// accomplish signaling vertex should be closed. In most cases, it
		// would suffice to set IsClosed in the CloseInfo. However, that
		// does not cover all code paths. Consider simplifying this.
		v := c.Wrap(s, c.CloseInfo())
		v.ClosedNonRecursive = true
		return v
	},
}

var closeAllBuiltin = &adt.Builtin{
	Name:   "__closeAll",
	Params: []adt.Param{topParam},
	Result: adt.TopKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()

		x := call.Expr(0)
		switch x.(type) {
		case *adt.StructLit, *adt.ListLit:
			if src := x.Source(); src == nil || !src.Pos().Experiment().ExplicitOpen {
				// Allow usage if explicit open is set
				return c.NewErrf("__closeAll may only be used when explicitopen is enabled")
			}
		default:
			return c.NewErrf("argument must be a struct or list literal")
		}

		// must be literal struct
		args := call.Args()

		s, ok := args[0].(*adt.Vertex)
		if !ok {
			return c.NewErrf("struct argument must be concrete")
		}

		s.ClosedRecursive = true

		return s
	},
}

var recloseBuiltin = &adt.Builtin{
	Name:   "__reclose",
	Params: []adt.Param{topParam},
	Result: adt.TopKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()

		x := call.Expr(0)
		switch x.(type) {
		case *adt.StructLit, *adt.ListLit:
			if src := x.Source(); src == nil || !src.Pos().Experiment().ExplicitOpen {
				// Allow usage if explicit open is set
				return c.NewErrf("__reclose may only be used when explicitopen is enabled")
			}
		default:
			return c.NewErrf("argument must be a struct or list literal")
		}

		// must be literal struct
		args := call.Args()

		// Note that we could have an embedded scalar here, so having a struct
		// or list does not guarantee that the result is that as well.
		//
		//	#Def: 1
		//	a: __reclose({ #Def })
		//
		if s, ok := args[0].(*adt.Vertex); ok && s.ShouldRecursivelyClose() {
			s.ClosedRecursive = true
		}

		return args[0]
	},
}

var andBuiltin = &adt.Builtin{
	Name:   "and",
	Params: []adt.Param{listParam},
	Result: adt.IntKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		seq := c.RawElems(args[0])
		a := []adt.Value{}
		for c := range seq {
			a = append(a, c)
		}
		if len(a) == 0 {
			return &adt.Top{}
		}
		return &adt.Conjunction{Values: a}
	},
}

var orBuiltin = &adt.Builtin{
	Name:        "or",
	Params:      []adt.Param{listParam},
	Result:      adt.IntKind,
	NonConcrete: true,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		d := []adt.Disjunct{}
		for c := range c.RawElems(args[0]) {
			d = append(d, adt.Disjunct{Val: c, Default: false})
		}
		if len(d) == 0 {
			// TODO(manifest): This should not be unconditionally incomplete,
			// but it requires results from comprehensions and all to have
			// some special status. Maybe this can be solved by having results
			// of list comprehensions be open if they result from iterating over
			// an open list or struct. This would actually be exactly what
			// that means. The error here could then only add an incomplete
			// status if the source is open.
			return &adt.Bottom{
				Code: adt.IncompleteError,
				// TODO: get and set Vertex
				Err: errors.Newf(c.Pos(), "empty list in call to or"),
			}
		}
		v := &adt.Vertex{}
		// TODO: make a Disjunction.
		closeInfo := c.CloseInfo()
		v.AddConjunct(adt.MakeConjunct(nil,
			&adt.DisjunctionExpr{Values: d, HasDefaults: false},
			closeInfo,
		))
		v.CompleteArcs(c)
		return v
	},
}

var divBuiltin = &adt.Builtin{
	Name:   "div",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		const name = "argument to div builtin"

		return intDivOp(c, (*adt.OpContext).IntDiv, name, args)
	},
}

var modBuiltin = &adt.Builtin{
	Name:   "mod",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		const name = "argument to mod builtin"

		return intDivOp(c, (*adt.OpContext).IntMod, name, args)
	},
}

var quoBuiltin = &adt.Builtin{
	Name:   "quo",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		const name = "argument to quo builtin"

		return intDivOp(c, (*adt.OpContext).IntQuo, name, args)
	},
}

var remBuiltin = &adt.Builtin{
	Name:   "rem",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call *adt.CallContext) adt.Expr {
		c := call.OpContext()
		args := call.Args()

		const name = "argument to rem builtin"

		return intDivOp(c, (*adt.OpContext).IntRem, name, args)
	},
}

type intFunc func(c *adt.OpContext, x, y *adt.Num) adt.Value

func intDivOp(c *adt.OpContext, fn intFunc, name string, args []adt.Value) adt.Value {
	a := c.Num(args[0], name)
	b := c.Num(args[1], name)

	if c.HasErr() {
		return nil
	}

	return fn(c, a, b)
}

var testExperiment = &adt.Builtin{
	Name:   "testExperiment",
	Params: []adt.Param{topParam},
	Result: adt.TopKind,
	Func: func(call *adt.CallContext) adt.Expr {
		args := call.Args()

		if call.Pos().Experiment().Testing {
			return args[0]
		} else {
			return call.OpContext().NewErrf("testing experiment disabled")
		}
	},
}
