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
	"fmt"
	"log"
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
)

func TestExport(t *testing.T) {
	testCases := []struct {
		raw     bool // skip evaluation the root, fully raw
		eval    bool // evaluate the full export
		in, out string
	}{{
		in:  `"hello"`,
		out: `"hello"`,
	}, {
		in:  `'hello'`,
		out: `'hello'`,
	}, {
		in: `'hello\nworld'`,
		out: "'''" +
			multiSep + "hello" +
			multiSep + "world" +
			multiSep + "'''",
	}, {
		in: `"hello\nworld"`,
		out: `"""` +
			multiSep + "hello" +
			multiSep + "world" +
			multiSep + `"""`,
	}, {
		in: "{ a: 1, b: a + 2, c: null, d: true, e: _, f: string }",
		out: unindent(`
			{
				a: 1
				b: 3
				c: null
				d: true
				e: _
				f: string
			}`),
	}, {
		in: `{ a: { b: 2.0, s: "abc" }, b: a.b, c: a.c, d: a["d"], e: a.t[2:3] }`,
		out: unindent(`
			{
				a: {
					b: 2.0
					s: "abc"
				}
				b: 2.0
				c: _|_ /* undefined field "c" */
				d: _|_ /* undefined field "d" */
				e: _|_ /* undefined field "t" */
			}`),
	}, {
		in: `{
			a: 5*[int]
			a: [1, 2, ...]
			b: <=5*[int]
			b: [1, 2, ...]
			c: (>=3 & <=5)*[int]
			c: [1, 2, ...]
			d: >=2*[int]
			d: [1, 2, ...]
			e: [...int]
			e: [1, 2, ...]
			f: [1, 2, ...]
		}`,
		out: unindent(`
		{
			a: [1, 2, int, int, int]
			b: <=5*[int] & [1, 2, ...]
			c: (>=3 & <=5)*[int] & [1, 2, ...]
			d: >=2*[int] & [1, 2, ...]
			e: [1, 2]
			f: [1, 2]
		}`),
	}, {
		raw: true,
		in: `{
			a: 5*[int]
			a: [1, 2, ...]
			b: <=5*[int]
			b: [1, 2, ...]
			c: (>=3 & <=5)*[int]
			c: [1, 2, ...]
			d: >=2*[int]
			d: [1, 2, ...]
			e: [...int]
			e: [1, 2, ...]
			f: [1, 2, ...]
		}`,
		out: unindent(`
		{
			a: 5*[int] & [1, 2, ...]
			b: <=5*[int] & [1, 2, ...]
			c: (>=3 & <=5)*[int] & [1, 2, ...]
			d: >=2*[int] & [1, 2, ...]
			e: [...int] & [1, 2, ...]
			f: [1, 2, ...]
		}`),
	}, {
		raw: true,
		in:  `{ a: { b: [] }, c: a.b, d: a["b"] }`,
		out: unindent(`
			{
				a b: []
				c: a.b
				d: a["b"]
			}`),
	}, {
		raw: true,
		in:  `{ a: *"foo" | *"bar" | *string | int, b: a[2:3] }`,
		out: unindent(`
			{
				a: *"foo" | *"bar" | *string | int
				b: a[2:3]
			}`),
	}, {
		in: `{
			a: >=0 & <=10 & !=1
		}`,
		out: unindent(`
			{
				a: >=0 & <=10 & !=1
			}`),
	}, {
		raw: true,
		in: `{
				a: >=0 & <=10 & !=1
			}`,
		out: unindent(`
			{
				a: >=0 & <=10 & !=1
			}`),
	}, {
		raw:  true,
		eval: true,
		in: `{
				a: (*1 | 2) & (1 | *2)
				b: [(*1 | 2) & (1 | *2)]
			}`,
		out: unindent(`
			{
				a: 1 | 2
				b: [1 | 2]
			}`),
	}, {
		raw:  true,
		eval: true,
		in: `{
				u16: int & >=0 & <=65535
				u32: uint32
				u64: uint64
				u128: uint128
				u8: uint8
				ua: uint16 & >0
				us: >=0 & <10_000 & int
				i16: >=-32768 & int & <=32767
				i32: int32 & > 0
				i64:  int64
				i128: int128
				f64:  float64
				fi:  float64 & int
			}`,
		out: unindent(`
			{
				u16:  uint16
				u32:  uint32
				u64:  uint64
				u128: uint128
				u8:   uint8
				ua:   uint16 & >0
				us:   uint & <10000
				i16:  int16
				i32:  int32 & >0
				i64:  int64
				i128: int128
				f64:  float64
				fi:   int & float64
			}`),
	}, {
		raw: true,
		in:  `{ a: [1, 2], b: { "\(k)": v for k, v in a if v > 1 } }`,
		out: unindent(`
			{
				a: [1, 2]
				b: {
					"\(k)": v for k, v in a if v > 1
				}
			}`),
	}, {
		raw: true,
		in:  `{ a: [1, 2], b: [ v for k, v in a ] }`,
		out: unindent(`
			{
				a: [1, 2]
				b: [ v for k, v in a ]
			}`),
	}, {
		raw: true,
		in:  `{ a: >=0 & <=10, b: "Count: \(a) times" }`,
		out: unindent(`
			{
				a: >=0 & <=10
				b: "Count: \(a) times"
			}`),
	}, {
		raw: true,
		in:  `{ a: "", b: len(a) }`,
		out: unindent(`
				{
					a: ""
					b: len(a)
				}`),
	}, {
		raw:  true,
		eval: true,
		in: `{
			b: {
				idx: a[str]
				str: string
			}
			b a b: 4
			a b: 3
		}`,
		// reference to a must be redirected to outer a through alias
		out: unindent(`
		{
			A = a
			b: {
				idx: A[str]
				a b: 4
				str: string
			}
			a b: 3
		}`),
	}, {
		raw:  true,
		eval: true,
		in: `{
			job <Name>: {
				name:     Name
				replicas: uint | *1 @protobuf(10)
				command:  string
			}
			
			job list command: "ls"

			job nginx: {
				command:  "nginx"
				replicas: 2
			}
		}`,
		out: unindent(`
		{
			job: {
				list: {
					name:     "list"
					replicas: 1 @protobuf(10)
					command:  "ls"
				}
				nginx: {
					name:     "nginx"
					replicas: 2 @protobuf(10)
					command:  "nginx"
				}
			}
		}`),
	}, {
		raw:  true,
		eval: true,
		in: `{
				b: [{
					<X>: int
					f: 4 if a > 4
				}][a]
				a: int
				c: *1 | 2
			}`,
		// reference to a must be redirected to outer a through alias
		out: unindent(`
			{
				b: [{
					<X>: int
					f:   4 if a > 4
				}][a]
				a: int
				c: 1
			}`),
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			body := fmt.Sprintf("Test: %s", tc.in)
			ctx, obj := compileFile(t, body)
			ctx.trace = *traceOn
			var root value = obj
			if !tc.raw {
				root = testResolve(ctx, obj, evalFull)
			}
			t.Log(debugStr(ctx, root))

			n := root.(*structLit).arcs[0].v
			v := newValueRoot(ctx, n)

			opts := options{raw: !tc.eval}
			b, err := format.Node(export(ctx, v.eval(ctx), opts), format.Simplify())
			if err != nil {
				log.Fatal(err)
			}
			if got := string(b); got != tc.out {
				t.Errorf("\ngot  %v;\nwant %v", got, tc.out)
			}
		})
	}
}

