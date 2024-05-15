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

package parser_test

import (
	"testing"

	"cuelang.org/go/cue/parser"
)

func FuzzParseFile(f *testing.F) {
	// Add a wide sample of different kinds of supported syntax.
	f.Add([]byte(`
package p

import "foo"
import b "bar"
import . "baz"
`))
	f.Add([]byte(`
// some comment
// group // here
`))
	f.Add([]byte(`"some string"`))
	f.Add([]byte(`[1, 2.3, 4M, 5Gi]`))
	f.Add([]byte(`if foo { if bar if baz { x } }`))
	f.Add([]byte(`[x for x in [a, b, c]]`))
	f.Add([]byte(`foo: "bar": (baz): "\(x)": y`))
	f.Add([]byte(`{x: _, y: _|_}`))
	f.Add([]byte(`3 & int32`))
	f.Add([]byte(`string | *"foo"`))
	f.Add([]byte(`let x = y`))
	f.Add([]byte(`[1+1, 2-2, 3*3, 4/4]`))
	f.Add([]byte(`[1>1, 2>=2, 3==3, 4!=4]`))
	f.Add([]byte(`[=~"^a"]: bool`))
	f.Add([]byte(`[X=string]: Y={}`))
	f.Add([]byte(`[len(x), close(y), and([]), or([])]`))
	f.Add([]byte(`[null, bool, float, bytes, int16, uint128]`))
	f.Add([]byte(`[ [...string], {x: string, ...}]]`))
	f.Add([]byte(`{regular: x, required!: x, optional?: x}`))
	f.Add([]byte(`{_hidden: x, #Definition: x, αβ: x}`))
	f.Add([]byte(`["\u65e5本\U00008a9e", '\xff\u00FF']`))
	f.Add([]byte(`["\(expr)", #"\#(expr) \(notexpr)"#]`))
	f.Add([]byte(`{@jsonschema(id="foo"), field: string @go(Field)}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, err := parser.ParseFile("fuzz.cue", b)
		if err != nil {
			t.Skip()
		}
	})
}
