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

package cue

import (
	"github.com/cockroachdb/apd/v2"
)

// context manages evaluation state.
type context struct {
	*apd.Context

	*index

	forwardMap []scope // pairs
	oldSize    []int

	// constraints are to be evaluated at the end values to be evaluated later.
	constraints []*binaryExpr
	evalStack   []bottom

	inDefinition int
	inSum        int
	cycleErr     bool

	noManifest bool

	// for debug strings
	nodeRefs map[scope]string

	// tracing
	trace bool
	level int

	// TODO: replace with proper structural cycle detection/ occurs check.
	// See Issue #29.
	maxDepth int
}

func (c *context) incEvalDepth() {
	if len(c.evalStack) > 0 {
		c.evalStack[len(c.evalStack)-1].exprDepth++
	}
}

func (c *context) decEvalDepth() {
	if len(c.evalStack) > 0 {
		c.evalStack[len(c.evalStack)-1].exprDepth--
	}
}

var baseContext apd.Context

func init() {
	baseContext = apd.BaseContext
	baseContext.Precision = 24
}

// newContext returns a new evaluation context.
func (idx *index) newContext() *context {
	c := &context{
		Context: &baseContext,
		index:   idx,
	}
	return c
}

// delayConstraint schedules constraint to be evaluated and returns ret. If
// delaying constraints is currently not allowed, it returns an error instead.
func (c *context) delayConstraint(ret evaluated, constraint *binaryExpr) evaluated {
	c.cycleErr = true
	c.constraints = append(c.constraints, constraint)
	return ret
}

func (c *context) processDelayedConstraints() evaluated {
	cons := c.constraints
	c.constraints = c.constraints[:0]
	for _, dc := range cons {
		v := binOp(c, dc, dc.op, dc.left.evalPartial(c), dc.right.evalPartial(c))
		if isBottom(v) {
			return v
		}
	}
	return nil
}

func (c *context) deref(f scope) scope {
outer:
	for {
		for i := 0; i < len(c.forwardMap); i += 2 {
			if c.forwardMap[i] == f {
				f = c.forwardMap[i+1]
				continue outer
			}
		}
		return f
	}
}

func (c *context) pushForwards(pairs ...scope) *context {
	c.oldSize = append(c.oldSize, len(c.forwardMap))
	c.forwardMap = append(c.forwardMap, pairs...)
	return c
}

func (c *context) popForwards() {
	last := len(c.oldSize) - 1
	c.forwardMap = c.forwardMap[:c.oldSize[last]]
	c.oldSize = c.oldSize[:last]
}
