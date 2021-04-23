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
	"path"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func ExampleValue_Format() {
	ctx := cuecontext.New()

	v := ctx.CompileString(`
		a: 2 + b
		b: *3 | int
		s: "foo\nbar"
	`)

	fmt.Println("### ALL")
	fmt.Println(v)
	fmt.Println("---")
	fmt.Printf("%#v\n", v)
	fmt.Println("---")
	fmt.Printf("%+v\n", v)

	a := v.LookupPath(cue.ParsePath("a"))
	fmt.Println("\n### INT")
	fmt.Printf("%%v:   %v\n", a)
	fmt.Printf("%%05d: %05d\n", a)

	s := v.LookupPath(cue.ParsePath("s"))
	fmt.Println("\n### STRING")
	fmt.Printf("%%v: %v\n", s)
	fmt.Printf("%%s: %s\n", s)
	fmt.Printf("%%q: %q\n", s)

	v = ctx.CompileString(`
		#Def: a: [string]: int
		b: #Def
		b: a: {
			a: 3
			b: 3
		}
	`)
	b := v.LookupPath(cue.ParsePath("b.a"))
	fmt.Println("\n### DEF")
	fmt.Println(b)
	fmt.Println("---")
	// This will indicate that the result is closed by including a hidden
	// definition.
	fmt.Printf("%#v\n", b)

	// Output:
	// ### ALL
	// {
	// 	a: 5
	// 	b: *3 | int
	// 	s: """
	// 		foo
	// 		bar
	// 		"""
	// }
	// ---
	// a: 2 + b
	// b: *3 | int
	// s: "foo\nbar"
	// ---
	// {
	// 	a: 5
	// 	b: 3
	// 	s: """
	// 		foo
	// 		bar
	// 		"""
	// }
	//
	// ### INT
	// %v:   5
	// %05d: 00005
	//
	// ### STRING
	// %v: """
	// 	foo
	// 	bar
	// 	"""
	// %s: foo
	// bar
	// %q: "foo\nbar"
	//
	// ### DEF
	// {
	// 	a: 3
	// 	b: 3
	// }
	// ---
	// _#def
	// _#def: {
	// 	{
	// 		[string]: int
	// 	}
	// 	a: 3
	// 	b: 3
	// }
}

func TestFormat(t *testing.T) {
	tests := func(s ...string) (a [][2]string) {
		for i := 0; i < len(s); i += 2 {
			a = append(a, [2]string{s[i], s[i+1]})
		}
		return a
	}
	testCases := []struct {
		desc string
		in   string
		out  [][2]string
	}{{
		desc: "int",
		in:   `12 + 14`,
		out: tests(
			"%#v", "26",
			"%d", "26",
			"%o", "32",
			"%O", "0o32",
			"%x", "1a",
			"%X", "1A",
			"%q", `"26"`,
			"%0.3d", "026",
		),
	}, {
		desc: "float",
		in:   `12.2 + 14.4`,
		out: tests(
			"%#v", "26.6",
			"%5f", " 26.6",
			"%e", "2.66e+1",
			"%08E", "02.66E+1",
			"%g", "26.6",
			"%3G", "26.6",
		),
	}, {
		desc: "strings",
		in:   `"string"`,
		out: tests(
			"%v", `"string"`,
			"%s", "string",
			"%x", "737472696e67",
			"%X", "737472696E67",
		),
	}, {
		desc: "multiline string",
		in: `"""
		foo
		bar
		"""`,
		out: tests(
			"%#v", `"""
	foo
	bar
	"""`,
			"%s", "foo\nbar",
			"%q", `"foo\nbar"`,
		),
	}, {
		desc: "multiline bytes",
		in: `'''
			foo
			bar
			'''`,
		out: tests(
			"%#v", `'''
	foo
	bar
	'''`,
			"%s", "foo\nbar",
			"%q", `"foo\nbar"`,
		),
	}, {
		desc: "interpolation",
		in: `
		#D: {
			a: string
			b: "hello \(a)"
		}
		d: #D
		d: a: "world"
		x: *1 | int
		`,
		out: tests(
			"%v", `{
	d: {
		a: "world"
		b: "hello world"
	}
	x: *1 | int
}`,
			"%#v", `#D: {
	a: string
	b: "hello \(a)"
}
d: #D & {
	a: "world"
}
x: *1 | int`,
			"%+v", `{
	d: {
		a: "world"
		b: "hello world"
	}
	x: 1
}`,
		),
	}, {
		desc: "indent",
		in: `
a: {
	b: """
		foo
		bar
		"""
	c: int
}`,
		out: tests(
			"%v", `{
	a: {
		b: """
			foo
			bar
			"""
		c: int
	}
}`,
			"%3v", `{
				a: {
					b: """
						foo
						bar
						"""
					c: int
				}
			}`,
			"%.1v", `{
 a: {
  b: """
   foo
   bar
   """
  c: int
 }
}`,
			"%3.1v", `{
    a: {
     b: """
      foo
      bar
      """
     c: int
    }
   }`,
		),
	}, {
		desc: "imports",
		in: `
		import "strings"
		a: strings.Contains("foo")
		`,
		out: tests(
			"%v", `{
	a: strings.Contains("foo")
}`,
			"%+v", `{
	a: strings.Contains("foo")
}`,
			"%#v", `import "strings"

a: strings.Contains("foo")`,
		),
	}}
	ctx := cuecontext.New()
	for _, tc := range testCases {
		for _, test := range tc.out {
			t.Run(path.Join(tc.desc, test[0]), func(t *testing.T) {
				v := ctx.CompileString(tc.in)
				got := fmt.Sprintf(test[0], v)
				if got != test[1] {
					t.Errorf(" got: %s\nwant: %s", got, test[1])
				}
			})
		}
	}
}