func TestExportFile(t *testing.T) {
	testCases := []struct {
		eval    bool // evaluate the full export
		in, out string
	}{{
		in: `
		import "strings"

		a: strings.ContainsAny("c")
		`,
		out: unindent(`
		import "strings"

		a: strings.ContainsAny("c")`),
	}, {
		in: `
		import "strings"

		a: strings.TrimSpace("  c  ")
		`,
		out: unindent(`
		import "strings"

		a: strings.TrimSpace("  c  ")`),
	}, {
		in: `
		import "strings"

		stringsx = strings

		a: {
			strings: stringsx.ContainsAny("c")
		}
		`,
		out: unindent(`
		import "strings"

		STRINGS = strings
		a strings: STRINGS.ContainsAny("c")`),
	}, {
		in: `
			a: b - 100
			b: a + 100
		`,
		out: unindent(`
		{
			a: b - 100
			b: a + 100
		}`),
	}, {
		in: `A: {
			<_>: B
		} @protobuf(1,"test")

		B: {}
		B: {a: int} | {b: int}
		`,
		out: unindent(`
		{
			A: {
				<_>: B
			} @protobuf(1,"test")
			B: {
			} & ({
				a: int
			} | {
				b: int
			})
		}`),
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			var r Runtime
			inst, err := r.Parse("test", tc.in)
			if err != nil {
				t.Fatal(err)
			}
			b, err := format.Node(inst.Value().Syntax(Raw()), format.Simplify())
			if err != nil {
				log.Fatal(err)
			}
			if got := strings.TrimSpace(string(b)); got != tc.out {
				t.Errorf("\ngot:\n%v\nwant:\n%v", got, tc.out)
			}
		})
	}
}

func unindent(s string) string {
	lines := strings.Split(s, "\n")[1:]
	ws := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], " \t"))]
	for i, s := range lines {
		if s == "" {
			continue
		}
		if !strings.HasPrefix(s, ws) {
			panic("invalid indentation")
		}
		lines[i] = lines[i][len(ws):]
	}
	return strings.Join(lines, "\n")
}
