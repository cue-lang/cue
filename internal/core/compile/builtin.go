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
	RawFunc: func(call adt.BuiltinCallContext) adt.Value {
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
					y.Kind()&(adt.NumberKind|adt.StringKind|adt.BytesKind|adt.BoolKind) != 0 {
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
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()

		v := call.Value(0)
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
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()
		s, ok := call.Value(0).(*adt.Vertex)
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
	Func: func(call adt.BuiltinCallContext) adt.Expr {
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

		// argument must be literal struct
		s, ok := call.Value(0).(*adt.Vertex)
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
	Func: func(call adt.BuiltinCallContext) adt.Expr {
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

		// Note that we could have an embedded scalar here, so having a struct
		// or list does not guarantee that the result is that as well.
		//
		//	#Def: 1
		//	a: __reclose({ #Def })
		//
		arg := call.Value(0)
		if s, ok := arg.(*adt.Vertex); ok && s.ShouldRecursivelyClose() {
			s.ClosedRecursive = true
		}

		return arg
	},
}

var andBuiltin = &adt.Builtin{
	Name:   "and",
	Params: []adt.Param{listParam},
	Result: adt.IntKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()
		seq := c.RawElems(call.Value(0))
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
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()

		var values []adt.Value
		for v := range c.RawElems(call.Value(0)) {
			values = append(values, v)
		}
		if len(values) == 0 {
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
		if len(values) == 1 {
			return values[0]
		}
		return &adt.Disjunction{Values: values}
	},
}

// boolListOp is the shared implementation for all() and some(). It evaluates
// every element strictly: any error or incomplete value causes an error even
// if the boolean result is already determined. identity=true means all (AND),
// identity=false means some (OR). Requires @experiment(shortcircuit).
func boolListOp(call adt.BuiltinCallContext, identity bool, opName string) adt.Expr {
	if !call.Pos().Experiment().ShortCircuit {
		return call.OpContext().NewErrf("%s requires @experiment(shortcircuit)", opName)
	}
	c := call.OpContext()
	result := identity
	var firstErr *adt.Bottom
	for elem := range c.Elems(call.Value(0)) {
		val := elem.Value()
		switch v := val.(type) {
		case *adt.Bool:
			if identity {
				result = result && v.B
			} else {
				result = result || v.B
			}
		case *adt.Bottom:
			if firstErr == nil {
				firstErr = v
			}
		default:
			if elem.Kind()&adt.BoolKind == 0 {
				return c.NewErrf("non-bool value in call to %s", opName)
			}
			if firstErr == nil {
				b := c.NewErrf("incomplete bool in call to %s", opName)
				b.Code = adt.IncompleteError
				firstErr = b
			}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return c.NewBool(result)
}

// allBuiltin returns true if every element of the list is true (identity for
// empty list), false if any element is false. Any error or incomplete element
// causes an error. Requires @experiment(shortcircuit).
var allBuiltin = &adt.Builtin{
	Name:   "all",
	Added:  "v0.17.0",
	Params: []adt.Param{listParam},
	Result: adt.BoolKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		return boolListOp(call, true, "all")
	},
}

// someBuiltin returns true if any element of the list is true, false if all
// elements are false (identity for empty list). Any error or incomplete element
// causes an error. Requires @experiment(shortcircuit).
var someBuiltin = &adt.Builtin{
	Name:   "some",
	Added:  "v0.17.0",
	Params: []adt.Param{listParam},
	Result: adt.BoolKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		return boolListOp(call, false, "some")
	},
}

var divBuiltin = &adt.Builtin{
	Name:   "div",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()
		const name = "argument to div builtin"
		return intDivOp(c, (*adt.OpContext).IntDiv, name, call.Value(0), call.Value(1))
	},
}

var modBuiltin = &adt.Builtin{
	Name:   "mod",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()

		const name = "argument to mod builtin"

		return intDivOp(c, (*adt.OpContext).IntMod, name, call.Value(0), call.Value(1))
	},
}

var quoBuiltin = &adt.Builtin{
	Name:   "quo",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()
		const name = "argument to quo builtin"
		return intDivOp(c, (*adt.OpContext).IntQuo, name, call.Value(0), call.Value(1))
	},
}

var remBuiltin = &adt.Builtin{
	Name:   "rem",
	Params: []adt.Param{intParam, intParam},
	Result: adt.IntKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		c := call.OpContext()
		const name = "argument to rem builtin"
		return intDivOp(c, (*adt.OpContext).IntRem, name, call.Value(0), call.Value(1))
	},
}

type intFunc func(c *adt.OpContext, x, y *adt.Num) adt.Value

func intDivOp(c *adt.OpContext, fn intFunc, name string, av, bv adt.Value) adt.Value {
	a := c.Num(av, name)
	b := c.Num(bv, name)
	if c.HasErr() {
		return nil
	}
	return fn(c, a, b)
}

var testExperiment = &adt.Builtin{
	Name:   "testExperiment",
	Params: []adt.Param{topParam},
	Result: adt.TopKind,
	Func: func(call adt.BuiltinCallContext) adt.Expr {
		if call.Pos().Experiment().Testing {
			return call.Value(0)
		} else {
			return call.OpContext().NewErrf("testing experiment disabled")
		}
	},
}
