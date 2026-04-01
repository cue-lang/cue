// Copyright 2019 CUE Authors
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
	"testing"

	"cuelang.org/go/cue/parser"
)

func FuzzStandaloneCUE(f *testing.F) {
	// Add a wide sample of different kinds of supported syntax.
	f.Add(`package p`)
	f.Add(`
import "list"

list.Concat(["foo"], [])
`)
	f.Add(`
// some comment
// group // here
`)
	f.Add(`"some string"`)
	f.Add(`[1, 2.3, 4M, 5Gi]`)
	f.Add(`if foo { if bar if baz { x } }`)
	f.Add(`[x for x in [a, b, c]]`)
	f.Add(`foo: "bar": (baz): "\(x)": y`)
	f.Add(`{x: _, y: _|_}`)
	f.Add(`3 & int32`)
	f.Add(`string | *"foo"`)
	f.Add(`let x = y`)
	f.Add(`[1+1, 2-2, 3*3, 4/4]`)
	f.Add(`[1>1, 2>=2, 3==3, 4!=4]`)
	f.Add(`[=~"^a"]: bool`)
	f.Add(`[X=string]: Y={}`)
	f.Add(`[len(x), close(y), and([]), or([]), div(5, 2)]`)
	f.Add(`[null, bool, float, bytes, int16, uint128]`)
	f.Add(`[ [...string], {x: string, ...}]]`)
	f.Add(`{regular: x, required!: x, optional?: x}`)
	f.Add(`{_hidden: x, #Definition: x, αβ: x}`)
	f.Add(`["\u65e5本\U00008a9e", '\xff\u00FF']`)
	f.Add(`["\(expr)", #"\#(expr) \(notexpr)"#]`)
	f.Add(`{@jsonschema(id="foo"), field: string @go(Field,type=Other)}`)
	f.Add(`@experiment(explicitopen), out: #Schema... & data`)
	f.Add(`@experiment(aliasv2), "-foo"~A: 42`)
	f.Add(`@experiment(try), a?: int, try { b: a? + 1 }`)
	f.Add(`@experiment(try), if false { "yes" } else { "no" }`)
	f.Add(`@experiment(try), for x in [] { x } fallback { "zero" }`)
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 100 {
			t.Skip() // keep inputs reasonably small for now
		}
		_, err := parser.ParseFile("fuzz.cue", s)
		if err != nil {
			t.Skip() // skip inputs which aren't valid syntax
		}

		// TODO: cover the compiler and evaluator, and various common operations like export
		// ctx := cuecontext.New()
		// v := ctx.BuildFile(f)
		// if err := v.Err(); err != nil {
		// 	return
		// }
	})
}
