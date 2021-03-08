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

package subsume

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
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
	}
	testCases := []subsumeTest{
		// Top subsumes everything
		0: {subsumes: true, in: `a: _, b: _ `},
		1: {subsumes: true, in: `a: _, b: null `},
		2: {subsumes: true, in: `a: _, b: int `},
		3: {subsumes: true, in: `a: _, b: 1 `},
		4: {subsumes: true, in: `a: _, b: float `},
		5: {subsumes: true, in: `a: _, b: "s" `},
		6: {subsumes: true, in: `a: _, b: {} `},
		7: {subsumes: true, in: `a: _, b: []`},
		8: {subsumes: true, in: `a: _, b: _|_ `},

		// Nothing besides top subsumed top
		9:  {subsumes: false, in: `a: null,    b: _`},
		10: {subsumes: false, in: `a: int, b: _`},
		11: {subsumes: false, in: `a: 1,       b: _`},
		12: {subsumes: false, in: `a: float, b: _`},
		13: {subsumes: false, in: `a: "s",     b: _`},
		14: {subsumes: false, in: `a: {},      b: _`},
		15: {subsumes: false, in: `a: [],      b: _`},
		16: {subsumes: false, in: `a: _|_ ,      b: _`},

		// Bottom subsumes nothing except bottom itself.
		17: {subsumes: false, in: `a: _|_, b: null `},
		18: {subsumes: false, in: `a: _|_, b: int `},
		19: {subsumes: false, in: `a: _|_, b: 1 `},
		20: {subsumes: false, in: `a: _|_, b: float `},
		21: {subsumes: false, in: `a: _|_, b: "s" `},
		22: {subsumes: false, in: `a: _|_, b: {} `},
		23: {subsumes: false, in: `a: _|_, b: [] `},
		24: {subsumes: true, in: ` a: _|_, b: _|_ `},

		// All values subsume bottom
		25: {subsumes: true, in: `a: null,    b: _|_`},
		26: {subsumes: true, in: `a: int, b: _|_`},
		27: {subsumes: true, in: `a: 1,       b: _|_`},
		28: {subsumes: true, in: `a: float, b: _|_`},
		29: {subsumes: true, in: `a: "s",     b: _|_`},
		30: {subsumes: true, in: `a: {},      b: _|_`},
		31: {subsumes: true, in: `a: [],      b: _|_`},
		32: {subsumes: true, in: `a: true,    b: _|_`},
		33: {subsumes: true, in: `a: _|_,       b: _|_`},

		// null subsumes only null
		34: {subsumes: true, in: ` a: null, b: null `},
		35: {subsumes: false, in: `a: null, b: 1 `},
		36: {subsumes: false, in: `a: 1,    b: null `},

		37: {subsumes: true, in: ` a: true, b: true `},
		38: {subsumes: false, in: `a: true, b: false `},

		39: {subsumes: true, in: ` a: "a",    b: "a" `},
		40: {subsumes: false, in: `a: "a",    b: "b" `},
		41: {subsumes: true, in: ` a: string, b: "a" `},
		42: {subsumes: false, in: `a: "a",    b: string `},

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
		43: {subsumes: true, in: `a: 1, b: 1 `},
		44: {subsumes: true, in: `a: 1.0, b: 1.0 `},
		45: {subsumes: true, in: `a: 3.0, b: 3.0 `},
		46: {subsumes: false, in: `a: 1.0, b: 1 `},
		47: {subsumes: false, in: `a: 1, b: 1.0 `},
		48: {subsumes: false, in: `a: 3, b: 3.0`},
		49: {subsumes: true, in: `a: int, b: 1`},
		50: {subsumes: true, in: `a: int, b: int & 1`},
		51: {subsumes: true, in: `a: float, b: 1.0`},
		52: {subsumes: false, in: `a: float, b: 1`},
		53: {subsumes: false, in: `a: int, b: 1.0`},
		54: {subsumes: true, in: `a: int, b: int`},
		55: {subsumes: true, in: `a: number, b: int`},

		// Structs
		64: {subsumes: true, in: `a: {}, b: {}`},
		65: {subsumes: true, in: `a: {}, b: {a: 1}`},
		66: {subsumes: true, in: `a: {a:1}, b: {a:1, b:1}`},
		67: {subsumes: true, in: `a: {s: { a:1} }, b: { s: { a:1, b:2 }}`},
		68: {subsumes: true, in: `a: {}, b: {}`},
		// TODO: allow subsumption of unevaluated values?
		// ref not yet evaluated and not structurally equivalent
		69: {subsumes: true, in: `a: {}, b: {} & c, c: {}`},

		70: {subsumes: false, in: `a: {a:1}, b: {}`},
		71: {subsumes: false, in: `a: {a:1, b:1}, b: {a:1}`},
		72: {subsumes: false, in: `a: {s: { a:1} }, b: { s: {}}`},

		84: {subsumes: true, in: `a: 1 | 2, b: 2 | 1`},
		85: {subsumes: true, in: `a: 1 | 2, b: 1 | 2`},

		86: {subsumes: true, in: `a: number, b: 2 | 1`},
		87: {subsumes: true, in: `a: number, b: 2 | 1`},
		88: {subsumes: false, in: `a: int, b: 1 | 2 | 3.1`},

		89: {subsumes: true, in: `a: float | number, b: 1 | 2 | 3.1`},

		90: {subsumes: false, in: `a: int, b: 1 | 2 | 3.1`},
		91: {subsumes: true, in: `a: 1 | 2, b: 1`},
		92: {subsumes: true, in: `a: 1 | 2, b: 2`},
		93: {subsumes: false, in: `a: 1 | 2, b: 3`},

		// 147: {subsumes: true, in: ` a: 7080, b: *7080 | int`, mode: subChoose},

		// Defaults
		150: {subsumes: false, in: `a: number | *1, b: number | *2`},
		151: {subsumes: true, in: `a: number | *2, b: number | *2`},
		152: {subsumes: true, in: `a: int | *float, b: int | *2.0`},
		153: {subsumes: false, in: `a: int | *2, b: int | *2.0`},
		154: {subsumes: true, in: `a: number | *2 | *3, b: number | *2`},
		155: {subsumes: true, in: `a: number, b: number | *2`},

		// Bounds
		170: {subsumes: true, in: `a: >=2, b: >=2`},
		171: {subsumes: true, in: `a: >=1, b: >=2`},
		172: {subsumes: true, in: `a: >0, b: >=2`},
		173: {subsumes: true, in: `a: >1, b: >1`},
		174: {subsumes: true, in: `a: >=1, b: >1`},
		175: {subsumes: false, in: `a: >1, b: >=1`},
		176: {subsumes: true, in: `a: >=1, b: >=1`},
		177: {subsumes: true, in: `a: <1, b: <1`},
		178: {subsumes: true, in: `a: <=1, b: <1`},
		179: {subsumes: false, in: `a: <1, b: <=1`},
		180: {subsumes: true, in: `a: <=1, b: <=1`},

		181: {subsumes: true, in: `a: !=1, b: !=1`},
		182: {subsumes: false, in: `a: !=1, b: !=2`},

		183: {subsumes: false, in: `a: !=1, b: <=1`},
		184: {subsumes: true, in: `a: !=1, b: <1`},
		185: {subsumes: false, in: `a: !=1, b: >=1`},
		186: {subsumes: true, in: `a: !=1, b: <1`},

		187: {subsumes: true, in: `a: !=1, b: <=0`},
		188: {subsumes: true, in: `a: !=1, b: >=2`},
		189: {subsumes: true, in: `a: !=1, b: >1`},

		195: {subsumes: false, in: `a: >=2, b: !=2`},
		196: {subsumes: false, in: `a: >2, b: !=2`},
		197: {subsumes: false, in: `a: <2, b: !=2`},
		198: {subsumes: false, in: `a: <=2, b: !=2`},

		200: {subsumes: true, in: `a: =~"foo", b: =~"foo"`},
		201: {subsumes: false, in: `a: =~"foo", b: =~"bar"`},
		202: {subsumes: false, in: `a: =~"foo1", b: =~"foo"`},

		203: {subsumes: true, in: `a: !~"foo", b: !~"foo"`},
		204: {subsumes: false, in: `a: !~"foo", b: !~"bar"`},
		205: {subsumes: false, in: `a: !~"foo", b: !~"foo1"`},

		// The following is could be true, but we will not go down the rabbit
		// hold of trying to prove subsumption of regular expressions.
		210: {subsumes: false, in: `a: =~"foo", b: =~"foo1"`},
		211: {subsumes: false, in: `a: !~"foo1", b: !~"foo"`},

		220: {subsumes: true, in: `a: <5, b: 4`},
		221: {subsumes: false, in: `a: <5, b: 5`},
		222: {subsumes: true, in: `a: <=5, b: 5`},
		223: {subsumes: false, in: `a: <=5.0, b: 5.00000001`},
		224: {subsumes: true, in: `a: >5, b: 6`},
		225: {subsumes: false, in: `a: >5, b: 5`},
		226: {subsumes: true, in: `a: >=5, b: 5`},
		227: {subsumes: false, in: `a: >=5, b: 4`},
		228: {subsumes: true, in: `a: !=5, b: 6`},
		229: {subsumes: false, in: `a: !=5, b: 5`},
		230: {subsumes: false, in: `a: !=5.0, b: 5.0`},
		231: {subsumes: false, in: `a: !=5.0, b: 5`},

		250: {subsumes: true, in: `a: =~ #"^\d{3}$"#, b: "123"`},
		251: {subsumes: false, in: `a: =~ #"^\d{3}$"#, b: "1234"`},
		252: {subsumes: true, in: `a: !~ #"^\d{3}$"#, b: "1234"`},
		253: {subsumes: false, in: `a: !~ #"^\d{3}$"#, b: "123"`},

		// Conjunctions
		300: {subsumes: true, in: `a: >0, b: >=2 & <=100`},
		301: {subsumes: false, in: `a: >0, b: >=0 & <=100`},

		310: {subsumes: true, in: `a: >=0 & <=100, b: 10`},
		311: {subsumes: true, in: `a: >=0 & <=100, b: >=0 & <=100`},
		312: {subsumes: false, in: `a: !=2 & !=4, b: >3`},
		313: {subsumes: true, in: `a: !=2 & !=4, b: >5`},

		314: {subsumes: false, in: `a: >=0 & <=100, b: >=0 & <=150`},
		315: {subsumes: true, in: `a: >=0 & <=150, b: >=0 & <=100`},

		// Disjunctions
		330: {subsumes: true, in: `a: >5, b: >10 | 8`},
		331: {subsumes: false, in: `a: >8, b: >10 | 8`},

		// Optional fields
		// Optional fields defined constraints on fields that are not yet
		// defined. So even if such a field is not part of the output, it
		// influences the lattice structure.
		// For a given A and B, where A and B unify and where A has an optional
		// field that is not defined in B, the addition of an incompatible
		// value of that field in B can cause A and B to no longer unify.
		//
		400: {subsumes: false, in: `a: {foo: 1}, b: {}`},
		401: {subsumes: false, in: `a: {foo?: 1}, b: {}`},
		402: {subsumes: true, in: `a: {}, b: {foo: 1}`},
		403: {subsumes: true, in: `a: {}, b: {foo?: 1}`},

		404: {subsumes: true, in: `a: {foo: 1}, b: {foo: 1}`},
		405: {subsumes: true, in: `a: {foo?: 1}, b: {foo: 1}`},
		406: {subsumes: true, in: `a: {foo?: 1}, b: {foo?: 1}`},
		407: {subsumes: false, in: `a: {foo: 1}, b: {foo?: 1}`},

		408: {subsumes: false, in: `a: {foo: 1}, b: {foo: 2}`},
		409: {subsumes: false, in: `a: {foo?: 1}, b: {foo: 2}`},
		410: {subsumes: false, in: `a: {foo?: 1}, b: {foo?: 2}`},
		411: {subsumes: false, in: `a: {foo: 1}, b: {foo?: 2}`},

		412: {subsumes: true, in: `a: {foo: number}, b: {foo: 2}`},
		413: {subsumes: true, in: `a: {foo?: number}, b: {foo: 2}`},
		414: {subsumes: true, in: `a: {foo?: number}, b: {foo?: 2}`},
		415: {subsumes: false, in: `a: {foo: number}, b: {foo?: 2}`},

		416: {subsumes: false, in: `a: {foo: 1}, b: {foo: number}`},
		417: {subsumes: false, in: `a: {foo?: 1}, b: {foo: number}`},
		418: {subsumes: false, in: `a: {foo?: 1}, b: {foo?: number}`},
		419: {subsumes: false, in: `a: {foo: 1}, b: {foo?: number}`},

		// The one exception of the rule: there is no value of foo that can be
		// added to b which would cause the unification of a and b to fail.
		// So an optional field with a value of top is equivalent to not
		// defining one at all.
		420: {subsumes: true, in: `a: {foo?: _}, b: {}`},

		430: {subsumes: false, in: `a: {[_]: 4}, b: {[_]: int}`},
		// TODO: handle optionals.
		431: {subsumes: false, in: `a: {[_]: int}, b: {[_]: 2}`},

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
		460: {subsumes: true, in: `a: {1, #foo: number}, b: {1, #foo: 1}`},
		461: {subsumes: true, in: `a: {1, #foo?: number}, b: {1, #foo: 1}`},
		462: {subsumes: true, in: `a: {1, #foo?: number}, b: {1, #foo?: 1}`},
		463: {subsumes: false, in: `a: {1, #foo: number}, b: {1, #foo?: 1}`},

		464: {subsumes: true, in: `a: {int, #foo: number}, b: {1, #foo: 1}`},
		465: {subsumes: false, in: `a: {int, #foo: 1}, b: {1, #foo: number}`},
		466: {subsumes: false, in: `a: {1, #foo: number}, b: {int, #foo: 1}`},
		467: {subsumes: false, in: `a: {1, #foo: 1}, b: {int, #foo: number}`},

		// Lists
		506: {subsumes: true, in: `a: [], b: [] `},
		507: {subsumes: true, in: `a: [1], b: [1] `},
		508: {subsumes: false, in: `a: [1], b: [2] `},
		509: {subsumes: false, in: `a: [1], b: [2, 3] `},
		510: {subsumes: true, in: `a: [{b: string}], b: [{b: "foo"}] `},
		511: {subsumes: true, in: `a: [...{b: string}], b: [{b: "foo"}] `},
		512: {subsumes: false, in: `a: [{b: "foo"}], b: [{b: string}] `},
		513: {subsumes: false, in: `a: [{b: string}], b: [{b: "foo"}, ...{b: "foo"}] `},
		520: {subsumes: false, in: `a: [_, int, ...], b: [int, string, ...string] `},

		// Closed structs.
		600: {subsumes: false, in: `a: close({}), b: {a: 1}`},
		601: {subsumes: false, in: `a: close({a: 1}), b: {a: 1}`},
		602: {subsumes: false, in: `a: close({a: 1, b: 1}), b: {a: 1}`},
		603: {subsumes: false, in: `a: {a: 1}, b: close({})`},
		604: {subsumes: true, in: `a: {a: 1}, b: close({a: 1})`},
		605: {subsumes: true, in: `a: {a: 1}, b: close({a: 1, b: 1})`},
		606: {subsumes: true, in: `a: close({b?: 1}), b: close({b: 1})`},
		607: {subsumes: false, in: `a: close({b: 1}), b: close({b?: 1})`},
		608: {subsumes: true, in: `a: {}, b: close({})`},
		609: {subsumes: true, in: `a: {}, b: close({foo?: 1})`},
		610: {subsumes: true, in: `a: {foo?:1}, b: close({})`},

		// New in new evaluator.
		611: {subsumes: false, in: `a: close({foo?:1}), b: close({bar?: 1})`},
		612: {subsumes: true, in: `a: {foo?:1}, b: close({bar?: 1})`},
		613: {subsumes: true, in: `a: {foo?:1}, b: close({bar: 1})`},

		// Definitions are not regular fields.
		630: {subsumes: false, in: `a: {#a: 1}, b: {a: 1}`},
		631: {subsumes: false, in: `a: {a: 1}, b: {#a: 1}`},

		// Subsuming final values.
		700: {subsumes: true, in: `a: [string]: 1, b: {foo: 1}`, mode: subFinal},
		701: {subsumes: true, in: `a: [string]: int, b: {foo: 1}`, mode: subFinal},
		702: {subsumes: true, in: `a: {["foo"]: int}, b: {foo: 1}`, mode: subFinal},
		703: {subsumes: false, in: `a: close({["foo"]: 1}), b: {bar: 1}`, mode: subFinal},
		704: {subsumes: false, in: `a: {foo: 1}, b: {foo?: 1}`, mode: subFinal},
		705: {subsumes: true, in: `a: close({}), b: {foo?: 1}`, mode: subFinal},
		706: {subsumes: true, in: `a: close({}), b: close({foo?: 1})`, mode: subFinal},
		707: {subsumes: true, in: `a: {}, b: close({})`, mode: subFinal},
		708: {subsumes: false, in: `a: {[string]: 1}, b: {foo: 2}`, mode: subFinal},
		709: {subsumes: true, in: `a: {}, b: close({foo?: 1})`, mode: subFinal},
		710: {subsumes: false, in: `a: {foo: [...string]}, b: {}`, mode: subFinal},

		// Schema values
		800: {subsumes: true, in: `a: close({}), b: {foo: 1}`, mode: subSchema},
		// TODO(eval): FIX
		// 801: {subsumes: true, in: `a: {[string]: int}, b: {foo: 1}`, mode: subSchema},
		804: {subsumes: false, in: `a: {foo: 1}, b: {foo?: 1}`, mode: subSchema},
		805: {subsumes: true, in: `a: close({}), b: {foo?: 1}`, mode: subSchema},
		806: {subsumes: true, in: `a: close({}), b: close({foo?: 1})`, mode: subSchema},
		807: {subsumes: true, in: `a: {}, b: close({})`, mode: subSchema},
		808: {subsumes: false, in: `a: {[string]: 1}, b: {foo: 2}`, mode: subSchema},
		809: {subsumes: true, in: `a: {}, b: close({foo?: 1})`, mode: subSchema},

		// Lists
		950: {subsumes: true, in: `a: [], b: []`},
		951: {subsumes: true, in: `a: [...], b: []`},
		952: {subsumes: true, in: `a: [...], b: [...]`},
		953: {subsumes: false, in: `a: [], b: [...]`},

		954: {subsumes: true, in: `a: [2], b: [2]`},
		955: {subsumes: true, in: `a: [int], b: [2]`},
		956: {subsumes: false, in: `a: [2], b: [int]`},
		957: {subsumes: true, in: `a: [int], b: [int]`},

		958: {subsumes: true, in: `a: [...2], b: [2]`},
		959: {subsumes: true, in: `a: [...int], b: [2]`},
		960: {subsumes: false, in: `a: [...2], b: [int]`},
		961: {subsumes: true, in: `a: [...int], b: [int]`},

		962: {subsumes: false, in: `a: [2], b: [...2]`},
		963: {subsumes: false, in: `a: [int], b: [...2]`},
		964: {subsumes: false, in: `a: [2], b: [...int]`},
		965: {subsumes: false, in: `a: [int], b: [...int]`},

		966: {subsumes: false, in: `a: [...int], b: ["foo"]`},
		967: {subsumes: false, in: `a: ["foo"], b: [...int]`},

		// Defaults:
		// TODO: for the purpose of v0.2 compatibility these
		// evaluate to true. Reconsider before making this package
		// public.
		970: {subsumes: true, in: `a: [], b: [...int]`, mode: subDefaults},
		971: {subsumes: true, in: `a: [2], b: [2, ...int]`, mode: subDefaults},

		// Final
		980: {subsumes: true, in: `a: [], b: [...int]`, mode: subFinal},
		981: {subsumes: true, in: `a: [2], b: [2, ...int]`, mode: subFinal},
	}

	re := regexp.MustCompile(`a: (.*).*b: ([^\n]*)`)
	for i, tc := range testCases {
		if tc.in == "" {
			continue
		}
		m := re.FindStringSubmatch(strings.Join(strings.Split(tc.in, "\n"), ""))
		const cutset = "\n ,"
		key := strings.Trim(m[1], cutset) + " ⊑ " + strings.Trim(m[2], cutset)

		r := runtime.New()

		t.Run(strconv.Itoa(i)+"/"+key, func(t *testing.T) {

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
				err = Value(ctx, a, b)
			case subSchema:
				err = API.Value(ctx, a, b)
			// TODO: see comments above.
			// case subNoOptional:
			// 	err = IgnoreOptional.Value(ctx, a, b)
			case subDefaults:
				p := Profile{Defaults: true}
				err = p.Value(ctx, a, b)
			case subFinal:
				err = Final.Value(ctx, a, b)
			}
			got := err == nil

			if got != tc.subsumes {
				t.Errorf("got %v; want %v (%v vs %v)", got, tc.subsumes, a.Kind(), b.Kind())
			}
		})
	}
}
