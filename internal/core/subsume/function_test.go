// Copyright 2026 CUE Authors
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
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/subsume"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetest"
)

// TestFunctions tests subsumption of native function values and function
// types (bodyless signatures). Subsumption is structural, following the
// signature matching rules of function tightening and type meets:
// positional parameters align by ordinal, their contract labels agree when
// both are present, and name-only parameters match by label;
// requiredness is ordered as for struct fields, and extras of the subsumed signature are
// admitted only against an open subsuming signature. Each matched
// parameter constraint and the result constraint of the subsumer must
// subsume the corresponding constraint of the subsumed, and every plain
// label promised by the subsumer must already select the matched slot.
func TestFunctions(t *testing.T) {
	type subsumeTest struct {
		// the result of b ⊑ a, where a and b are defined in "in"
		err string
		in  string
	}
	const exp = "@experiment(functions)\n"
	testCases := []subsumeTest{
		// Type ⊑ type: open and closed signatures.
		{
			in:  `a: func(n: int, ...) -> int, b: func(n: int, m: int, ...) -> int`,
			err: "",
		},
		{
			// A closed signature does not admit an extra plain parameter.
			in:  `a: func(n: int) -> int, b: func(n: int, m: int) -> int`,
			err: "value not an instance",
		},
		{
			// An open type may acquire future parameters and is not an
			// instance of an otherwise matching closed type.
			in:  `a: func(n: int) -> int, b: func(n: int, ...) -> int`,
			err: "value not an instance",
		},
		{
			// A composite type is effectively closed when any retained
			// signature is closed.
			in:  `a: func(n: int) -> int, b: (func(n: int, ...) -> int) & (func(n: int) -> int)`,
			err: "",
		},
		{
			// Extra declarations in a retained signature count even when the
			// composite type's selected head does not declare them.
			in:  `a: func(n: int) -> int, b: (func(n: int, ...) -> int) & (func(n: int, m: int, ...) -> int)`,
			err: "value not an instance",
		},
		{
			// The same result holds when the retained signatures are met in
			// the opposite order.
			in:  `a: func(n: int) -> int, b: (func(n: int, m: int, ...) -> int) & (func(n: int, ...) -> int)`,
			err: "value not an instance",
		},
		{
			// A composite bodyless type is checked independently of which
			// constituent becomes its selected head. The optional name-only x
			// is not a second concrete slot in either operand order.
			in:  `a: func(x: int, ...) -> int, b: (func(int, x?: int, ...) -> int) & (func(x: int, ...) -> int)`,
			err: "value not an instance",
		},
		{
			in:  `a: func(x: int, ...) -> int, b: (func(x: int, ...) -> int) & (func(int, x?: int, ...) -> int)`,
			err: "value not an instance",
		},
		{
			// A weaker selected head cannot hide an incompatible constraint
			// contributed by another signature.
			in:  `a: func(x?: int, ...) -> int, b: (func(...) -> int) & (func(x: string, ...) -> int)`,
			err: "value not an instance",
		},
		{
			in:  `a: func(x?: int, ...) -> int, b: (func(x: string, ...) -> int) & (func(...) -> int)`,
			err: "value not an instance",
		},
		{
			// An optional extra parameter is not needed for a call to
			// succeed and is admitted even by a closed signature.
			in:  `a: func(n: int) -> int, b: func(n: int, m?: int) -> int`,
			err: "",
		},
		{
			// b must declare every parameter a declares.
			in:  `a: func(n: int, m: int, ...) -> int, b: func(n: int) -> int`,
			err: "value not an instance",
		},
		{
			// The same holds when a is closed.
			in:  `a: func(n: int, m: int) -> int, b: func(n: int) -> int`,
			err: "value not an instance",
		},
		{
			// An extra optional parameter of a is not needed for a call to
			// succeed and does not prevent subsumption of a signature that
			// lacks it: an extra parameter is admitted against a closed
			// signature iff it is optional, mirroring the tightening and
			// type-meet rules.
			in:  `a: func(n: int, m?: int) -> int, b: func(n: int) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int, m?: int, ...) -> int, b: func(n: int) -> int`,
			err: "",
		},
		{
			// The optional-extra rule also admits a function value that does
			// not declare the optional parameter.
			in:  `a: func(n: int, m?: int) -> int, b: func(n: int) -> int: n`,
			err: "",
		},
		{
			in:  `a: func(n: int) -> int, b: func(n: int) -> int`,
			err: "",
		},

		// Declared defaults. An extra defaulted parameter is admitted like an
		// optional one, in both directions, and the value of a default never
		// participates. For a matched parameter, omittability is part of the
		// contract: b ⊑ a holds when adding a default (b may be omitted
		// where a could not) and fails when removing one.
		{
			in:  `a: func(n: int) -> int, b: func(n: int, m: int = 1) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int, m: int = 1) -> int, b: func(n: int) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int, m: int = 1) -> int, b: func(n: int) -> int: n`,
			err: "",
		},
		{
			in:  `a: func(n: int) -> int, b: func(n: int = 1) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int = 1) -> int, b: func(n: int) -> int`,
			err: "value not an instance",
		},
		{
			in:  `a: func(n: int = 1) -> int, b: func(n: int = 2) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int = 1) -> int, b: func(n: int = 1) -> int: n`,
			err: "",
		},
		{
			// A default declared by a signature attached to b makes b's
			// parameter omittable, for a matched parameter ...
			in:  `a: func(n: int = 1) -> int, b: (func(n: int) -> int: n) & (func(n: int = 2) -> int)`,
			err: "",
		},
		{
			// ... but not for an extra one against a closed signature:
			// admission beyond a closed type depends on the value's own
			// declaration, as it does for unification, where a & b is
			// bottom whatever the order of the attachments.
			in:  `a: func(n: int) -> int, b: (func(n: int, m: int) -> int: n) & (func(n: int, m: int = 1) -> int)`,
			err: "value not an instance",
		},

		// Parameter widening and narrowing in both directions.
		{
			in:  `a: func(n: number, ...) -> int, b: func(n: int, ...) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int, ...) -> int, b: func(n: number, ...) -> int`,
			err: "value not an instance",
		},
		{
			in:  `a: func(n: <10, ...) -> int, b: func(n: <5, ...) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: <5, ...) -> int, b: func(n: <10, ...) -> int`,
			err: "value not an instance",
		},

		// Result widening and narrowing in both directions.
		{
			in:  `a: func(n: int, ...) -> number, b: func(n: int, ...) -> int`,
			err: "",
		},
		{
			in:  `a: func(n: int, ...) -> int, b: func(n: int, ...) -> number`,
			err: "value not an instance",
		},

		// Requiredness is ordered as for struct fields: optional subsumes
		// required, which subsumes plain. A required or optional parameter
		// matches a positional one by label.
		{
			in:  `a: func(n!: int, ...) -> int, b: func(n: int, ...) -> int`,
			err: "",
		},
		{
			in:  `a: func(n?: int) -> int, b: func(n!: int) -> int`,
			err: "",
		},
		{
			in:  `a: func(n!: int) -> int, b: func(n?: int) -> int`,
			err: "value not an instance",
		},
		{
			in:  `a: func(n?: int) -> int, b: func(n!: int = 1) -> int`,
			err: "",
		},
		{
			in:  `a: func(n!: int) -> int, b: func(n: int) -> int: n`,
			err: "",
		},
		{
			// A signature unified with b makes its optional parameter
			// required, so b is an instance of a signature requiring it.
			in:  `a: func(n!: int) -> int, b: (func(n?: int) -> int: 1) & (func(n!: int) -> int)`,
			err: "",
		},
		{
			in:  `a: func(n: int, ...) -> int, b: func(n!: int, ...) -> int`,
			err: "value not an instance",
		},
		{
			in:  `a: func(n!: int, ...) -> int, b: func(n!: int, ...) -> int`,
			err: "",
		},

		// Positional parameters align by ordinal and name-only parameters match
		// by label. A plain contract label is an additional requirement.
		{
			in:  `a: func(int, ...) -> int, b: func(n: int, ...) -> int`,
			err: "",
		},
		{
			in:  `a: func(int, ...) -> int, b: func(int, ...) -> int`,
			err: "",
		},
		{
			// A named parameter is positional too, so an anonymous parameter
			// of b matches it by position, but b does not satisfy the complete
			// callable interface until that name has actually been attached.
			in:  `a: func(n: int, ...) -> int, b: func(int, ...) -> int`,
			err: "value not an instance",
		},
		{
			// Different contract labels conflict during unification and also fail
			// the directional callable-interface check for subsumption.
			in:  `a: func(n: int, ...) -> int, b: func(other: int, ...) -> int`,
			err: "value not an instance",
		},
		{
			// Attaching the type names the previously unnamed slot, after which
			// the tightened value satisfies the complete interface.
			in:  `T: func(n: int, ...) -> int, f: func(_~v: int) -> int: v, a: T, b: T & f`,
			err: "",
		},
		{
			// A name-only constraint follows a sibling contract label when
			// deciding whether the candidate satisfies the type.
			in:  `a: func(x?: int, ...) -> string, b: (func(x: string, ...) -> string) & (func(_~v: string) -> string: v)`,
			err: "value not an instance",
		},
		{
			// A name-only parameter of a can only be bound by label, which
			// an anonymous parameter of b cannot satisfy.
			in:  `a: func(int, m!: int, ...) -> int, b: func(int, int, ...) -> int`,
			err: "value not an instance",
		},

		// A function value subsumes itself, but not a distinct value with
		// an identical signature.
		{
			in:  `f: func(n: int) -> int: n, a: f, b: f`,
			err: "",
		},
		{
			in:  `f: func(n: int) -> int: n, g: func(n: int) -> int: n, a: f, b: g`,
			err: "value not an instance",
		},

		// A function value subsumes a composition of itself with another
		// function, but not the other way around. Compositions of the same
		// functions subsume each other regardless of order.
		{
			in:  `f: func(n: int) -> int: n, g: func(n: int) -> int: n, a: f, b: f & g`,
			err: "",
		},
		{
			in:  `f: func(n: int) -> int: n, g: func(n: int) -> int: n, a: g, b: f & g`,
			err: "",
		},
		{
			in:  `f: func(n: int) -> int: n, g: func(n: int) -> int: n, a: f & g, b: f`,
			err: "value not an instance",
		},
		{
			in:  `f: func(n: int) -> int: n, g: func(n: int) -> int: n, a: f & g, b: g & f`,
			err: "",
		},
		{
			// A type admits a composition that satisfies it.
			in:  `f: func(n: int) -> int: n, g: func(n: int) -> int: n, a: func(n: int) -> int, b: f & g`,
			err: "",
		},

		// A function type subsumes a function value whose signature
		// satisfies it, including a value it tightened; a value subsumes a
		// tightening of itself, but a tightened value does not subsume the
		// plain value.
		{
			in:  `a: func(n: int, ...) -> int, b: func(n: int) -> int: n`,
			err: "",
		},
		{
			in:  `T: func(n: int, ...) -> int, f: func(n: int) -> int: n, a: T, b: T & f`,
			err: "",
		},
		{
			in:  `T: func(n: int, ...) -> int, f: func(n: int) -> int: n, a: f, b: T & f`,
			err: "",
		},
		{
			in:  `T: func(n: <10, ...) -> int, f: func(n: int) -> int: n, a: T & f, b: f`,
			err: "value not an instance",
		},
		{
			// A value does not subsume a type.
			in:  `f: func(n: int) -> int: n, a: f, b: func(n: int, ...) -> int`,
			err: "value not an instance",
		},

		// A partial application subsumes itself, but distinct partial
		// applications differ on every call and never subsume one another.
		// Bound arguments are compared conservatively by identity, so even
		// two separately written applications of the same argument are not
		// treated as equal.
		{
			in:  `f: func(n: int, m: int) -> int: n+m, g: f(5, ...), a: g, b: g`,
			err: "",
		},
		{
			in:  `f: func(n: int, m: int) -> int: n+m, a: f(5, ...), b: f(10, ...)`,
			err: "value not an instance",
		},
		{
			in:  `f: func(n: int, m: int) -> int: n+m, a: f(5, ...), b: f(5, ...)`,
			err: "value not an instance",
		},
		{
			// A partial application no longer exposes the complete callable
			// interface of the type attached before parameters were bound.
			in:  `T: func(a: int, b: int) -> int, f: func(a: int, b: int) -> int: a+b, a: T, b: (T & f)(1, ...)`,
			err: "value not an instance",
		},
		{
			// A plain function value does not subsume a partial application
			// of itself, nor the other way around.
			in:  `f: func(n: int, m: int) -> int: n+m, a: f, b: f(5, ...)`,
			err: "value not an instance",
		},
		{
			in:  `f: func(n: int, m: int) -> int: n+m, a: f(5, ...), b: f`,
			err: "value not an instance",
		},

		// A builtin subsumes itself and a tightening of itself; the
		// tightened builtin does not subsume the plain one.
		{
			in:  "import \"strings\"\na: strings.ToUpper, b: strings.ToUpper",
			err: "",
		},
		{
			in:  "import \"strings\"\nT: func(string, ...) -> string, a: strings.ToUpper, b: T & strings.ToUpper",
			err: "",
		},
		{
			in:  "import \"strings\"\nT: func(string, ...) -> string, a: T & strings.ToUpper, b: strings.ToUpper",
			err: "value not an instance",
		},
		{
			// Distinct builtins do not subsume each other.
			in:  "import \"strings\"\na: strings.ToUpper, b: strings.ToLower",
			err: "value not an instance",
		},

		// A function type subsumes a builtin that satisfies it, including a
		// builtin it tightened, applying the same static signature check
		// used for tightening. A builtin does not subsume a function type.
		{
			in:  "import \"strings\"\na: func(string, ...) -> string, b: strings.ToUpper",
			err: "",
		},
		{
			in:  "import \"strings\"\na: func(string) -> string, b: strings.ToUpper",
			err: "",
		},
		{
			in:  "import \"strings\"\nT: func(string, ...) -> string, a: T, b: T & strings.ToUpper",
			err: "",
		},
		{
			// A parameter kind conflicting with the builtin's is rejected.
			in:  "import \"strings\"\na: func(int, ...) -> string, b: strings.ToUpper",
			err: "value not an instance",
		},
		{
			// So is a conflicting result kind.
			in:  "import \"strings\"\na: func(string, ...) -> int, b: strings.ToUpper",
			err: "value not an instance",
		},
		{
			// The builtin package's attached signature exposes the same contract
			// label for this raw positional slot.
			in:  "import \"strings\"\na: func(s: string, ...) -> string, b: strings.ToUpper",
			err: "",
		},
		{
			// A different contract name conflicts with the builtin package's
			// attached signature.
			in:  "import \"strings\"\na: func(input: string, ...) -> string, b: strings.ToUpper",
			err: "value not an instance",
		},
		{
			// Static kind compatibility does not prove that a bare builtin
			// enforces a narrower value-level constraint.
			in:  "import \"strings\"\na: func(s: =~\"^x$\", ...) -> string, b: strings.ToUpper",
			err: "value not an instance",
		},
		{
			// Once the narrowing signature is attached, the builtin enforces
			// it and satisfies the type by construction.
			in:  "import \"strings\"\nT: func(s: =~\"^x$\", ...) -> string, a: T, b: T & strings.ToUpper",
			err: "",
		},
		{
			// An optional name-only constraint follows a sibling contract label.
			// It cannot be ignored when that label is exposed by b.
			in:  "import \"strings\"\na: func(s?: int, ...) -> string, b: (func(s: string, ...) -> string) & strings.ToUpper",
			err: "value not an instance",
		},
		{
			// A parameter that can only be passed by label cannot be
			// satisfied by a builtin, which binds its arguments by position.
			in:  "import \"strings\"\na: func(string, sep!: string, ...) -> string, b: strings.ToUpper",
			err: "value not an instance",
		},
		{
			in:  "import \"strings\"\na: strings.ToUpper, b: func(string, ...) -> string",
			err: "value not an instance",
		},

		// Functions and non-functions do not subsume each other, except
		// through top.
		{
			in:  `a: func(n: int, ...) -> int, b: 3`,
			err: "value not an instance",
		},
		{
			in:  `f: func(n: int) -> int: n, a: 3, b: f`,
			err: "value not an instance",
		},
		{
			in:  `f: func(n: int) -> int: n, a: _, b: f`,
			err: "",
		},
	}

	cuetdtest.Run(t, testCases, func(t *cuetdtest.T, tc *subsumeTest) {
		t.Update(cuetest.UpdateGoldenFiles())

		// Log descriptive name for debugging
		t.Log(tc.in)

		r := t.M.Runtime()

		file, err := parser.ParseFile("subsume", exp+tc.in)
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

		err = subsume.Value(ctx, a, b)

		var gotErr string
		if err != nil {
			gotErr = strings.TrimSpace(errors.Details(err, nil))
			if strings.Contains(gotErr, "\n") {
				gotErr = "\n" + gotErr
				gotErr = strings.Replace(gotErr, "\n", "\n\t\t\t\t", -1)
			}
		}

		t.Equal(gotErr, tc.err)
	})
}
