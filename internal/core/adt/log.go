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
	"io"
	"log"
	"path/filepath"
	"runtime"
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
	log.SetFlags(0)
}

var pMap = map[*Vertex]int{}

type nestString string

func (c *OpContext) Un(s nestString, id int) {
	c.nest--
	if id != c.logID {
		c.Logf(nil, "END %s", string(s))
	}
}

// Indentf logs a function call and increases the nesting level.
// The first argument must be the function name.
func (c *OpContext) Indentf(v *Vertex, format string, args ...any) (s nestString, id int) {
	if c.LogEval == 0 {
		// The Go compiler as of 1.24 is not very clever with no-op function calls;
		// any arguments passed to ...args above escape to the heap and allocate.
		panic("avoid calling OpContext.Indentf when logging is disabled to prevent overhead")
	}
	name := strings.Split(format, "(")[0]
	if name == "" {
		name, _ = getCallerFunctionName(1)
		format = name + format
	}

	caller, line := getCallerFunctionName(2)
	args = append(args, caller, line)

	format += " %s:%d"

	c.Logf(v, format, args...)
	c.nest++

	return nestString(name), c.logID
}

func (c *OpContext) RewriteArgs(args ...interface{}) {
	for i, a := range args {
		switch x := a.(type) {
		case Node:
			args[i] = c.Str(x)
		case Feature:
			args[i] = x.SelectorString(c)
		}
	}
}

func (c *OpContext) Logf(v *Vertex, format string, args ...interface{}) {
	if c.LogEval == 0 {
		// The Go compiler as of 1.24 is not very clever with no-op function calls;
		// any arguments passed to ...args above escape to the heap and allocate.
		panic("avoid calling OpContext.Logf when logging is disabled to prevent overhead")
	}
	w := &strings.Builder{}

	c.logID++
	fmt.Fprintf(w, "%3d ", c.logID)

	if c.nest > 0 {
		for i := 0; i < c.nest; i++ {
			w.WriteString("... ")
		}
	}

	if v == nil {
		fmt.Fprintf(w, format, args...)
		_ = log.Output(2, w.String())
		return
	}

	c.RewriteArgs(args...)
	n, _ := fmt.Fprintf(w, format, args...)
	if n < 60 {
		w.WriteString(strings.Repeat(" ", 60-n))
	}

	p := pMap[v]
	if p == 0 {
		p = len(pMap) + 1
		pMap[v] = p
	}
	disjunctInfo := c.disjunctInfo()
	fmt.Fprintf(w, "; n:%d %v %v%s ",
		p, c.PathToString(v.Path()), v.Path(), disjunctInfo)

	_ = log.Output(2, w.String())
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
	disjunctionSeq  int  // index into node.disjunctions
	numDisjunctions int  // number of disjunctions
	crossProductSeq int  // index into node.disjuncts (previous results)
	numPrevious     int  // index into node.disjuncts (previous results)
	numDisjuncts    int  // index into node.disjuncts (previous results)
	disjunctID      int  // unique ID for disjunct
	disjunctSeq     int  // index into node.disjunctions[disjunctionSeq].disjuncts
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

func (n *nodeContext) pushDisjunctionTask() *disjunctInfo {
	c := n.ctx
	c.currentDisjunctionID++
	id := disjunctInfo{
		node:          n,
		disjunctionID: c.currentDisjunctionID,
	}
	c.disjunctStack = append(c.disjunctStack, id)

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

	if n.ctx.LogEval > 0 {
		n.Logf("====== Do DISJUNCT %v & %v ======", d.lhs, d.rhs)
	}

	return d
}

func (d disjunctInfo) pop() {
	c := d.node.ctx
	c.disjunctStack = c.disjunctStack[:len(c.disjunctStack)-1]
}

// Format implements the fmt.Formatter interface for disjunctInfo.
func (d *disjunctInfo) Format(f fmt.State, c rune) {
	d.Write(f)
}

func (d *disjunctInfo) Write(w io.Writer) {
	// which disjunct
	fmt.Fprintf(w, " D%d:H%d:%d/%d",
		d.disjunctionID, d.holeID, d.disjunctionSeq, d.numDisjunctions)
	if d.crossProductSeq != 0 {
		fmt.Fprintf(w, " P%d/%d", d.crossProductSeq, d.numPrevious)
	}
	if d.disjunctID != 0 {
		fmt.Fprintf(w, " d%d:%d/%d",
			d.disjunctID, d.disjunctSeq, d.numDisjuncts,
		)
	}
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
		d.Write(&b)
	}
	return b.String()
}

func getCallerFunctionName(i int) (caller string, line int) {
	pc, _, line, ok := runtime.Caller(1 + i)
	if !ok {
		return "unknown", 0
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown", 0
	}
	fullName := fn.Name()
	name := filepath.Base(fullName)
	if idx := strings.LastIndex(name, "."); idx != -1 {
		name = name[idx+1:]
	}
	return name, line
}
