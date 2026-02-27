// Copyright 2025 CUE Authors
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

package adt

import (
	"cuelang.org/go/cue/token"
)

// A BuiltinCallContext holds all relevant information for a function call to
// be executed.
type BuiltinCallContext struct {
	ctx         *OpContext
	call        *CallExpr
	builtin     *Builtin
	args        []Value
	isValidator bool
}

func (c BuiltinCallContext) OpContext() *OpContext {
	return c.ctx
}

func (c BuiltinCallContext) Pos() token.Pos {
	if c.call != nil {
		return Pos(c.call)
	}
	return Pos(c.builtin)
}

func (c BuiltinCallContext) Value(i int) Value {
	return c.args[i]
}

// NumParams returns the total number of parameters to this function.
func (c BuiltinCallContext) NumParams() int {
	return len(c.args)
}

func (c BuiltinCallContext) AddPositions(err *ValueError) {
	for _, v := range c.args {
		err.AddPosition(v)
	}
}

// Arg returns the nth argument expression. The value is evaluated and any
// cycle information is accumulated in the context. This allows cycles in
// arguments to be detected.
//
// This method of getting an argument should be used when the argument is used
// as a schema and may contain cycles.
func (c BuiltinCallContext) Arg(i int) Value {
	// If the call context represents a validator call, the argument will be
	// offset by 1.
	if c.isValidator {
		if i == 0 {
			c.Errf("Expr may not be called for 0th argument of validator")
			return nil
		}
		i--
	}
	x := c.call.Args[i]

	// Evaluated while keeping any cycle information in the context.
	return c.ctx.EvaluateKeepState(x)
}

// Expr returns the nth argument expression without evaluating it.
func (c BuiltinCallContext) Expr(i int) Expr {
	// If the call context represents a validator call, the argument will be
	// offset by 1.
	if c.isValidator {
		if i == 0 {
			c.Errf("Expr may not be called for 0th argument of validator")
			return nil
		}
		i--
	}
	x := c.call.Args[i]

	return x
}

func (c BuiltinCallContext) Errf(format string, args ...interface{}) *Bottom {
	return c.ctx.NewErrf(format, args...)
}
