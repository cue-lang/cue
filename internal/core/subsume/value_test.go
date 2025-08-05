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
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/subsume"
	"cuelang.org/go/internal/cuetdtest"
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
		subsumes bool
		in       string
		mode     int

		skip_v2 bool // Bug only exists in v2. Won't fix.
	}
	testCases := []subsumeTest{
		// Top subsumes everything
		0: {
			in:       `a: _, b: _ `,
			subsumes: true,
		},
		1: {
			in:       `a: _, b: null `,
			subsumes: true,
		},
		2: {
			in:       `a: _, b: int `,
			subsumes: true,
		},
		3: {
			in:       `a: _, b: 1 `,
			subsumes: true,
		},
		4: {
			in:       `a: _, b: float `,
			subsumes: true,
		},
		5: {
			in:       `a: _, b: "s" `,
			subsumes: true,
		},
		6: {
			in:       `a: _, b: {} `,
			subsumes: true,
		},
		7: {
			in:       `a: _, b: []`,
			subsumes: true,
		},
		8: {
			in:       `a: _, b: _|_ `,
			subsumes: true,
		},

		// Nothing besides top subsumed top
		9: {
			in:       `a: null,    b: _`,
			subsumes: false,
		},
		10: {
			in:       `a: int, b: _`,
			subsumes: false,
		},
		11: {
			in:       `a: 1,       b: _`,
			subsumes: false,
		},
		12: {
			in:       `a: float, b: _`,
			subsumes: false,
		},
		13: {
			in:       `a: "s",     b: _`,
			subsumes: false,
		},
		14: {
			in:       `a: {},      b: _`,
			subsumes: false,
		},
		15: {
			in:       `a: [],      b: _`,
			subsumes: false,
		},
		16: {
			in:       `a: _|_ ,      b: _`,
			subsumes: false,
		},

		// Bottom subsumes nothing except bottom itself.
		17: {
			in:       `a: _|_, b: null `,
			subsumes: false,
		},
		18: {
			in:       `a: _|_, b: int `,
			subsumes: false,
		},
		19: {
			in:       `a: _|_, b: 1 `,
			subsumes: false,
		},
		20: {
			in:       `a: _|_, b: float `,
			subsumes: false,
		},
		21: {
			in:       `a: _|_, b: "s" `,
			subsumes: false,
		},
		22: {
			in:       `a: _|_, b: {} `,
			subsumes: false,
		},
		23: {
			in:       `a: _|_, b: [] `,
			subsumes: false,
		},
		24: {
			in:       ` a: _|_, b: _|_ `,
			subsumes: true,
		},

		// All values subsume bottom
		25: {
			in:       `a: null,    b: _|_`,
			subsumes: true,
		},
		26: {
			in:       `a: int, b: _|_`,
			subsumes: true,
		},
		27: {
			in:       `a: 1,       b: _|_`,
			subsumes: true,
		},
		28: {
			in:       `a: float, b: _|_`,
			subsumes: true,
		},
		29: {
			in:       `a: "s",     b: _|_`,
			subsumes: true,
		},
		30: {
			in:       `a: {},      b: _|_`,
			subsumes: true,
		},
		31: {
			in:       `a: [],      b: _|_`,
			subsumes: true,
		},
		32: {
			in:       `a: true,    b: _|_`,
			subsumes: true,
		},
		33: {
			in:       `a: _|_,       b: _|_`,
			subsumes: true,
		},

		// null subsumes only null
		34: {
			in:       ` a: null, b: null `,
			subsumes: true,
		},
		35: {
			in:       `a: null, b: 1 `,
			subsumes: false,
		},
		36: {
			in:       `a: 1,    b: null `,
			subsumes: false,
		},

		37: {
			in:       ` a: true, b: true `,
			subsumes: true,
		},
		38: {
			in:       `a: true, b: false `,
			subsumes: false,
		},

		39: {
			in:       ` a: "a",    b: "a" `,
			subsumes: true,
		},
		40: {
			in:       `a: "a",    b: "b" `,
			subsumes: false,
		},
		41: {
			in:       ` a: string, b: "a" `,
			subsumes: true,
		},
		42: {
			in:       `a: "a",    b: string `,
			subsumes: false,
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
		43: {
			in:       `a: 1, b: 1 `,
			subsumes: true,
		},
		44: {
			in:       `a: 1.0, b: 1.0 `,
			subsumes: true,
		},
		45: {
			in:       `a: 3.0, b: 3.0 `,
			subsumes: true,
		},
		46: {
			in:       `a: 1.0, b: 1 `,
			subsumes: false,
		},
		47: {
			in:       `a: 1, b: 1.0 `,
			subsumes: false,
		},
		48: {
			in:       `a: 3, b: 3.0`,
			subsumes: false,
		},
		49: {
			in:       `a: int, b: 1`,
			subsumes: true,
		},
		50: {
			in:       `a: int, b: int & 1`,
			subsumes: true,
		},
		51: {
			in:       `a: float, b: 1.0`,
			subsumes: true,
		},
		52: {
			in:       `a: float, b: 1`,
			subsumes: false,
		},
		53: {
			in:       `a: int, b: 1.0`,
			subsumes: false,
		},
		54: {
			in:       `a: int, b: int`,
			subsumes: true,
		},
		55: {
			in:       `a: number, b: int`,
			subsumes: true,
		},

		// Structs
		64: {
			in:       `a: {}, b: {}`,
			subsumes: true,
		},
		65: {
			in:       `a: {}, b: {a: 1}`,
			subsumes: true,
		},
		66: {
			in:       `a: {a:1}, b: {a:1, b:1}`,
			subsumes: true,
		},
		67: {
			in:       `a: {s: { a:1} }, b: { s: { a:1, b:2 }}`,
			subsumes: true,
		},
		68: {
			in:       `a: {}, b: {}`,
			subsumes: true,
		},
		// TODO: allow subsumption of unevaluated values?
		// ref not yet evaluated and not structurally equivalent
		69: {
			in:       `a: {}, b: {} & c, c: {}`,
			subsumes: true,
		},

		70: {
			in:       `a: {a:1}, b: {}`,
			subsumes: false,
		},
		71: {
			in:       `a: {a:1, b:1}, b: {a:1}`,
			subsumes: false,
		},
		72: {
			in:       `a: {s: { a:1} }, b: { s: {}}`,
			subsumes: false,
		},

		84: {
			in:       `a: 1 | 2, b: 2 | 1`,
			subsumes: true,
		},
		85: {
			in:       `a: 1 | 2, b: 1 | 2`,
			subsumes: true,
		},

		86: {
			in:       `a: number, b: 2 | 1`,
			subsumes: true,
		},
		87: {
			in:       `a: number, b: 2 | 1`,
			subsumes: true,
		},
		88: {
			in:       `a: int, b: 1 | 2 | 3.1`,
			subsumes: false,
		},

		89: {
			in:       `a: float | number, b: 1 | 2 | 3.1`,
			subsumes: true,
		},

		90: {
			in:       `a: int, b: 1 | 2 | 3.1`,
			subsumes: false,
		},
		91: {
			in:       `a: 1 | 2, b: 1`,
			subsumes: true,
		},
		92: {
			in:       `a: 1 | 2, b: 2`,
			subsumes: true,
		},
		93: {
			in:       `a: 1 | 2, b: 3`,
			subsumes: false,
		},

		// 147: {subsumes: true, in: ` a: 7080, b: *7080 | int`, mode: subChoose},

		// Defaults
		150: {
			in:       `a: number | *1, b: number | *2`,
			subsumes: false,
		},
		151: {
			in:       `a: number | *2, b: number | *2`,
			subsumes: true,
		},
		152: {
			in:       `a: int | *float, b: int | *2.0`,
			subsumes: true,
		},
		153: {
			in:       `a: int | *2, b: int | *2.0`,
			subsumes: false,
		},
		154: {
			in:       `a: number | *2 | *3, b: number | *2`,
			subsumes: true,
		},
		155: {
			in:       `a: number, b: number | *2`,
			subsumes: true,
		},

		// Bounds
		170: {
			in:       `a: >=2, b: >=2`,
			subsumes: true,
		},
		171: {
			in:       `a: >=1, b: >=2`,
			subsumes: true,
		},
		172: {
			in:       `a: >0, b: >=2`,
			subsumes: true,
		},
		173: {
			in:       `a: >1, b: >1`,
			subsumes: true,
		},
		174: {
			in:       `a: >=1, b: >1`,
			subsumes: true,
		},
		175: {
			in:       `a: >1, b: >=1`,
			subsumes: false,
		},
		176: {
			in:       `a: >=1, b: >=1`,
			subsumes: true,
		},
		177: {
			in:       `a: <1, b: <1`,
			subsumes: true,
		},
		178: {
			in:       `a: <=1, b: <1`,
			subsumes: true,
		},
		179: {
			in:       `a: <1, b: <=1`,
			subsumes: false,
		},
		180: {
			in:       `a: <=1, b: <=1`,
			subsumes: true,
		},

		181: {
			in:       `a: !=1, b: !=1`,
			subsumes: true,
		},
		182: {
			in:       `a: !=1, b: !=2`,
			subsumes: false,
		},

		183: {
			in:       `a: !=1, b: <=1`,
			subsumes: false,
		},
		184: {
			in:       `a: !=1, b: <1`,
			subsumes: true,
		},
		185: {
			in:       `a: !=1, b: >=1`,
			subsumes: false,
		},
		186: {
			in:       `a: !=1, b: <1`,
			subsumes: true,
		},

		187: {
			in:       `a: !=1, b: <=0`,
			subsumes: true,
		},
		188: {
			in:       `a: !=1, b: >=2`,
			subsumes: true,
		},
		189: {
			in:       `a: !=1, b: >1`,
			subsumes: true,
		},

		195: {
			in:       `a: >=2, b: !=2`,
			subsumes: false,
		},
		196: {
			in:       `a: >2, b: !=2`,
			subsumes: false,
		},
		197: {
			in:       `a: <2, b: !=2`,
			subsumes: false,
		},
		198: {
			in:       `a: <=2, b: !=2`,
			subsumes: false,
		},

		200: {
			in:       `a: =~"foo", b: =~"foo"`,
			subsumes: true,
		},
		201: {
			in:       `a: =~"foo", b: =~"bar"`,
			subsumes: false,
		},
		202: {
			in:       `a: =~"foo1", b: =~"foo"`,
			subsumes: false,
		},

		203: {
			in:       `a: !~"foo", b: !~"foo"`,
			subsumes: true,
		},
		204: {
			in:       `a: !~"foo", b: !~"bar"`,
			subsumes: false,
		},
		205: {
			in:       `a: !~"foo", b: !~"foo1"`,
			subsumes: false,
		},

		// The following is could be true, but we will not go down the rabbit
		// hold of trying to prove subsumption of regular expressions.
		210: {
			in:       `a: =~"foo", b: =~"foo1"`,
			subsumes: false,
		},
		211: {
			in:       `a: !~"foo1", b: !~"foo"`,
			subsumes: false,
		},

		220: {
			in:       `a: <5, b: 4`,
			subsumes: true,
		},
		221: {
			in:       `a: <5, b: 5`,
			subsumes: false,
		},
		222: {
			in:       `a: <=5, b: 5`,
			subsumes: true,
		},
		223: {
			in:       `a: <=5.0, b: 5.00000001`,
			subsumes: false,
		},
		224: {
			in:       `a: >5, b: 6`,
			subsumes: true,
		},
		225: {
			in:       `a: >5, b: 5`,
			subsumes: false,
		},
		226: {
			in:       `a: >=5, b: 5`,
			subsumes: true,
		},
		227: {
			in:       `a: >=5, b: 4`,
			subsumes: false,
		},
		228: {
			in:       `a: !=5, b: 6`,
			subsumes: true,
		},
		229: {
			in:       `a: !=5, b: 5`,
			subsumes: false,
		},
		230: {
			in:       `a: !=5.0, b: 5.0`,
			subsumes: false,
		},
		231: {
			in:       `a: !=5.0, b: 5`,
			subsumes: false,
		},

		250: {
			in:       `a: =~ #"^\d{3}$"#, b: "123"`,
			subsumes: true,
		},
		251: {
			in:       `a: =~ #"^\d{3}$"#, b: "1234"`,
			subsumes: false,
		},
		252: {
			in:       `a: !~ #"^\d{3}$"#, b: "1234"`,
			subsumes: true,
		},
		253: {
			in:       `a: !~ #"^\d{3}$"#, b: "123"`,
			subsumes: false,
		},

		// Conjunctions
		300: {
			in:       `a: >0, b: >=2 & <=100`,
			subsumes: true,
		},
		301: {
			in:       `a: >0, b: >=0 & <=100`,
			subsumes: false,
		},

		310: {
			in:       `a: >=0 & <=100, b: 10`,
			subsumes: true,
		},
		311: {
			in:       `a: >=0 & <=100, b: >=0 & <=100`,
			subsumes: true,
		},
		312: {
			in:       `a: !=2 & !=4, b: >3`,
			subsumes: false,
		},
		313: {
			in:       `a: !=2 & !=4, b: >5`,
			subsumes: true,
		},

		314: {
			in:       `a: >=0 & <=100, b: >=0 & <=150`,
			subsumes: false,
		},
		315: {
			in:       `a: >=0 & <=150, b: >=0 & <=100`,
			subsumes: true,
		},

		// Disjunctions
		330: {
			in:       `a: >5, b: >10 | 8`,
			subsumes: true,
		},
		331: {
			in:       `a: >8, b: >10 | 8`,
			subsumes: false,
		},

		// Optional fields
		// Optional fields defined constraints on fields that are not yet
		// defined. So even if such a field is not part of the output, it
		// influences the lattice structure.
		// For a given A and B, where A and B unify and where A has an optional
		// field that is not defined in B, the addition of an incompatible
		// value of that field in B can cause A and B to no longer unify.
		//
		400: {
			in:       `a: {foo: 1}, b: {}`,
			subsumes: false,
		},
		401: {
			in:       `a: {foo?: 1}, b: {}`,
			subsumes: false,
		},
		402: {
			in:       `a: {}, b: {foo: 1}`,
			subsumes: true,
		},
		403: {
			in:       `a: {}, b: {foo?: 1}`,
			subsumes: true,
		},

		404: {
			in:       `a: {foo: 1}, b: {foo: 1}`,
			subsumes: true,
		},
		405: {
			in:       `a: {foo?: 1}, b: {foo: 1}`,
			subsumes: true,
		},
		406: {
			in:       `a: {foo?: 1}, b: {foo?: 1}`,
			subsumes: true,
		},
		407: {
			in:       `a: {foo: 1}, b: {foo?: 1}`,
			subsumes: false,
		},

		408: {
			in:       `a: {foo: 1}, b: {foo: 2}`,
			subsumes: false,
		},
		409: {
			in:       `a: {foo?: 1}, b: {foo: 2}`,
			subsumes: false,
		},
		410: {
			in:       `a: {foo?: 1}, b: {foo?: 2}`,
			subsumes: false,
		},
		411: {
			in:       `a: {foo: 1}, b: {foo?: 2}`,
			subsumes: false,
		},

		412: {
			in:       `a: {foo: number}, b: {foo: 2}`,
			subsumes: true,
		},
		413: {
			in:       `a: {foo?: number}, b: {foo: 2}`,
			subsumes: true,
		},
		414: {
			in:       `a: {foo?: number}, b: {foo?: 2}`,
			subsumes: true,
		},
		415: {
			in:       `a: {foo: number}, b: {foo?: 2}`,
			subsumes: false,
		},

		416: {
			in:       `a: {foo: 1}, b: {foo: number}`,
			subsumes: false,
		},
		417: {
			in:       `a: {foo?: 1}, b: {foo: number}`,
			subsumes: false,
		},
		418: {
			in:       `a: {foo?: 1}, b: {foo?: number}`,
			subsumes: false,
		},
		419: {
			in:       `a: {foo: 1}, b: {foo?: number}`,
			subsumes: false,
		},

		// The one exception of the rule: there is no value of foo that can be
		// added to b which would cause the unification of a and b to fail.
		// So an optional field with a value of top is equivalent to not
		// defining one at all.
		420: {
			in:       `a: {foo?: _}, b: {}`,
			subsumes: true,
		},

		430: {
			in:       `a: {[_]: 4}, b: {[_]: int}`,
			subsumes: false,
		},
		431: {
			in:       `a: {[_]: int}, b: {[_]: 2}`,
			skip_v2:  true,
			subsumes: true,
		},
		432: {
			in:       `a: {[string]: int, [<"m"]: 3}, b: {[string]: 2, [<"m"]: 3}`,
			skip_v2:  true,
			subsumes: true,
		},
		433: {
			in:       `a: {[<"m"]: 3, [string]: int}, b: {[string]: 2, [<"m"]: 3}`,
			skip_v2:  true,
			subsumes: true,
		},
		434: {
			in:       `a: {[<"n"]: 3, [string]: int}, b: {[string]: 2, [<"m"]: 3}`,
			subsumes: false,
		},
		435: {
			// both sides unify to a single string pattern.
			in:       `a: {[string]: <5, [string]: int}, b: {[string]: <=3, [string]: 3}`,
			skip_v2:  true,
			subsumes: true,
		},
		436: {
			// matches because bottom is subsumed by >5
			in:       `a: {[string]: >5}, b: {[string]: 1, [string]: 2}`,
			skip_v2:  true,
			subsumes: true,
		},
		437: {
			// subsumption gives up if a has more pattern constraints than b.
			// TODO: support this?
			in:       `a: {[_]: >5, [>"b"]: int}, b: {[_]: 6}`,
			subsumes: false,
		},
		438: {
			in:       `a: {}, b: {[_]: 6}`,
			subsumes: true,
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
		460: {
			in:       `a: {1, #foo: number}, b: {1, #foo: 1}`,
			subsumes: true,
		},
		461: {
			in:       `a: {1, #foo?: number}, b: {1, #foo: 1}`,
			subsumes: true,
		},
		462: {
			in:       `a: {1, #foo?: number}, b: {1, #foo?: 1}`,
			subsumes: true,
		},
		463: {
			in:       `a: {1, #foo: number}, b: {1, #foo?: 1}`,
			subsumes: false,
		},

		464: {
			in:       `a: {int, #foo: number}, b: {1, #foo: 1}`,
			subsumes: true,
		},
		465: {
			in:       `a: {int, #foo: 1}, b: {1, #foo: number}`,
			subsumes: false,
		},
		466: {
			in:       `a: {1, #foo: number}, b: {int, #foo: 1}`,
			subsumes: false,
		},
		467: {
			in:       `a: {1, #foo: 1}, b: {int, #foo: number}`,
			subsumes: false,
		},

		// Lists
		506: {
			in:       `a: [], b: [] `,
			subsumes: true,
		},
		507: {
			in:       `a: [1], b: [1] `,
			subsumes: true,
		},
		508: {
			in:       `a: [1], b: [2] `,
			subsumes: false,
		},
		509: {
			in:       `a: [1], b: [2, 3] `,
			subsumes: false,
		},
		510: {
			in:       `a: [{b: string}], b: [{b: "foo"}] `,
			subsumes: true,
		},
		511: {
			in:       `a: [...{b: string}], b: [{b: "foo"}] `,
			subsumes: true,
		},
		512: {
			in:       `a: [{b: "foo"}], b: [{b: string}] `,
			subsumes: false,
		},
		513: {
			in:       `a: [{b: string}], b: [{b: "foo"}, ...{b: "foo"}] `,
			subsumes: false,
		},
		520: {
			in:       `a: [_, int, ...], b: [int, string, ...string] `,
			subsumes: false,
		},

		// Closed structs.
		600: {
			in:       `a: close({}), b: {a: 1}`,
			subsumes: false,
		},
		601: {
			in:       `a: close({a: 1}), b: {a: 1}`,
			subsumes: false,
		},
		602: {
			in:       `a: close({a: 1, b: 1}), b: {a: 1}`,
			subsumes: false,
		},
		603: {
			in:       `a: {a: 1}, b: close({})`,
			subsumes: false,
		},
		604: {
			in:       `a: {a: 1}, b: close({a: 1})`,
			subsumes: true,
		},
		605: {
			in:       `a: {a: 1}, b: close({a: 1, b: 1})`,
			subsumes: true,
		},
		606: {
			in:       `a: close({b?: 1}), b: close({b: 1})`,
			subsumes: true,
		},
		607: {
			in:       `a: close({b: 1}), b: close({b?: 1})`,
			subsumes: false,
		},
		608: {
			in:       `a: {}, b: close({})`,
			subsumes: true,
		},
		609: {
			in:       `a: {}, b: close({foo?: 1})`,
			subsumes: true,
		},
		610: {
			in:       `a: {foo?:1}, b: close({})`,
			subsumes: true,
		},

		// New in new evaluator.
		611: {
			in:       `a: close({foo?:1}), b: close({bar?: 1})`,
			subsumes: false,
		},
		612: {
			in:       `a: {foo?:1}, b: close({bar?: 1})`,
			subsumes: true,
		},
		613: {
			in:       `a: {foo?:1}, b: close({bar: 1})`,
			subsumes: true,
		},

		// Definitions are not regular fields.
		630: {
			in:       `a: {#a: 1}, b: {a: 1}`,
			subsumes: false,
		},
		631: {
			in:       `a: {a: 1}, b: {#a: 1}`,
			subsumes: false,
		},

		// Subsuming final values.
		700: {
			in:       `a: [string]: 1, b: {foo: 1}`,
			mode:     subFinal,
			subsumes: true,
		},
		701: {
			in:       `a: [string]: int, b: {foo: 1}`,
			mode:     subFinal,
			subsumes: true,
		},
		702: {
			in:       `a: {["foo"]: int}, b: {foo: 1}`,
			mode:     subFinal,
			subsumes: true,
		},
		703: {
			in:       `a: close({["foo"]: 1}), b: {bar: 1}`,
			mode:     subFinal,
			subsumes: false,
		},
		704: {
			in:       `a: {foo: 1}, b: {foo?: 1}`,
			mode:     subFinal,
			subsumes: false,
		},
		705: {
			in:       `a: close({}), b: {foo?: 1}`,
			mode:     subFinal,
			subsumes: true,
		},
		706: {
			in:       `a: close({}), b: close({foo?: 1})`,
			mode:     subFinal,
			subsumes: true,
		},
		707: {
			in:       `a: {}, b: close({})`,
			mode:     subFinal,
			subsumes: true,
		},
		708: {
			in:       `a: {[string]: 1}, b: {foo: 2}`,
			mode:     subFinal,
			subsumes: false,
		},
		709: {
			in:       `a: {}, b: close({foo?: 1})`,
			mode:     subFinal,
			subsumes: true,
		},
		710: {
			in:       `a: {foo: [...string]}, b: {}`,
			mode:     subFinal,
			subsumes: false,
		},

		// Schema values
		800: {
			in:       `a: close({}), b: {foo: 1}`,
			mode:     subSchema,
			subsumes: true,
		},
		// TODO(eval): FIX
		// 801: {subsumes: true, in: `a: {[string]: int}, b: {foo: 1}`, mode: subSchema},
		804: {
			in:       `a: {foo: 1}, b: {foo?: 1}`,
			mode:     subSchema,
			subsumes: false,
		},
		805: {
			in:       `a: close({}), b: {foo?: 1}`,
			mode:     subSchema,
			subsumes: true,
		},
		806: {
			in:       `a: close({}), b: close({foo?: 1})`,
			mode:     subSchema,
			subsumes: true,
		},
		807: {
			in:       `a: {}, b: close({})`,
			mode:     subSchema,
			subsumes: true,
		},
		808: {
			in:       `a: {[string]: 1}, b: {foo: 2}`,
			mode:     subSchema,
			subsumes: false,
		},
		809: {
			in:       `a: {}, b: close({foo?: 1})`,
			mode:     subSchema,
			subsumes: true,
		},

		// Lists
		950: {
			in:       `a: [], b: []`,
			subsumes: true,
		},
		951: {
			in:       `a: [...], b: []`,
			subsumes: true,
		},
		952: {
			in:       `a: [...], b: [...]`,
			subsumes: true,
		},
		953: {
			in:       `a: [], b: [...]`,
			subsumes: false,
		},

		954: {
			in:       `a: [2], b: [2]`,
			subsumes: true,
		},
		955: {
			in:       `a: [int], b: [2]`,
			subsumes: true,
		},
		956: {
			in:       `a: [2], b: [int]`,
			subsumes: false,
		},
		957: {
			in:       `a: [int], b: [int]`,
			subsumes: true,
		},

		958: {
			in:       `a: [...2], b: [2]`,
			subsumes: true,
		},
		959: {
			in:       `a: [...int], b: [2]`,
			subsumes: true,
		},
		960: {
			in:       `a: [...2], b: [int]`,
			subsumes: false,
		},
		961: {
			in:       `a: [...int], b: [int]`,
			subsumes: true,
		},

		962: {
			in:       `a: [2], b: [...2]`,
			subsumes: false,
		},
		963: {
			in:       `a: [int], b: [...2]`,
			subsumes: false,
		},
		964: {
			in:       `a: [2], b: [...int]`,
			subsumes: false,
		},
		965: {
			in:       `a: [int], b: [...int]`,
			subsumes: false,
		},

		966: {
			in:       `a: [...int], b: ["foo"]`,
			subsumes: false,
		},
		967: {
			in:       `a: ["foo"], b: [...int]`,
			subsumes: false,
		},

		// Defaults:
		// TODO: for the purpose of v0.2 compatibility these
		// evaluate to true. Reconsider before making this package
		// public.
		970: {
			in:       `a: [], b: [...int]`,
			mode:     subDefaults,
			subsumes: true,
		},
		971: {
			in:       `a: [2], b: [2, ...int]`,
			mode:     subDefaults,
			subsumes: true,
		},

		// Final
		980: {
			in:       `a: [], b: [...int]`,
			mode:     subFinal,
			subsumes: true,
		},
		981: {
			in:       `a: [2], b: [2, ...int]`,
			mode:     subFinal,
			subsumes: true,
		},
	}

	re := regexp.MustCompile(`a: (.*).*b: ([^\n]*)`)
	for i, tc := range testCases {
		if tc.in == "" {
			continue
		}
		m := re.FindStringSubmatch(strings.Join(strings.Split(tc.in, "\n"), ""))
		const cutset = "\n ,"
		key := strings.Trim(m[1], cutset) + " ⊑ " + strings.Trim(m[2], cutset)

		cuetdtest.FullMatrix.Run(t, strconv.Itoa(i)+"/"+key, func(t *testing.T, m *cuetdtest.M) {
			if tc.skip_v2 && m.IsDefault() {
				t.Skipf("skipping v2 test")
			}
			r := m.Runtime()

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
			got := err == nil

			if got != tc.subsumes {
				t.Errorf("got %v; want %v (%v vs %v)", got, tc.subsumes, a.Kind(), b.Kind())
			}
		})
	}
}
