// Copyright 2020 CUE Authors
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

package subsume_test

import (
	"regexp"
	"strings"
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/subsume"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
)

const (
	subNone = iota
	subFinal
	subNoOptional
	subSchema
	subDefaults
)

func TestValues(t *testing.T) {
	// Do not inline: the named struct is used as a marker in
	// testdata/gen.go.
	type subsumeTest struct {
		// the result of b ⊑ a, where a and b are defined in "in"
		err  string
		in   string
		mode int

		skip_v2 bool // Bug only exists in v2. Won't fix.
	}
	testCases := []subsumeTest{
		// Top subsumes everything
		{
			in:  `a: _, b: _ `,
			err: "",
		},
		{
			in:  `a: _, b: null `,
			err: "",
		},
		{
			in:  `a: _, b: int `,
			err: "",
		},
		{
			in:  `a: _, b: 1 `,
			err: "",
		},
		{
			in:  `a: _, b: float `,
			err: "",
		},
		{
			in:  `a: _, b: "s" `,
			err: "",
		},
		{
			in:  `a: _, b: {} `,
			err: "",
		},
		{
			in:  `a: _, b: []`,
			err: "",
		},
		{
			in:  `a: _, b: _|_ `,
			err: "",
		},

		// Nothing besides top subsumed top
		{
			in:  `a: null,    b: _`,
			err: "value not an instance",
		},
		{
			in:  `a: int, b: _`,
			err: "value not an instance",
		},
		{
			in:  `a: 1,       b: _`,
			err: "value not an instance",
		},
		{
			in:  `a: float, b: _`,
			err: "value not an instance",
		},
		{
			in:  `a: "s",     b: _`,
			err: "value not an instance",
		},
		{
			in:  `a: {},      b: _`,
			err: "value not an instance",
		},
		{
			in:  `a: [],      b: _`,
			err: "list does not subsume _ (type _) (and 1 more errors)",
		},
		{
			in:  `a: _|_ ,      b: _`,
			err: "value not an instance",
		},

		// Bottom subsumes nothing except bottom itself.
		{
			in:  `a: _|_, b: null `,
			err: "value not an instance",
		},
		{
			in:  `a: _|_, b: int `,
			err: "value not an instance",
		},
		{
			in:  `a: _|_, b: 1 `,
			err: "value not an instance",
		},
		{
			in:  `a: _|_, b: float `,
			err: "value not an instance",
		},
		{
			in:  `a: _|_, b: "s" `,
			err: "value not an instance",
		},
		{
			in:  `a: _|_, b: {} `,
			err: "value not an instance",
		},
		{
			in:  `a: _|_, b: [] `,
			err: "value not an instance",
		},
		{
			in:  ` a: _|_, b: _|_ `,
			err: "",
		},

		// All values subsume bottom
		{
			in:  `a: null,    b: _|_`,
			err: "",
		},
		{
			in:  `a: int, b: _|_`,
			err: "",
		},
		{
			in:  `a: 1,       b: _|_`,
			err: "",
		},
		{
			in:  `a: float, b: _|_`,
			err: "",
		},
		{
			in:  `a: "s",     b: _|_`,
			err: "",
		},
		{
			in:  `a: {},      b: _|_`,
			err: "",
		},
		{
			in:  `a: [],      b: _|_`,
			err: "",
		},
		{
			in:  `a: true,    b: _|_`,
			err: "",
		},
		{
			in:  `a: _|_,       b: _|_`,
			err: "",
		},

		// null subsumes only null
		{
			in:  ` a: null, b: null `,
			err: "",
		},
		{
			in:  `a: null, b: 1 `,
			err: "value not an instance",
		},
		{
			in:  `a: 1,    b: null `,
			err: "value not an instance",
		},

		{
			in:  ` a: true, b: true `,
			err: "",
		},
		{
			in:  `a: true, b: false `,
			err: "value not an instance",
		},

		{
			in:  ` a: "a",    b: "a" `,
			err: "",
		},
		{
			in:  `a: "a",    b: "b" `,
			err: "value not an instance",
		},
		{
			in:  ` a: string, b: "a" `,
			err: "",
		},
		{
			in:  `a: "a",    b: string `,
			err: "value not an instance",
		},

		// Number typing (TODO)
		//
		// In principle, an "int" cannot assume an untyped "1", as "1" may
		// still by typed as a float. They are two different type aspects. When
		// considering, keep in mind that:
		//   Key requirement: if A subsumes B, it must not be possible to
		//   specialize B further such that A does not subsume B. HOWEVER,
		//   The type conversion rules for conversion are INDEPENDENT of the
		//   rules for subsumption!
		// Consider:
		// - only having number, but allowing user-defined types.
		//   Subsumption would still work the same, but it may be somewhat
		//   less weird.
		// - making 1 always an int and 1.0 always a float.
		//   - the int type would subsume any derived type from int.
		//   - arithmetic would allow implicit conversions, but maybe not for
		//     types.
		//
		// TODO: irrational numbers: allow untyped, but require explicit
		//       trunking when assigning to float.
		//
		// a: number; cue.IsInteger(a) && a > 0
		// t: (x) -> number; cue.IsInteger(a) && a > 0
		// type x number: cue.IsInteger(x) && x > 0
		// x: typeOf(number); cue.IsInteger(x) && x > 0
		{
			in:  `a: 1, b: 1 `,
			err: "",
		},
		{
			in:  `a: 1.0, b: 1.0 `,
			err: "",
		},
		{
			in:  `a: 3.0, b: 3.0 `,
			err: "",
		},
		{
			in:  `a: 1.0, b: 1 `,
			err: "value not an instance",
		},
		{
			in:  `a: 1, b: 1.0 `,
			err: "value not an instance",
		},
		{
			in:  `a: 3, b: 3.0`,
			err: "value not an instance",
		},
		{
			in:  `a: int, b: 1`,
			err: "",
		},
		{
			in:  `a: int, b: int & 1`,
			err: "",
		},
		{
			in:  `a: float, b: 1.0`,
			err: "",
		},
		{
			in:  `a: float, b: 1`,
			err: "value not an instance",
		},
		{
			in:  `a: int, b: 1.0`,
			err: "value not an instance",
		},
		{
			in:  `a: int, b: int`,
			err: "",
		},
		{
			in:  `a: number, b: int`,
			err: "",
		},

		// Structs
		{
			in:  `a: {}, b: {}`,
			err: "",
		},
		{
			in:  `a: {}, b: {a: 1}`,
			err: "",
		},
		{
			in:  `a: {a:1}, b: {a:1, b:1}`,
			err: "",
		},
		{
			in:  `a: {s: { a:1} }, b: { s: { a:1, b:2 }}`,
			err: "",
		},
		{
			in:  `a: {}, b: {}`,
			err: "",
		},
		// TODO: allow subsumption of unevaluated values?
		// ref not yet evaluated and not structurally equivalent
		{
			in:  `a: {}, b: {} & c, c: {}`,
			err: "",
		},

		{
			in:  `a: {a:1}, b: {}`,
			err: "regular field is constraint in subsumed value: a (and 1 more errors)",
		},
		{
			in:  `a: {a:1, b:1}, b: {a:1}`,
			err: "regular field is constraint in subsumed value: b (and 1 more errors)",
		},
		{
			in:  `a: {s: { a:1} }, b: { s: {}}`,
			err: "regular field is constraint in subsumed value: a (and 2 more errors)",
		},

		{
			in:  `a: 1 | 2, b: 2 | 1`,
			err: "",
		},
		{
			in:  `a: 1 | 2, b: 1 | 2`,
			err: "",
		},

		{
			in:  `a: number, b: 2 | 1`,
			err: "",
		},
		{
			in:  `a: number, b: 2 | 1`,
			err: "",
		},
		{
			in:  `a: int, b: 1 | 2 | 3.1`,
			err: "value not an instance",
		},

		{
			in:  `a: float | number, b: 1 | 2 | 3.1`,
			err: "",
		},

		{
			in:  `a: int, b: 1 | 2 | 3.1`,
			err: "value not an instance",
		},
		{
			in:  `a: 1 | 2, b: 1`,
			err: "",
		},
		{
			in:  `a: 1 | 2, b: 2`,
			err: "",
		},
		{
			in:  `a: 1 | 2, b: 3`,
			err: "value not an instance",
		},

		// 147: {subsumes: true, in: ` a: 7080, b: *7080 | int`, mode: subChoose},

		// Defaults
		{
			in:  `a: number | *1, b: number | *2`,
			err: "value not an instance",
		},
		{
			in:  `a: number | *2, b: number | *2`,
			err: "",
		},
		{
			in:  `a: int | *float, b: int | *2.0`,
			err: "",
		},
		{
			in:  `a: int | *2, b: int | *2.0`,
			err: "value not an instance",
		},
		{
			in:  `a: number | *2 | *3, b: number | *2`,
			err: "",
		},
		{
			in:  `a: number, b: number | *2`,
			err: "",
		},

		// Bounds
		{
			in:  `a: >=2, b: >=2`,
			err: "",
		},
		{
			in:  `a: >=1, b: >=2`,
			err: "",
		},
		{
			in:  `a: >0, b: >=2`,
			err: "",
		},
		{
			in:  `a: >1, b: >1`,
			err: "",
		},
		{
			in:  `a: >=1, b: >1`,
			err: "",
		},
		{
			in:  `a: >1, b: >=1`,
			err: "value not an instance",
		},
		{
			in:  `a: >=1, b: >=1`,
			err: "",
		},
		{
			in:  `a: <1, b: <1`,
			err: "",
		},
		{
			in:  `a: <=1, b: <1`,
			err: "",
		},
		{
			in:  `a: <1, b: <=1`,
			err: "value not an instance",
		},
		{
			in:  `a: <=1, b: <=1`,
			err: "",
		},

		{
			in:  `a: !=1, b: !=1`,
			err: "",
		},
		{
			in:  `a: !=1, b: !=2`,
			err: "value not an instance",
		},

		{
			in:  `a: !=1, b: <=1`,
			err: "value not an instance",
		},
		{
			in:  `a: !=1, b: <1`,
			err: "",
		},
		{
			in:  `a: !=1, b: >=1`,
			err: "value not an instance",
		},
		{
			in:  `a: !=1, b: <1`,
			err: "",
		},

		{
			in:  `a: !=1, b: <=0`,
			err: "",
		},
		{
			in:  `a: !=1, b: >=2`,
			err: "",
		},
		{
			in:  `a: !=1, b: >1`,
			err: "",
		},

		{
			in:  `a: >=2, b: !=2`,
			err: "value not an instance",
		},
		{
			in:  `a: >2, b: !=2`,
			err: "value not an instance",
		},
		{
			in:  `a: <2, b: !=2`,
			err: "value not an instance",
		},
		{
			in:  `a: <=2, b: !=2`,
			err: "value not an instance",
		},

		{
			in:  `a: =~"foo", b: =~"foo"`,
			err: "",
		},
		{
			in:  `a: =~"foo", b: =~"bar"`,
			err: "value not an instance",
		},
		{
			in:  `a: =~"foo1", b: =~"foo"`,
			err: "value not an instance",
		},

		{
			in:  `a: !~"foo", b: !~"foo"`,
			err: "",
		},
		{
			in:  `a: !~"foo", b: !~"bar"`,
			err: "value not an instance",
		},
		{
			in:  `a: !~"foo", b: !~"foo1"`,
			err: "value not an instance",
		},

		// The following is could be true, but we will not go down the rabbit
		// hold of trying to prove subsumption of regular expressions.
		{
			in:  `a: =~"foo", b: =~"foo1"`,
			err: "value not an instance",
		},
		{
			in:  `a: !~"foo1", b: !~"foo"`,
			err: "value not an instance",
		},

		{
			in:  `a: <5, b: 4`,
			err: "",
		},
		{
			in:  `a: <5, b: 5`,
			err: "value not an instance",
		},
		{
			in:  `a: <=5, b: 5`,
			err: "",
		},
		{
			in:  `a: <=5.0, b: 5.00000001`,
			err: "value not an instance",
		},
		{
			in:  `a: >5, b: 6`,
			err: "",
		},
		{
			in:  `a: >5, b: 5`,
			err: "value not an instance",
		},
		{
			in:  `a: >=5, b: 5`,
			err: "",
		},
		{
			in:  `a: >=5, b: 4`,
			err: "value not an instance",
		},
		{
			in:  `a: !=5, b: 6`,
			err: "",
		},
		{
			in:  `a: !=5, b: 5`,
			err: "value not an instance",
		},
		{
			in:  `a: !=5.0, b: 5.0`,
			err: "value not an instance",
		},
		{
			in:  `a: !=5.0, b: 5`,
			err: "value not an instance",
		},

		{
			in:  `a: =~ #"^\d{3}$"#, b: "123"`,
			err: "",
		},
		{
			in:  `a: =~ #"^\d{3}$"#, b: "1234"`,
			err: "value not an instance",
		},
		{
			in:  `a: !~ #"^\d{3}$"#, b: "1234"`,
			err: "",
		},
		{
			in:  `a: !~ #"^\d{3}$"#, b: "123"`,
			err: "value not an instance",
		},

		// Conjunctions
		{
			in:  `a: >0, b: >=2 & <=100`,
			err: "",
		},
		{
			in:  `a: >0, b: >=0 & <=100`,
			err: "value not an instance",
		},

		{
			in:  `a: >=0 & <=100, b: 10`,
			err: "",
		},
		{
			in:  `a: >=0 & <=100, b: >=0 & <=100`,
			err: "",
		},
		{
			in:  `a: !=2 & !=4, b: >3`,
			err: "value not an instance",
		},
		{
			in:  `a: !=2 & !=4, b: >5`,
			err: "",
		},

		{
			in:  `a: >=0 & <=100, b: >=0 & <=150`,
			err: "value not an instance",
		},
		{
			in:  `a: >=0 & <=150, b: >=0 & <=100`,
			err: "",
		},

		// Disjunctions
		{
			in:  `a: >5, b: >10 | 8`,
			err: "",
		},
		{
			in:  `a: >8, b: >10 | 8`,
			err: "value not an instance",
		},

		// Optional fields
		// Optional fields defined constraints on fields that are not yet
		// defined. So even if such a field is not part of the output, it
		// influences the lattice structure.
		// For a given A and B, where A and B unify and where A has an optional
		// field that is not defined in B, the addition of an incompatible
		// value of that field in B can cause A and B to no longer unify.
		//
		{
			in:  `a: {foo: 1}, b: {}`,
			err: "regular field is constraint in subsumed value: foo (and 1 more errors)",
		},
		{
			in:  `a: {foo?: 1}, b: {}`,
			err: "field foo not present in {} (and 1 more errors)",
		},
		{
			in:  `a: {}, b: {foo: 1}`,
			err: "",
		},
		{
			in:  `a: {}, b: {foo?: 1}`,
			err: "",
		},

		{
			in:  `a: {foo: 1}, b: {foo: 1}`,
			err: "",
		},
		{
			in:  `a: {foo?: 1}, b: {foo: 1}`,
			err: "",
		},
		{
			in:  `a: {foo?: 1}, b: {foo?: 1}`,
			err: "",
		},
		{
			in:  `a: {foo: 1}, b: {foo?: 1}`,
			err: `field foo not present in {foo?:1} (and 1 more errors)`,
		},

		{
			in:  `a: {foo: 1}, b: {foo: 2}`,
			err: `field foo not present in {foo:2} (and 1 more errors)`,
		},
		{
			in:  `a: {foo?: 1}, b: {foo: 2}`,
			err: `field foo not present in {foo:2} (and 1 more errors)`,
		},
		{
			in:  `a: {foo?: 1}, b: {foo?: 2}`,
			err: `field foo not present in {foo?:2} (and 1 more errors)`,
		},
		{
			in:  `a: {foo: 1}, b: {foo?: 2}`,
			err: `field foo not present in {foo?:2} (and 1 more errors)`,
		},

		{
			in:  `a: {foo: number}, b: {foo: 2}`,
			err: "",
		},
		{
			in:  `a: {foo?: number}, b: {foo: 2}`,
			err: "",
		},
		{
			in:  `a: {foo?: number}, b: {foo?: 2}`,
			err: "",
		},
		{
			in:  `a: {foo: number}, b: {foo?: 2}`,
			err: `field foo not present in {foo?:2} (and 1 more errors)`,
		},

		{
			in:  `a: {foo: 1}, b: {foo: number}`,
			err: `field foo not present in {foo:number} (and 1 more errors)`,
		},
		{
			in:  `a: {foo?: 1}, b: {foo: number}`,
			err: `field foo not present in {foo:number} (and 1 more errors)`,
		},
		{
			in:  `a: {foo?: 1}, b: {foo?: number}`,
			err: `field foo not present in {foo?:number} (and 1 more errors)`,
		},
		{
			in:  `a: {foo: 1}, b: {foo?: number}`,
			err: `field foo not present in {foo?:number} (and 1 more errors)`,
		},

		// The one exception of the rule: there is no value of foo that can be
		// added to b which would cause the unification of a and b to fail.
		// So an optional field with a value of top is equivalent to not
		// defining one at all.
		{
			in:  `a: {foo?: _}, b: {}`,
			err: "",
		},

		{
			in:  `a: {[_]: 4}, b: {[_]: int}`,
			err: "value not an instance",
		},
		{
			in:      `a: {[_]: int}, b: {[_]: 2}`,
			skip_v2: true,
			err:     "",
		},
		{
			in:      `a: {[string]: int, [<"m"]: 3}, b: {[string]: 2, [<"m"]: 3}`,
			skip_v2: true,
			err:     "",
		},
		{
			in:      `a: {[<"m"]: 3, [string]: int}, b: {[string]: 2, [<"m"]: 3}`,
			skip_v2: true,
			err:     "",
		},
		{
			in:  `a: {[<"n"]: 3, [string]: int}, b: {[string]: 2, [<"m"]: 3}`,
			err: "value not an instance: inexact subsumption",
		},
		{
			// both sides unify to a single string pattern.
			in:      `a: {[string]: <5, [string]: int}, b: {[string]: <=3, [string]: 3}`,
			skip_v2: true,
			err:     "",
		},
		{
			// matches because bottom is subsumed by >5
			in:      `a: {[string]: >5}, b: {[string]: 1, [string]: 2}`,
			skip_v2: true,
			err:     "",
		},
		{
			// subsumption gives up if a has more pattern constraints than b.
			// TODO: support this?
			in:  `a: {[_]: >5, [>"b"]: int}, b: {[_]: 6}`,
			err: "value not an instance: inexact subsumption",
		},
		{
			in:  `a: {}, b: {[_]: 6}`,
			err: "",
		},

		// TODO: the subNoOptional mode used to be used by the equality check.
		// Now this has its own implementation it is no longer necessary. Keep
		// around for now in case we still need the more permissive equality
		// check that can be created by using subsumption.
		//
		// 440: {subsumes: true, in: `a: {foo?: 1}, b: {}`, mode: subNoOptional},
		// 441: {subsumes: true, in: `a: {}, b: {foo?: 1}`, mode: subNoOptional},
		// 442: {subsumes: true, in: `a: {foo?: 1}, b: {foo: 1}`, mode: subNoOptional},
		// 443: {subsumes: true, in: `a: {foo?: 1}, b: {foo?: 1}`, mode: subNoOptional},
		// 444: {subsumes: false, in: `a: {foo: 1}, b: {foo?: 1}`, mode: subNoOptional},
		// 445: {subsumes: true, in: `a: close({}), b: {foo?: 1}`, mode: subNoOptional},
		// 446: {subsumes: true, in: `a: close({}), b: close({foo?: 1})`, mode: subNoOptional},
		// 447: {subsumes: true, in: `a: {}, b: close({})`, mode: subNoOptional},
		// 448: {subsumes: true, in: `a: {}, b: close({foo?: 1})`, mode: subNoOptional},

		// embedded scalars
		{
			in:  `a: {1, #foo: number}, b: {1, #foo: 1}`,
			err: "",
		},
		{
			in:  `a: {1, #foo?: number}, b: {1, #foo: 1}`,
			err: "",
		},
		{
			in:  `a: {1, #foo?: number}, b: {1, #foo?: 1}`,
			err: "",
		},
		{
			in:  `a: {1, #foo: number}, b: {1, #foo?: 1}`,
			err: `field #foo not present in 1 (and 1 more errors)`,
		},

		{
			in:  `a: {int, #foo: number}, b: {1, #foo: 1}`,
			err: "",
		},
		{
			in:  `a: {int, #foo: 1}, b: {1, #foo: number}`,
			err: `field #foo not present in 1 (and 1 more errors)`,
		},
		{
			in:  `a: {1, #foo: number}, b: {int, #foo: 1}`,
			err: "value not an instance",
		},
		{
			in:  `a: {1, #foo: 1}, b: {int, #foo: number}`,
			err: "value not an instance",
		},

		// Lists
		{
			in:  `a: [], b: [] `,
			err: "",
		},
		{
			in:  `a: [1], b: [1] `,
			err: "",
		},
		{
			in:  `a: [1], b: [2] `,
			err: "value not an instance",
		},
		{
			in:  `a: [1], b: [2, 3] `,
			err: "value not an instance",
		},
		{
			in:  `a: [{b: string}], b: [{b: "foo"}] `,
			err: "",
		},
		{
			in:  `a: [...{b: string}], b: [{b: "foo"}] `,
			err: "",
		},
		{
			in:  `a: [{b: "foo"}], b: [{b: string}] `,
			err: `field b not present in {b:string} (and 1 more errors)`,
		},
		{
			in:  `a: [{b: string}], b: [{b: "foo"}, ...{b: "foo"}] `,
			err: "value not an instance",
		},
		{
			in:  `a: [_, int, ...], b: [int, string, ...string] `,
			err: "value not an instance",
		},

		// Closed structs.
		{
			in:  `a: close({}), b: {a: 1}`,
			err: "value not an instance",
		},
		{
			in:  `a: close({a: 1}), b: {a: 1}`,
			err: "value not an instance",
		},
		{
			in:  `a: close({a: 1, b: 1}), b: {a: 1}`,
			err: "value not an instance",
		},
		{
			in:  `a: {a: 1}, b: close({})`,
			err: "regular field is constraint in subsumed value: a (and 1 more errors)",
		},
		{
			in:  `a: {a: 1}, b: close({a: 1})`,
			err: "",
		},
		{
			in:  `a: {a: 1}, b: close({a: 1, b: 1})`,
			err: "",
		},
		{
			in:  `a: close({b?: 1}), b: close({b: 1})`,
			err: "",
		},
		{
			in:  `a: close({b: 1}), b: close({b?: 1})`,
			err: `field b not present in {b?:1} (and 1 more errors)`,
		},
		{
			in:  `a: {}, b: close({})`,
			err: "",
		},
		{
			in:  `a: {}, b: close({foo?: 1})`,
			err: "",
		},
		{
			in:  `a: {foo?:1}, b: close({})`,
			err: "",
		},

		// New in new evaluator.
		{
			in:  `a: close({foo?:1}), b: close({bar?: 1})`,
			err: "field not allowed in closed struct: bar (and 1 more errors)",
		},
		{
			in:  `a: {foo?:1}, b: close({bar?: 1})`,
			err: "",
		},
		{
			in:  `a: {foo?:1}, b: close({bar: 1})`,
			err: "",
		},

		// Definitions are not regular fields.
		{
			in:  `a: {#a: 1}, b: {a: 1}`,
			err: "regular field is constraint in subsumed value: #a (and 1 more errors)",
		},
		{
			in:  `a: {a: 1}, b: {#a: 1}`,
			err: "regular field is constraint in subsumed value: a (and 1 more errors)",
		},

		// Subsuming final values.
		{
			in:   `a: [string]: 1, b: {foo: 1}`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: [string]: int, b: {foo: 1}`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: {["foo"]: int}, b: {foo: 1}`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: close({["foo"]: 1}), b: {bar: 1}`,
			mode: subFinal,
			err:  "field not allowed in closed struct: bar (and 1 more errors)",
		},
		{
			in:   `a: {foo: 1}, b: {foo?: 1}`,
			mode: subFinal,
			err:  `field foo not present in {foo?:1} (and 1 more errors)`,
		},
		{
			in:   `a: close({}), b: {foo?: 1}`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: close({}), b: close({foo?: 1})`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: {}, b: close({})`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: {[string]: 1}, b: {foo: 2}`,
			mode: subFinal,
			err:  "value not an instance",
		},
		{
			in:   `a: {}, b: close({foo?: 1})`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: {foo: [...string]}, b: {}`,
			mode: subFinal,
			err:  "regular field is constraint in subsumed value: foo (and 1 more errors)",
		},

		// Schema values
		{
			in:   `a: close({}), b: {foo: 1}`,
			mode: subSchema,
			err:  "",
		},
		// TODO(eval): FIX
		// 801: {subsumes: true, in: `a: {[string]: int}, b: {foo: 1}`, mode: subSchema},
		{
			in:   `a: {foo: 1}, b: {foo?: 1}`,
			mode: subSchema,
			err:  `field foo not present in {foo?:1} (and 1 more errors)`,
		},
		{
			in:   `a: close({}), b: {foo?: 1}`,
			mode: subSchema,
			err:  "",
		},
		{
			in:   `a: close({}), b: close({foo?: 1})`,
			mode: subSchema,
			err:  "",
		},
		{
			in:   `a: {}, b: close({})`,
			mode: subSchema,
			err:  "",
		},
		{
			in:   `a: {[string]: 1}, b: {foo: 2}`,
			mode: subSchema,
			err:  "value not an instance",
		},
		{
			in:   `a: {}, b: close({foo?: 1})`,
			mode: subSchema,
			err:  "",
		},

		// Lists
		{
			in:  `a: [], b: []`,
			err: "",
		},
		{
			in:  `a: [...], b: []`,
			err: "",
		},
		{
			in:  `a: [...], b: [...]`,
			err: "",
		},
		{
			in:  `a: [], b: [...]`,
			err: "value not an instance",
		},

		{
			in:  `a: [2], b: [2]`,
			err: "",
		},
		{
			in:  `a: [int], b: [2]`,
			err: "",
		},
		{
			in:  `a: [2], b: [int]`,
			err: "value not an instance",
		},
		{
			in:  `a: [int], b: [int]`,
			err: "",
		},

		{
			in:  `a: [...2], b: [2]`,
			err: "",
		},
		{
			in:  `a: [...int], b: [2]`,
			err: "",
		},
		{
			in:  `a: [...2], b: [int]`,
			err: "value not an instance",
		},
		{
			in:  `a: [...int], b: [int]`,
			err: "",
		},

		{
			in:  `a: [2], b: [...2]`,
			err: "value not an instance",
		},
		{
			in:  `a: [int], b: [...2]`,
			err: "value not an instance",
		},
		{
			in:  `a: [2], b: [...int]`,
			err: "value not an instance",
		},
		{
			in:  `a: [int], b: [...int]`,
			err: "value not an instance",
		},

		{
			in:  `a: [...int], b: ["foo"]`,
			err: "value not an instance",
		},
		{
			in:  `a: ["foo"], b: [...int]`,
			err: "value not an instance",
		},

		// Defaults:
		// TODO: for the purpose of v0.2 compatibility these
		// evaluate to true. Reconsider before making this package
		// public.
		{
			in:   `a: [], b: [...int]`,
			mode: subDefaults,
			err:  "",
		},
		{
			in:   `a: [2], b: [2, ...int]`,
			mode: subDefaults,
			err:  "",
		},

		// Final
		{
			in:   `a: [], b: [...int]`,
			mode: subFinal,
			err:  "",
		},
		{
			in:   `a: [2], b: [2, ...int]`,
			mode: subFinal,
			err:  "",
		},
	}

	re := regexp.MustCompile(`a: (.*).*b: ([^\n]*)`)

	cuetdtest.Run(t, testCases, func(t *cuetdtest.T, tc *subsumeTest) {
		t.Update(cuetest.UpdateGoldenFiles)
		t.M.TODO_V2(t)

		if tc.in == "" {
			t.Skip("empty test case")
		}

		m := re.FindStringSubmatch(strings.Join(strings.Split(tc.in, "\n"), ""))
		const cutset = "\n ,"
		key := strings.Trim(m[1], cutset) + " ⊑ " + strings.Trim(m[2], cutset)
		// Log descriptive name for debugging
		t.Log(key)

		if tc.skip_v2 && t.M.IsDefault() {
			t.Skipf("skipping v2 test")
		}

		r := t.M.Runtime()

		file, err := parser.ParseFile("subsume", tc.in)
		if err != nil {
			t.Fatal(err)
		}

		root, errs := compile.Files(nil, r, "", file)
		if errs != nil {
			t.Fatal(errs)
		}

		ctx := eval.NewContext(r, root)
		root.Finalize(ctx)

		// Use low-level lookup to avoid evaluation.
		var a, b adt.Value
		for _, arc := range root.Arcs {
			switch arc.Label {
			case ctx.StringLabel("a"):
				a = arc
			case ctx.StringLabel("b"):
				b = arc
			}
		}

		switch tc.mode {
		case subNone:
			err = subsume.Value(ctx, a, b)
		case subSchema:
			err = subsume.API.Value(ctx, a, b)
		// TODO: see comments above.
		// case subNoOptional:
		// 	err = IgnoreOptional.Value(ctx, a, b)
		case subDefaults:
			p := subsume.Profile{Defaults: true}
			err = p.Value(ctx, a, b)
		case subFinal:
			err = subsume.Final.Value(ctx, a, b)
		}

		var gotErr string
		if err != nil {
			gotErr = err.Error()
		}

		t.Equal(gotErr, tc.err)
	})
}
