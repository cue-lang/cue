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
	"fmt"
	"log"
	"strings"

	"cuelang.org/go/cue/token"
)

// Assert panics if the condition is false. Assert can be used to check for
// conditions that are considers to break an internal variant or unexpected
// condition, but that nonetheless probably will be handled correctly down the
// line. For instance, a faulty condition could lead to error being caught
// down the road, but resulting in an inaccurate error message. In production
// code it is better to deal with the bad error message than to panic.
//
// It is advisable for each use of Assert to document how the error is expected
// to be handled down the line.
func Assertf(c *OpContext, b bool, format string, args ...interface{}) {
	if c.Strict && !b {
		panic(fmt.Sprintf("assertion failed: "+format, args...))
	}
}

// Assertf either panics or reports an error to c if the condition is not met.
func (c *OpContext) Assertf(pos token.Pos, b bool, format string, args ...interface{}) {
	if !b {
		if c.Strict {
			panic(fmt.Sprintf("assertion failed: "+format, args...))
		}
		c.addErrf(0, pos, format, args...)
	}
}

func init() {
	log.SetFlags(log.Lshortfile)
}

var pMap = map[*Vertex]int{}

func (c *OpContext) Logf(v *Vertex, format string, args ...interface{}) {
	if c.LogEval == 0 {
		return
	}
	var prefix string
	if c.nest > 0 {
		prefix = strings.Repeat("... ", c.nest)
		prefix = prefix[:len(prefix)-1]
	}

	if v == nil {
		s := fmt.Sprintf(prefix+format, args...)
		_ = log.Output(2, s)
		return
	}
	p := pMap[v]
	if p == 0 {
		p = len(pMap) + 1
		pMap[v] = p
	}
	disjunctInfo := c.disjunctInfo()

	a := append([]interface{}{
		prefix,
		p,
		v.Path(),
		c.PathToString(v.Path()),
		disjunctInfo,
	}, args...)
	for i := 2; i < len(a); i++ {
		switch x := a[i].(type) {
		case Node:
			a[i] = c.Str(x)
		case Feature:
			a[i] = x.SelectorString(c)
		}
	}
	s := fmt.Sprintf("%s [n:%d %v%s%s] "+format, a...)
	_ = log.Output(2, s)
}

// PathToString creates a pretty-printed path of the given list of features.
func (c *OpContext) PathToString(path []Feature) string {
	var b strings.Builder
	for i, f := range path {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(f.SelectorString(c))
	}
	return b.String()
}

type disjunctInfo struct {
	node            *nodeContext
	disjunctionID   int  // unique ID for sequence
	disjunctionSeq  int  // index into n.disjunctions
	numDisjunctions int  // number of disjunctions
	crossProductSeq int  // index into n.disjuncts (previous results)
	numPrevious     int  // index into n.disjuncts (previous results)
	numDisjuncts    int  // index into n.disjuncts (previous results)
	disjunctID      int  // unique ID for disjunct
	disjunctSeq     int  // index into n.disjunctions[disjunctionSeq].disjuncts
	holeID          int  // unique ID for hole
	lhs             Node // current LHS expression
	rhs             Node // current RHS expression
}

func (c *OpContext) currentDisjunct() *disjunctInfo {
	if len(c.disjunctStack) == 0 {
		panic("no disjunct")
	}
	return &c.disjunctStack[len(c.disjunctStack)-1]
}

func (n *nodeContext) logDisjunctionTask() *disjunctInfo {
	c := n.ctx
	c.currentDisjunctionID++
	id := disjunctInfo{
		node:          n,
		disjunctionID: c.currentDisjunctionID,
	}
	c.disjunctStack = append(c.disjunctStack, id)

	n.Logf("========= DISJUNCTION %d =========", c.currentDisjunctionID)
	c.nest += 1

	return c.currentDisjunct()
}

func (n *nodeContext) nextDisjunction(index, num, hole int) {
	d := n.ctx.currentDisjunct()

	d.disjunctionSeq = index + 1
	d.numDisjunctions = num
	d.holeID = hole
}

func (n *nodeContext) nextCrossProduct(index, num int, v *nodeContext) *disjunctInfo {
	d := n.ctx.currentDisjunct()

	d.crossProductSeq = index + 1
	d.numPrevious = num
	d.lhs = v.node.Value()

	return d
}

func (n *nodeContext) nextDisjunct(index, num int, expr Node) {
	d := n.ctx.currentDisjunct()

	d.disjunctSeq = index + 1
	d.numDisjuncts = num
	d.rhs = expr
}

func (n *nodeContext) logDoDisjunct() *disjunctInfo {
	c := n.ctx
	c.stats.Disjuncts++

	d := c.currentDisjunct()

	d.disjunctID = int(c.stats.Disjuncts)

	n.Logf("====== Do DISJUNCT %v & %v ======", d.lhs, d.rhs)

	return d
}

func (d disjunctInfo) pop() {
	c := d.node.ctx
	c.nest -= 1
	c.disjunctStack = c.disjunctStack[:len(c.disjunctStack)-1]
}

// disjunctInfo prints a header for log to indicate the current disjunct.
func (c *OpContext) disjunctInfo() string {
	if len(c.disjunctStack) == 0 {
		return ""
	}
	var b strings.Builder
	for i, d := range c.disjunctStack {
		if i != len(c.disjunctStack)-1 && d.disjunctID == 0 {
			continue
		}
		if i != 0 {
			b.WriteString(" =>")
		}
		// which disjunct
		fmt.Fprintf(&b, " D%d:H%d:%d/%d",
			d.disjunctionID, d.holeID, d.disjunctionSeq, d.numDisjunctions)
		if d.crossProductSeq != 0 {
			fmt.Fprintf(&b, " P%d/%d", d.crossProductSeq, d.numPrevious)
		}
		if d.disjunctID != 0 {
			fmt.Fprintf(&b, " d%d:%d/%d",
				d.disjunctID, d.disjunctSeq, d.numDisjuncts,
			)
		}
	}
	return b.String()
}
