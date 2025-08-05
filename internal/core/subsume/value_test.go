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
		err  string
		in   string
		mode int

		skip_v2 bool // Bug only exists in v2. Won't fix.
	}
	testCases := []subsumeTest{
		// Top subsumes everything
		0: {
			in:  `a: _, b: _ `,
			err: "",
		},
		1: {
			in:  `a: _, b: null `,
			err: "",
		},
		2: {
			in:  `a: _, b: int `,
			err: "",
		},
		3: {
			in:  `a: _, b: 1 `,
			err: "",
		},
		4: {
			in:  `a: _, b: float `,
			err: "",
		},
		5: {
			in:  `a: _, b: "s" `,
			err: "",
		},
		6: {
			in:  `a: _, b: {} `,
			err: "",
		},
		7: {
			in:  `a: _, b: []`,
			err: "",
		},
		8: {
			in:  `a: _, b: _|_ `,
			err: "",
		},

		// Nothing besides top subsumed top
		9: {
			in:  `a: null,    b: _`,
			err: "subsumption failed",
		},
		10: {
			in:  `a: int, b: _`,
			err: "subsumption failed",
		},
		11: {
			in:  `a: 1,       b: _`,
			err: "subsumption failed",
		},
		12: {
			in:  `a: float, b: _`,
			err: "subsumption failed",
		},
		13: {
			in:  `a: "s",     b: _`,
			err: "subsumption failed",
		},
		14: {
			in:  `a: {},      b: _`,
			err: "subsumption failed",
		},
		15: {
			in:  `a: [],      b: _`,
			err: "subsumption failed",
		},
		16: {
			in:  `a: _|_ ,      b: _`,
			err: "subsumption failed",
		},

		// Bottom subsumes nothing except bottom itself.
		17: {
			in:  `a: _|_, b: null `,
			err: "subsumption failed",
		},
		18: {
			in:  `a: _|_, b: int `,
			err: "subsumption failed",
		},
		19: {
			in:  `a: _|_, b: 1 `,
			err: "subsumption failed",
		},
		20: {
			in:  `a: _|_, b: float `,
			err: "subsumption failed",
		},
		21: {
			in:  `a: _|_, b: "s" `,
			err: "subsumption failed",
		},
		22: {
			in:  `a: _|_, b: {} `,
			err: "subsumption failed",
		},
		23: {
			in:  `a: _|_, b: [] `,
			err: "subsumption failed",
		},
		24: {
			in:  ` a: _|_, b: _|_ `,
			err: "",
		},

		// All values subsume bottom
		25: {
			in:  `a: null,    b: _|_`,
			err: "",
		},
		26: {
			in:  `a: int, b: _|_`,
			err: "",
		},
		27: {
			in:  `a: 1,       b: _|_`,
			err: "",
		},
		28: {
			in:  `a: float, b: _|_`,
			err: "",
		},
		29: {
			in:  `a: "s",     b: _|_`,
			err: "",
		},
		30: {
			in:  `a: {},      b: _|_`,
			err: "",
		},
		31: {
			in:  `a: [],      b: _|_`,
			err: "",
		},
		32: {
			in:  `a: true,    b: _|_`,
			err: "",
		},
		33: {
			in:  `a: _|_,       b: _|_`,
			err: "",
		},

		// null subsumes only null
		34: {
			in:  ` a: null, b: null `,
			err: "",
		},
		35: {
			in:  `a: null, b: 1 `,
			err: "subsumption failed",
		},
		36: {
			in:  `a: 1,    b: null `,
			err: "subsumption failed",
		},

		37: {
			in:  ` a: true, b: true `,
			err: "",
		},
		38: {
			in:  `a: true, b: false `,
			err: "subsumption failed",
		},

		39: {
			in:  ` a: "a",    b: "a" `,
			err: "",
		},
		40: {
			in:  `a: "a",    b: "b" `,
			err: "subsumption failed",
		},
		41: {
			in:  ` a: string, b: "a" `,
			err: "",
		},
		42: {
			in:  `a: "a",    b: string `,
			err: "subsumption failed",
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
			in:  `a: 1, b: 1 `,
			err: "",
		},
		44: {
			in:  `a: 1.0, b: 1.0 `,
			err: "",
		},
		45: {
			in:  `a: 3.0, b: 3.0 `,
			err: "",
		},
		46: {
			in:  `a: 1.0, b: 1 `,
			err: "subsumption failed",
		},
		47: {
			in:  `a: 1, b: 1.0 `,
			err: "subsumption failed",
		},
		48: {
			in:  `a: 3, b: 3.0`,
			err: "subsumption failed",
		},
		49: {
			in:  `a: int, b: 1`,
			err: "",
		},
		50: {
			in:  `a: int, b: int & 1`,
			err: "",
		},
		51: {
			in:  `a: float, b: 1.0`,
			err: "",
		},
		52: {
			in:  `a: float, b: 1`,
			err: "subsumption failed",
		},
		53: {
			in:  `a: int, b: 1.0`,
			err: "subsumption failed",
		},
		54: {
			in:  `a: int, b: int`,
			err: "",
		},
		55: {
			in:  `a: number, b: int`,
			err: "",
		},

		// Structs
		64: {
			in:  `a: {}, b: {}`,
			err: "",
		},
		65: {
			in:  `a: {}, b: {a: 1}`,
			err: "",
		},
		66: {
			in:  `a: {a:1}, b: {a:1, b:1}`,
			err: "",
		},
		67: {
			in:  `a: {s: { a:1} }, b: { s: { a:1, b:2 }}`,
			err: "",
		},
		68: {
			in:  `a: {}, b: {}`,
			err: "",
		},
		// TODO: allow subsumption of unevaluated values?
		// ref not yet evaluated and not structurally equivalent
		69: {
			in:  `a: {}, b: {} & c, c: {}`,
			err: "",
		},

		70: {
			in:  `a: {a:1}, b: {}`,
			err: "subsumption failed",
		},
		71: {
			in:  `a: {a:1, b:1}, b: {a:1}`,
			err: "subsumption failed",
		},
		72: {
			in:  `a: {s: { a:1} }, b: { s: {}}`,
			err: "subsumption failed",
		},

		84: {
			in:  `a: 1 | 2, b: 2 | 1`,
			err: "",
		},
		85: {
			in:  `a: 1 | 2, b: 1 | 2`,
			err: "",
		},

		86: {
			in:  `a: number, b: 2 | 1`,
			err: "",
		},
		87: {
			in:  `a: number, b: 2 | 1`,
			err: "",
		},
		88: {
			in:  `a: int, b: 1 | 2 | 3.1`,
			err: "subsumption failed",
		},

		89: {
			in:  `a: float | number, b: 1 | 2 | 3.1`,
			err: "",
		},

		90: {
			in:  `a: int, b: 1 | 2 | 3.1`,
			err: "subsumption failed",
		},
		91: {
			in:  `a: 1 | 2, b: 1`,
			err: "",
		},
		92: {
			in:  `a: 1 | 2, b: 2`,
			err: "",
		},
		93: {
			in:  `a: 1 | 2, b: 3`,
			err: "subsumption failed",
		},

		// 147: {subsumes: true, in: ` a: 7080, b: *7080 | int`, mode: subChoose},

		// Defaults
		150: {
			in:  `a: number | *1, b: number | *2`,
			err: "subsumption failed",
		},
		151: {
			in:  `a: number | *2, b: number | *2`,
			err: "",
		},
		152: {
			in:  `a: int | *float, b: int | *2.0`,
			err: "",
		},
		153: {
			in:  `a: int | *2, b: int | *2.0`,
			err: "subsumption failed",
		},
		154: {
			in:  `a: number | *2 | *3, b: number | *2`,
			err: "",
		},
		155: {
			in:  `a: number, b: number | *2`,
			err: "",
		},

		// Bounds
		170: {
			in:  `a: >=2, b: >=2`,
			err: "",
		},
		171: {
			in:  `a: >=1, b: >=2`,
			err: "",
		},
		172: {
			in:  `a: >0, b: >=2`,
			err: "",
		},
		173: {
			in:  `a: >1, b: >1`,
			err: "",
		},
		174: {
			in:  `a: >=1, b: >1`,
			err: "",
		},
		175: {
			in:  `a: >1, b: >=1`,
			err: "subsumption failed",
		},
		176: {
			in:  `a: >=1, b: >=1`,
			err: "",
		},
		177: {
			in:  `a: <1, b: <1`,
			err: "",
		},
		178: {
			in:  `a: <=1, b: <1`,
			err: "",
		},
		179: {
			in:  `a: <1, b: <=1`,
			err: "subsumption failed",
		},
		180: {
			in:  `a: <=1, b: <=1`,
			err: "",
		},

		181: {
			in:  `a: !=1, b: !=1`,
			err: "",
		},
		182: {
			in:  `a: !=1, b: !=2`,
			err: "subsumption failed",
		},

		183: {
			in:  `a: !=1, b: <=1`,
			err: "subsumption failed",
		},
		184: {
			in:  `a: !=1, b: <1`,
			err: "",
		},
		185: {
			in:  `a: !=1, b: >=1`,
			err: "subsumption failed",
		},
		186: {
			in:  `a: !=1, b: <1`,
			err: "",
		},

		187: {
			in:  `a: !=1, b: <=0`,
			err: "",
		},
		188: {
			in:  `a: !=1, b: >=2`,
			err: "",
		},
		189: {
			in:  `a: !=1, b: >1`,
			err: "",
		},

		195: {
			in:  `a: >=2, b: !=2`,
			err: "subsumption failed",
		},
		196: {
			in:  `a: >2, b: !=2`,
			err: "subsumption failed",
		},
		197: {
			in:  `a: <2, b: !=2`,
			err: "subsumption failed",
		},
		198: {
			in:  `a: <=2, b: !=2`,
			err: "subsumption failed",
		},

		200: {
			in:  `a: =~"foo", b: =~"foo"`,
			err: "",
		},
		201: {
			in:  `a: =~"foo", b: =~"bar"`,
			err: "subsumption failed",
		},
		202: {
			in:  `a: =~"foo1", b: =~"foo"`,
			err: "subsumption failed",
		},

		203: {
			in:  `a: !~"foo", b: !~"foo"`,
			err: "",
		},
		204: {
			in:  `a: !~"foo", b: !~"bar"`,
			err: "subsumption failed",
		},
		205: {
			in:  `a: !~"foo", b: !~"foo1"`,
			err: "subsumption failed",
		},

		// The following is could be true, but we will not go down the rabbit
		// hold of trying to prove subsumption of regular expressions.
		210: {
			in:  `a: =~"foo", b: =~"foo1"`,
			err: "subsumption failed",
		},
		211: {
			in:  `a: !~"foo1", b: !~"foo"`,
			err: "subsumption failed",
		},

		220: {
			in:  `a: <5, b: 4`,
			err: "",
		},
		221: {
			in:  `a: <5, b: 5`,
			err: "subsumption failed",
		},
		222: {
			in:  `a: <=5, b: 5`,
			err: "",
		},
		223: {
			in:  `a: <=5.0, b: 5.00000001`,
			err: "subsumption failed",
		},
		224: {
			in:  `a: >5, b: 6`,
			err: "",
		},
		225: {
			in:  `a: >5, b: 5`,
			err: "subsumption failed",
		},
		226: {
			in:  `a: >=5, b: 5`,
			err: "",
		},
		227: {
			in:  `a: >=5, b: 4`,
			err: "subsumption failed",
		},
		228: {
			in:  `a: !=5, b: 6`,
			err: "",
		},
		229: {
			in:  `a: !=5, b: 5`,
			err: "subsumption failed",
		},
		230: {
			in:  `a: !=5.0, b: 5.0`,
			err: "subsumption failed",
		},
		231: {
			in:  `a: !=5.0, b: 5`,
			err: "subsumption failed",
		},

		250: {
			in:  `a: =~ #"^\d{3}$"#, b: "123"`,
			err: "",
		},
		251: {
			in:  `a: =~ #"^\d{3}$"#, b: "1234"`,
			err: "subsumption failed",
		},
		252: {
			in:  `a: !~ #"^\d{3}$"#, b: "1234"`,
			err: "",
		},
		253: {
			in:  `a: !~ #"^\d{3}$"#, b: "123"`,
			err: "subsumption failed",
		},

		// Conjunctions
		300: {
			in:  `a: >0, b: >=2 & <=100`,
			err: "",
		},
		301: {
			in:  `a: >0, b: >=0 & <=100`,
			err: "subsumption failed",
		},

		310: {
			in:  `a: >=0 & <=100, b: 10`,
			err: "",
		},
		311: {
			in:  `a: >=0 & <=100, b: >=0 & <=100`,
			err: "",
		},
		312: {
			in:  `a: !=2 & !=4, b: >3`,
			err: "subsumption failed",
		},
		313: {
			in:  `a: !=2 & !=4, b: >5`,
			err: "",
		},

		314: {
			in:  `a: >=0 & <=100, b: >=0 & <=150`,
			err: "subsumption failed",
		},
		315: {
			in:  `a: >=0 & <=150, b: >=0 & <=100`,
			err: "",
		},

		// Disjunctions
		330: {
			in:  `a: >5, b: >10 | 8`,
			err: "",
		},
		331: {
			in:  `a: >8, b: >10 | 8`,
			err: "subsumption failed",
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
			in:  `a: {foo: 1}, b: {}`,
			err: "subsumption failed",
		},
		401: {
			in:  `a: {foo?: 1}, b: {}`,
			err: "subsumption failed",
		},
		402: {
			in:  `a: {}, b: {foo: 1}`,
			err: "",
		},
		403: {
			in:  `a: {}, b: {foo?: 1}`,
			err: "",
		},

		404: {
			in:  `a: {foo: 1}, b: {foo: 1}`,
			err: "",
		},
		405: {
			in:  `a: {foo?: 1}, b: {foo: 1}`,
			err: "",
		},
		406: {
			in:  `a: {foo?: 1}, b: {foo?: 1}`,
			err: "",
		},
		407: {
			in:  `a: {foo: 1}, b: {foo?: 1}`,
			err: "subsumption failed",
		},

		408: {
			in:  `a: {foo: 1}, b: {foo: 2}`,
			err: "subsumption failed",
		},
		409: {
			in:  `a: {foo?: 1}, b: {foo: 2}`,
			err: "subsumption failed",
		},
		410: {
			in:  `a: {foo?: 1}, b: {foo?: 2}`,
			err: "subsumption failed",
		},
		411: {
			in:  `a: {foo: 1}, b: {foo?: 2}`,
			err: "subsumption failed",
		},

		412: {
			in:  `a: {foo: number}, b: {foo: 2}`,
			err: "",
		},
		413: {
			in:  `a: {foo?: number}, b: {foo: 2}`,
			err: "",
		},
		414: {
			in:  `a: {foo?: number}, b: {foo?: 2}`,
			err: "",
		},
		415: {
			in:  `a: {foo: number}, b: {foo?: 2}`,
			err: "subsumption failed",
		},

		416: {
			in:  `a: {foo: 1}, b: {foo: number}`,
			err: "subsumption failed",
		},
		417: {
			in:  `a: {foo?: 1}, b: {foo: number}`,
			err: "subsumption failed",
		},
		418: {
			in:  `a: {foo?: 1}, b: {foo?: number}`,
			err: "subsumption failed",
		},
		419: {
			in:  `a: {foo: 1}, b: {foo?: number}`,
			err: "subsumption failed",
		},

		// The one exception of the rule: there is no value of foo that can be
		// added to b which would cause the unification of a and b to fail.
		// So an optional field with a value of top is equivalent to not
		// defining one at all.
		420: {
			in:  `a: {foo?: _}, b: {}`,
			err: "",
		},

		430: {
			in:  `a: {[_]: 4}, b: {[_]: int}`,
			err: "subsumption failed",
		},
		431: {
			in:      `a: {[_]: int}, b: {[_]: 2}`,
			skip_v2: true,
			err:     "",
		},
		432: {
			in:      `a: {[string]: int, [<"m"]: 3}, b: {[string]: 2, [<"m"]: 3}`,
			skip_v2: true,
			err:     "",
		},
		433: {
			in:      `a: {[<"m"]: 3, [string]: int}, b: {[string]: 2, [<"m"]: 3}`,
			skip_v2: true,
			err:     "",
		},
		434: {
			in:  `a: {[<"n"]: 3, [string]: int}, b: {[string]: 2, [<"m"]: 3}`,
			err: "subsumption failed",
		},
		435: {
			// both sides unify to a single string pattern.
			in:      `a: {[string]: <5, [string]: int}, b: {[string]: <=3, [string]: 3}`,
			skip_v2: true,
			err:     "",
		},
		436: {
			// matches because bottom is subsumed by >5
			in:      `a: {[string]: >5}, b: {[string]: 1, [string]: 2}`,
			skip_v2: true,
			err:     "",
		},
		437: {
			// subsumption gives up if a has more pattern constraints than b.
			// TODO: support this?
			in:  `a: {[_]: >5, [>"b"]: int}, b: {[_]: 6}`,
			err: "subsumption failed",
		},
		438: {
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
		460: {
			in:  `a: {1, #foo: number}, b: {1, #foo: 1}`,
			err: "",
		},
		461: {
			in:  `a: {1, #foo?: number}, b: {1, #foo: 1}`,
			err: "",
		},
		462: {
			in:  `a: {1, #foo?: number}, b: {1, #foo?: 1}`,
			err: "",
		},
		463: {
			in:  `a: {1, #foo: number}, b: {1, #foo?: 1}`,
			err: "subsumption failed",
		},

		464: {
			in:  `a: {int, #foo: number}, b: {1, #foo: 1}`,
			err: "",
		},
		465: {
			in:  `a: {int, #foo: 1}, b: {1, #foo: number}`,
			err: "subsumption failed",
		},
		466: {
			in:  `a: {1, #foo: number}, b: {int, #foo: 1}`,
			err: "subsumption failed",
		},
		467: {
			in:  `a: {1, #foo: 1}, b: {int, #foo: number}`,
			err: "subsumption failed",
		},

		// Lists
		506: {
			in:  `a: [], b: [] `,
			err: "",
		},
		507: {
			in:  `a: [1], b: [1] `,
			err: "",
		},
		508: {
			in:  `a: [1], b: [2] `,
			err: "subsumption failed",
		},
		509: {
			in:  `a: [1], b: [2, 3] `,
			err: "subsumption failed",
		},
		510: {
			in:  `a: [{b: string}], b: [{b: "foo"}] `,
			err: "",
		},
		511: {
			in:  `a: [...{b: string}], b: [{b: "foo"}] `,
			err: "",
		},
		512: {
			in:  `a: [{b: "foo"}], b: [{b: string}] `,
			err: "subsumption failed",
		},
		513: {
			in:  `a: [{b: string}], b: [{b: "foo"}, ...{b: "foo"}] `,
			err: "subsumption failed",
		},
		520: {
			in:  `a: [_, int, ...], b: [int, string, ...string] `,
			err: "subsumption failed",
		},

		// Closed structs.
		600: {
			in:  `a: close({}), b: {a: 1}`,
			err: "subsumption failed",
		},
		601: {
			in:  `a: close({a: 1}), b: {a: 1}`,
			err: "subsumption failed",
		},
		602: {
			in:  `a: close({a: 1, b: 1}), b: {a: 1}`,
			err: "subsumption failed",
		},
		603: {
			in:  `a: {a: 1}, b: close({})`,
			err: "subsumption failed",
		},
		604: {
			in:  `a: {a: 1}, b: close({a: 1})`,
			err: "",
		},
		605: {
			in:  `a: {a: 1}, b: close({a: 1, b: 1})`,
			err: "",
		},
		606: {
			in:  `a: close({b?: 1}), b: close({b: 1})`,
			err: "",
		},
		607: {
			in:  `a: close({b: 1}), b: close({b?: 1})`,
			err: "subsumption failed",
		},
		608: {
			in:  `a: {}, b: close({})`,
			err: "",
		},
		609: {
			in:  `a: {}, b: close({foo?: 1})`,
			err: "",
		},
		610: {
			in:  `a: {foo?:1}, b: close({})`,
			err: "",
		},

		// New in new evaluator.
		611: {
			in:  `a: close({foo?:1}), b: close({bar?: 1})`,
			err: "subsumption failed",
		},
		612: {
			in:  `a: {foo?:1}, b: close({bar?: 1})`,
			err: "",
		},
		613: {
			in:  `a: {foo?:1}, b: close({bar: 1})`,
			err: "",
		},

		// Definitions are not regular fields.
		630: {
			in:  `a: {#a: 1}, b: {a: 1}`,
			err: "subsumption failed",
		},
		631: {
			in:  `a: {a: 1}, b: {#a: 1}`,
			err: "subsumption failed",
		},

		// Subsuming final values.
		700: {
			in:   `a: [string]: 1, b: {foo: 1}`,
			mode: subFinal,
			err:  "",
		},
		701: {
			in:   `a: [string]: int, b: {foo: 1}`,
			mode: subFinal,
			err:  "",
		},
		702: {
			in:   `a: {["foo"]: int}, b: {foo: 1}`,
			mode: subFinal,
			err:  "",
		},
		703: {
			in:   `a: close({["foo"]: 1}), b: {bar: 1}`,
			mode: subFinal,
			err:  "subsumption failed",
		},
		704: {
			in:   `a: {foo: 1}, b: {foo?: 1}`,
			mode: subFinal,
			err:  "subsumption failed",
		},
		705: {
			in:   `a: close({}), b: {foo?: 1}`,
			mode: subFinal,
			err:  "",
		},
		706: {
			in:   `a: close({}), b: close({foo?: 1})`,
			mode: subFinal,
			err:  "",
		},
		707: {
			in:   `a: {}, b: close({})`,
			mode: subFinal,
			err:  "",
		},
		708: {
			in:   `a: {[string]: 1}, b: {foo: 2}`,
			mode: subFinal,
			err:  "subsumption failed",
		},
		709: {
			in:   `a: {}, b: close({foo?: 1})`,
			mode: subFinal,
			err:  "",
		},
		710: {
			in:   `a: {foo: [...string]}, b: {}`,
			mode: subFinal,
			err:  "subsumption failed",
		},

		// Schema values
		800: {
			in:   `a: close({}), b: {foo: 1}`,
			mode: subSchema,
			err:  "",
		},
		// TODO(eval): FIX
		// 801: {subsumes: true, in: `a: {[string]: int}, b: {foo: 1}`, mode: subSchema},
		804: {
			in:   `a: {foo: 1}, b: {foo?: 1}`,
			mode: subSchema,
			err:  "subsumption failed",
		},
		805: {
			in:   `a: close({}), b: {foo?: 1}`,
			mode: subSchema,
			err:  "",
		},
		806: {
			in:   `a: close({}), b: close({foo?: 1})`,
			mode: subSchema,
			err:  "",
		},
		807: {
			in:   `a: {}, b: close({})`,
			mode: subSchema,
			err:  "",
		},
		808: {
			in:   `a: {[string]: 1}, b: {foo: 2}`,
			mode: subSchema,
			err:  "subsumption failed",
		},
		809: {
			in:   `a: {}, b: close({foo?: 1})`,
			mode: subSchema,
			err:  "",
		},

		// Lists
		950: {
			in:  `a: [], b: []`,
			err: "",
		},
		951: {
			in:  `a: [...], b: []`,
			err: "",
		},
		952: {
			in:  `a: [...], b: [...]`,
			err: "",
		},
		953: {
			in:  `a: [], b: [...]`,
			err: "subsumption failed",
		},

		954: {
			in:  `a: [2], b: [2]`,
			err: "",
		},
		955: {
			in:  `a: [int], b: [2]`,
			err: "",
		},
		956: {
			in:  `a: [2], b: [int]`,
			err: "subsumption failed",
		},
		957: {
			in:  `a: [int], b: [int]`,
			err: "",
		},

		958: {
			in:  `a: [...2], b: [2]`,
			err: "",
		},
		959: {
			in:  `a: [...int], b: [2]`,
			err: "",
		},
		960: {
			in:  `a: [...2], b: [int]`,
			err: "subsumption failed",
		},
		961: {
			in:  `a: [...int], b: [int]`,
			err: "",
		},

		962: {
			in:  `a: [2], b: [...2]`,
			err: "subsumption failed",
		},
		963: {
			in:  `a: [int], b: [...2]`,
			err: "subsumption failed",
		},
		964: {
			in:  `a: [2], b: [...int]`,
			err: "subsumption failed",
		},
		965: {
			in:  `a: [int], b: [...int]`,
			err: "subsumption failed",
		},

		966: {
			in:  `a: [...int], b: ["foo"]`,
			err: "subsumption failed",
		},
		967: {
			in:  `a: ["foo"], b: [...int]`,
			err: "subsumption failed",
		},

		// Defaults:
		// TODO: for the purpose of v0.2 compatibility these
		// evaluate to true. Reconsider before making this package
		// public.
		970: {
			in:   `a: [], b: [...int]`,
			mode: subDefaults,
			err:  "",
		},
		971: {
			in:   `a: [2], b: [2, ...int]`,
			mode: subDefaults,
			err:  "",
		},

		// Final
		980: {
			in:   `a: [], b: [...int]`,
			mode: subFinal,
			err:  "",
		},
		981: {
			in:   `a: [2], b: [2, ...int]`,
			mode: subFinal,
			err:  "",
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
			var gotErr string
			if err != nil {
				gotErr = err.Error()
			}

			if tc.err == "" {
				// Expected success
				if err != nil {
					t.Errorf("got error %q; want success", gotErr)
				}
			} else {
				// Expected failure - just check that we got any error
				if err == nil {
					t.Errorf("got success; want failure")
				}
			}
		})
	}
}
