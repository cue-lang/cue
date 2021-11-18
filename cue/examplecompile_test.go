// Copyright 2021 CUE Authors
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

package cue_test

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func ExampleContext() {
	ctx := cuecontext.New()

	v := ctx.CompileString(`
		a: 2
		b: 3
		"a+b": a + b
	`)

	p("lookups")
	p("a:     %v", v.LookupPath(cue.ParsePath("a")))
	p("b:     %v", v.LookupPath(cue.ParsePath("b")))
	p(`"a+b": %v`, v.LookupPath(cue.ParsePath(`"a+b"`)))
	p("")
	p("expressions")
	p("a + b: %v", ctx.CompileString("a + b", cue.Scope(v)))
	p("a * b: %v", ctx.CompileString("a * b", cue.Scope(v)))

	// Output:
	// lookups
	// a:     2
	// b:     3
	// "a+b": 5
	//
	// expressions
	// a + b: 5
	// a * b: 6
}

func p(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}
