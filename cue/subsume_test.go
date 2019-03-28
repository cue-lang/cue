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
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestSubsume(t *testing.T) {
	testCases := []struct {
		// the result of b ⊑ a, where a and b are defined in "in"
		subsumes bool
		in       string
		mode     subsumeMode
	}{
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
		47: {subsumes: true, in: `a: 1, b: 1.0 `},
		48: {subsumes: true, in: `a: 3, b: 3.0`},
		49: {subsumes: false, in: `a: int, b: 1`},
		50: {subsumes: true, in: `a: int, b: int & 1`},
		51: {subsumes: true, in: `a: float, b: 1.0`},
		52: {subsumes: false, in: `a: float, b: 1`},
		53: {subsumes: false, in: `a: int, b: 1.0`},
		54: {subsumes: true, in: `a: int, b: int`},
		55: {subsumes: true, in: `a: number, b: int`},

		// Lists
		56: {subsumes: true, in: `a: [], b: [] `},
		57: {subsumes: true, in: `a: [1], b: [1] `},
		58: {subsumes: false, in: `a: [1], b: [2] `},
		59: {subsumes: false, in: `a: [1], b: [2, 3] `},
		60: {subsumes: true, in: `a: [{b: string}], b: [{b: "foo"}] `},
		61: {subsumes: true, in: `a: [...{b: string}], b: [{b: "foo"}] `},
		62: {subsumes: false, in: `a: [{b: "foo"}], b: [{b: string}] `},
		63: {subsumes: false, in: `a: [{b: string}], b: [{b: "foo"}, ...{b: "foo"}] `},

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

		// Structural
		94: {subsumes: false, in: `a: int + int, b: int`},
		95: {subsumes: true, in: `a: int + int, b: int + int`},
		96: {subsumes: true, in: `a: int + number, b: int + int`},
		97: {subsumes: true, in: `a: number + number, b: int + int`},
		// TODO: allow subsumption of unevaluated values?
		// TODO: may be false if we allow arithmetic on incomplete values.
		98: {subsumes: false, in: `a: int + int, b: int * int`},

		99:  {subsumes: true, in: `a: !int, b: !int`},
		100: {subsumes: true, in: `a: !number, b: !int`},
		// TODO: allow subsumption of unevaluated values?
		// true because both evaluate to bottom
		101: {subsumes: true, in: `a: !int, b: !number`},
		// TODO: allow subsumption of unevaluated values?
		// May be true because both evaluate to bottom. false is always allowed.
		102: {subsumes: false, in: `a: int + int, b: !number`},
		// TODO: allow subsumption of unevaluated values?
		// true because both evaluate to bool
		103: {subsumes: true, in: `a: !bool, b: bool`},

		// Call
		113: {subsumes: true, in: `
			a: fn(),
			b: fn()`,
		},
		// TODO: allow subsumption of unevaluated values?
		114: {subsumes: true, in: `
			a: len(),
			b: len(1)`,
		},
		115: {subsumes: true, in: `
			a: fn(2)
			b: fn(2)`,
		},
		// TODO: allow subsumption of unevaluated values?
		116: {subsumes: true, in: `
			a: fn(number)
			b: fn(2)`,
		},
		// TODO: allow subsumption of unevaluated values?
		117: {subsumes: true, in: `
			a: fn(2)
			b: fn(number)`,
		},

		// TODO: allow subsumption of unevaluated values?
		// TODO: okay, but why false?
		121: {subsumes: false, in: `a: c + d, b: int, c: int, d: int`},
		// TODO: allow subsumption of unevaluated values?
		122: {subsumes: true, in: `a: {}, b: c & {}, c: {}`},

		// references
		123: {subsumes: true, in: `a: c, b: c, c: {}`},
		// TODO: allow subsumption of unevaluated values?
		124: {subsumes: true, in: `a: c, b: d, c: {}, d: {}`},
		125: {subsumes: false, in: `a: c, b: d, c: {a:1}, d: {}`},
		// TODO: allow subsumption of unevaluated values?
		126: {subsumes: true, in: `a: c, b: d, c: {a:1}, d: c & {b:1}`},
		127: {subsumes: false, in: `a: d, b: c, c: {a:1}, d: c & {b:1}`},
		128: {subsumes: false, in: `a: c.c, b: c, c: { d: number}`},

		// type unification catches a reference error.
		129: {subsumes: false, in: `a: c, b: d, c: 1, d: 2`},

		130: {subsumes: true, in: ` a: [1][1], b: [1][1]`},
		131: {subsumes: true, in: ` a: [1][number], b: [1][1]`},
		132: {subsumes: true, in: ` a: [number][1], b: [1][1]`},
		133: {subsumes: true, in: ` a: [number][number], b: [1][1]`},
		134: {subsumes: false, in: ` a: [1][0], b: [1][number]`},
		135: {subsumes: false, in: ` a: [1][0], b: [number][0]`},
		136: {subsumes: true, in: ` a: [number][number], b: [1][number]`},
		137: {subsumes: true, in: ` a: [number][number], b: [number][1]`},
		// purely structural:
		138: {subsumes: false, in: ` a: [number][number], b: number`},

		// interpolations
		139: {subsumes: true, in: ` a: "\(d)", b: "\(d)", d: _`},
		// TODO: allow subsumption of unevaluated values?
		140: {subsumes: true, in: ` a: "\(d)", b: "\(e)", d: _, e: _`},

		141: {subsumes: true, in: ` a: "\(string)", b: "\("foo")"`},
		// TODO: allow subsumption of unevaluated values?
		142: {subsumes: true, in: ` a: "\(string)", b: "\(d)", d: "foo"`},
		143: {subsumes: true, in: ` a: "\("foo")", b: "\("foo")"`},
		144: {subsumes: false, in: ` a: "\("foo")", b: "\(1) \(2)"`},

		145: {subsumes: false, in: ` a: "s \(d) e", b: "s a e", d: _`},
		146: {subsumes: false, in: ` a: "s \(d)m\(d) e", b: "s a e", d: _`},

		147: {subsumes: true, in: ` a: 7080, b: *7080 | int`, mode: subChoose},

		// Defaults
		150: {subsumes: false, in: `a: number | *1, b: number | *2`},
		151: {subsumes: true, in: `a: number | *2, b: number | *2`},
		152: {subsumes: true, in: `a: int | *float, b: int | *2.0`},
		153: {subsumes: true, in: `a: int | *2, b: int | *2.0`},
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
		420: {subsumes: true, in: `a: {foo?: _}, b: {}`},
	}

	re := regexp.MustCompile(`a: (.*).*b: ([^\n]*)`)
	for i, tc := range testCases {
		if tc.in == "" {
			continue
		}
		m := re.FindStringSubmatch(strings.Join(strings.Split(tc.in, "\n"), ""))
		const cutset = "\n ,"
		key := strings.Trim(m[1], cutset) + " ⊑ " + strings.Trim(m[2], cutset)

		t.Run(strconv.Itoa(i)+"/"+key, func(t *testing.T) {
			ctx, root := compileFile(t, tc.in)

			// Use low-level lookup to avoid evaluation.
			var a, b value
			for _, arc := range root.arcs {
				switch arc.feature {
				case ctx.strLabel("a"):
					a = arc.v
				case ctx.strLabel("b"):
					b = arc.v
				}
			}
			if got := subsumes(ctx, a, b, tc.mode); got != tc.subsumes {
				t.Errorf("got %v; want %v (%v vs %v)", got, tc.subsumes, a.kind(), b.kind())
			}
		})
	}
}

func TestTouchBottom(t *testing.T) {
	// Just call this function to mark coverage. It is otherwise never called.
	var x bottom
	x.subsumesImpl(nil, &bottom{}, 0)
}
