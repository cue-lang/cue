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

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
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
	f.Add(`[1, 2.3, 4M, 5Gi, 1E6, 1.e+0, .23E-10]`)
	f.Add(`if foo { if bar if baz { x } }`)
	f.Add(`[x for x in [a, b, c]]`)
	f.Add(`foo: "bar ": (baz): "\(x)": y`)
	f.Add(`{x: _, y: _|_}`)
	f.Add(`3 & int32`)
	f.Add(`string | *"foo"`)
	f.Add(`let x = y`)
	f.Add(`[1+1, 2-2, 3*3, 4/4]`)
	f.Add(`[1>1, 2>=2, 3==3, 4!=4]`)
	f.Add(`[=~"^a"]: bool`)
	f.Add(`[X=string]: Y={}`)
	f.Add(`[len(x), close({y}), and([]), or([]), div(5, 2)]`)
	f.Add(`value: "foo" & matchN(2, [string, !="bar", <4])`)
	f.Add(`a: b: a`)
	f.Add(`[null, bool, float, bytes, int16, uint128]`)
	f.Add(`[ [...string], {x: string, ...}]]`)
	f.Add(`{regular: x, required!: x, optional?: x}`)
	f.Add(`{_hidden: x, #Definition: x, αβ: x}`)
	f.Add(`["\u65e5本\U00008a9e", '\xff\003']`)
	f.Add(`"""
	here is a multiline
	string literal
	"""`)
	f.Add(`'''
	here is a multiline
	bytes literal
	'''`)
	f.Add(`["\(expr)", #"\#(expr) \(notexpr)"#]`)
	f.Add(`##"""
		\(these are not
		#\(interpolations
		"""##`)
	f.Add(`{@jsonschema(id="foo"), field: string @go(Field,type=Other)}`)
	f.Add(`@experiment(explicitopen), out: #Schema... & data`)
	f.Add(`@experiment(aliasv2), "-foo"~A: 42`)
	f.Add(`@experiment(try), a?: int, try { b: a? + 1 }`)
	f.Add(`@experiment(try), if false { "yes" } else { "no" }`)
	f.Add(`@experiment(try), for x in [] { x } fallback { "zero" }`)
	f.Add(`a[0], b["foo"], #c.#bar, _d._baz`)
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 100 {
			t.Skip() // keep inputs reasonably small for now
		}
		f, err := parser.ParseFile("fuzz.cue", s, parser.ParseComments)
		// Check ParseExpr too. Note that a valid file is not always a valid expression,
		// but we still want to check that ParseExpr doesn't panic.
		_, exprErr := parser.ParseExpr("fuzz.cue", s, parser.ParseComments)
		if err != nil {
			// Per the spec, "package" and "import" are valid identifiers after the preamble,
			// so ParseExpr and IsValidIdent correctly accept them.
			if s != "package" && s != "import" {
				if exprErr == nil {
					t.Errorf("ParseFile rejects this input but ParseExpr does not: %q", s)
				}
				if ast.IsValidIdent(s) {
					t.Errorf("cue/parser rejects this identifier but cue/ast accepts it as valid: %q", s)
				}
			}
			var info literal.NumInfo
			if err := literal.ParseNum(s, &info); err == nil {
				t.Errorf("cue/parser rejects this number but cue/literal accepts it as valid: %q", s)
			}
			if _, err := literal.Unquote(s); err == nil {
				t.Errorf("cue/parser rejects this string but cue/literal accepts it as valid: %q", s)
			}

			// Nothing else to do for invalid syntax; stop here.
			return
		}

		// Common operations with the syntax tree.
		if _, err := format.Node(f); err != nil {
			t.Errorf("cue/format should not fail on parsed input: %v", err)
		}
		ast.Walk(f,
			func(ast.Node) bool { return true },
			func(ast.Node) {},
		)
		astutil.Apply(f,
			func(c astutil.Cursor) bool {
				node := c.Node()
				_ = node.Pos().Position()
				_ = node.End().Position()
				switch node := node.(type) {
				case *ast.Ident:
					if !ast.IsValidIdent(node.Name) {
						t.Errorf("cue/parser accepts this identifier as valid but cue/ast does not: %q", node.Name)
					}
				case *ast.BasicLit:
					switch node.Kind {
					case token.INT, token.FLOAT:
						var info literal.NumInfo
						if err := literal.ParseNum(node.Value, &info); err != nil {
							t.Errorf("cue/parser accepts this number as valid but cue/literal does not: %q", node.Value)
						}
					case token.STRING:
						if _, ok := c.Parent().Node().(*ast.Interpolation); ok {
							// An interpolation consists of incomplete basic literals like: "\(
							break
						}
						if _, err := literal.Unquote(node.Value); err != nil {
							t.Errorf("cue/parser accepts this string as valid but cue/literal does not: %q", node.Value)
						}
					}
				}

				return true
			},
			func(astutil.Cursor) bool { return true },
		)

		// TODO: cover the compiler and evaluator, and various common operations like export
		// ctx := cuecontext.New()
		// v := ctx.BuildFile(f)
		// if err := v.Err(); err != nil {
		// 	return
		// }
	})
}
