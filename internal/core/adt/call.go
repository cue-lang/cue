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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// A CallContext holds all relevant information for a function call to
// be executed.
type CallContext struct {
	ctx         *OpContext
	call        *CallExpr
	builtin     *Builtin
	args        []Value
	isValidator bool
}

func (c *CallContext) Pos() token.Pos {
	var src ast.Node
	switch {
	case c.call != nil:
		src = c.call.Source()
	case c.builtin != nil:
		src = c.builtin.Source()
	}
	if src != nil {
		return src.Pos()
	}
	return token.NoPos
}

func (c *CallContext) Value(i int) Value {
	return c.args[i]
}

// NumParams returns the total number of parameters to this function.
func (c *CallContext) NumParams() int {
	return len(c.args)
}

func (c *CallContext) AddPositions(err *ValueError) {
	for _, v := range c.args {
		err.AddPosition(v)
	}
}
