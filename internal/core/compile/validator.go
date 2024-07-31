// Copyright 2024 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package compile

// This file contains validator and other non-monotonic builtins.

import (
	"cuelang.org/go/internal/core/adt"
)

// matchN is a validator that checks that the number of schemas in the given
// list that unify with "self" matches the number passed as the first argument
// of the validator. Note that this number may itself be a number constraint
// and does not have to be a concrete number.
var matchNBuiltin = &adt.Builtin{
	Name:   "matchN",
	Params: []adt.Param{topParam, intParam, listParam}, // varargs
	Result: adt.BoolKind,
	Func: func(c *adt.OpContext, args []adt.Value) adt.Expr {
		if !c.IsValidator {
			return c.NewErrf("mustN is a validator and should not be used as a function")
		}

		self := finalizeSelf(c, args[0])
		if err := bottom(c, self); err != nil {
			return &adt.Bool{B: false}
		}

		constraints := c.Elems(args[2])

		var count int64
		for _, check := range constraints {
			v := unifyValidator(c, self, check)
			if err := bottom(c, v); err == nil {
				// TODO: is it always true that the lack of an error signifies
				// success?
				count++
			}
		}

		bound := args[1]
		// TODO: consider a mode to require "all" to pass, for instance by
		// supporting the value null or "all".

		b := checkNum(c, bound, count)
		if b != nil {
			return b
		}
		return &adt.Bool{B: true}
	},
}

// finalizeSelf ensures a value is fully evaluated and then strips it of any
// of its validators or default values.
func finalizeSelf(c *adt.OpContext, self adt.Value) adt.Value {
	if x, ok := self.(*adt.Vertex); ok {
		self = x.ToDataAll(c)
	}
	return self
}

func unifyValidator(c *adt.OpContext, self, check adt.Value) *adt.Vertex {
	v := &adt.Vertex{}
	closeInfo := c.CloseInfo()
	v.AddConjunct(adt.MakeConjunct(nil, self, closeInfo))
	v.AddConjunct(adt.MakeConjunct(nil, check, closeInfo))
	v.Finalize(c)
	return v
}

func checkNum(ctx *adt.OpContext, bound adt.Expr, count int64) *adt.Bottom {
	n := adt.Vertex{}
	n.AddConjunct(ctx.MakeConjunct(bound))
	n.AddConjunct(ctx.MakeConjunct(ctx.NewInt64(count)))
	n.Finalize(ctx)

	b, _ := n.BaseValue.(*adt.Bottom)
	if b != nil {
		return ctx.NewErrf("%d matched, expected %v", count, bound)
	}
	return nil
}

func bottom(c *adt.OpContext, v adt.Value) *adt.Bottom {
	switch x := v.(type) {
	case *adt.Vertex:
		return x.Err(c)
	case *adt.Bottom:
		return x
	}
	return nil
}
