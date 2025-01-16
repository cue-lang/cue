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
	if v == nil {
		s := fmt.Sprintf(strings.Repeat("..", c.nest)+format, args...)
		_ = log.Output(2, s)
		return
	}
	p := pMap[v]
	if p == 0 {
		p = len(pMap) + 1
		pMap[v] = p
	}
	a := append([]interface{}{
		strings.Repeat("..", c.nest),
		p,
		v.Label.SelectorString(c),
		v.Path(),
	}, args...)
	for i := 2; i < len(a); i++ {
		switch x := a[i].(type) {
		case Node:
			a[i] = c.Str(x)
		case Feature:
			a[i] = x.SelectorString(c)
		}
	}
	s := fmt.Sprintf("%s [%d] %s/%v"+format, a...)
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
