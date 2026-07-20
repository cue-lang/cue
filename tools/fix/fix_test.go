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

package fix

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

func TestFile(t *testing.T) {
	testCases := []struct {
		name     string
		in       string
		out      string
		simplify bool
		exps     []string
	}{
		{
			name:     "simplify literal tops",
			simplify: true,
			in: `
x1: 3 & _
x2: _ | {[string]: int}
x3: 4 & (9 | _)
x4: (_ | 9) & 4
x5: (_ & 9) & 4
x6: 4 & (_ & 9)
`,
			out: `x1: 3
x2: _
x3: 4
x4: 4
x5: 9 & 4
x6: 4 & 9
`,
		},

		{
			name: "rewrite list addition",
			in: `a: [7]
b: a + a
c: a + [8]
d: [9] + a
e: [0] + [1]
f: [0] + [1] + [2]
g: list.Concat([[0], [1, 2]]) + list.Concat([[3, 4], [5]])
h: list.Concat([[0], [1, 2]]) + [3] + [4] + list.Concat([[5, 6], [7]])
i: list.Concat(list.Concat([[0], [1, 2]]), list.Concat([[3, 4], [5]]))
`,
			out: `import "list"

a: [7]
b: a + a
c: list.Concat([a, [8]])
d: list.Concat([[9], a])
e: list.Concat([[0], [1]])
f: list.Concat([[0], [1], [2]])
g: list.Concat([[0], [1, 2], [3, 4], [5]])
h: list.Concat([[0], [1, 2], [3], [4], [5, 6], [7]])
i: list.Concat(list.Concat([[0], [1, 2]]), list.Concat([[3, 4], [5]]))
`,
		},

		{
			name: "rewrite list multiplication",
			in: `a: [7]
b: a * 3
c: 4
d: [7] * c
e: c * [8]
f: [9] * 5
g: ([9] * 5) + (6 * [10])
`,
			out: `import "list"

a: [7]
b: a * 3
c: 4
d: list.Repeat([7], c)
e: list.Repeat([8], c)
f: list.Repeat([9], 5)
g: (list.Repeat([9], 5)) + (list.Repeat([10], 6))
`,
		},

		{
			name: "add ellipsis to embeddings (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: a: int
#B: b: int

X: {
	#A // foo

	// bar
	#B
	b: string
}
`,
			out: `@experiment(explicitopen)

package foo

#A: a: int
#B: b: int

X: __closeAll({
	#A... // foo

	// bar
	#B...
	b: string
})
`,
		},

		{
			// Embeddings nested inside a rewritten embedding, such as in
			// the struct operand of a conjunction, must be rewritten too.
			name: "nested embeddings inside conjunction operands (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#X: ctx: {...}
ref: {a: int}

v: {
	#X & {
		ctx: {
			ref
			extra: 1
		}
	}
	more: 2
}
`,
			out: `@experiment(explicitopen)

package foo

#X: ctx: {...}
ref: {a: int}

v: __closeAll({
	(#X & {
		ctx: __reclose({
			ref...
			extra: 1
		})
	})...
	more: 2
})
`,
		},

		{
			// Under the old semantics, conjuncts inserted through
			// comprehensions are treated like embeddings and do not close
			// their fields. Field values that may resolve to closed
			// structs must be opened to preserve that behavior.
			name: "open field values in comprehensions (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#HC: hc: {port: 1}

#Service: {
	enable: bool
	egress?: [string]: {...}
	if enable {
		egress: #HC
	}
}
`,
			out: `@experiment(explicitopen)

package foo

#HC: hc: {port: 1}

#Service: {
	enable: bool
	egress?: [string]: {...}
	if enable {
		egress: #HC...
	}
}
`,
		},

		{
			// Selectors may resolve to closed values just like plain
			// references, so a conjunction with a selector operand needs
			// a runtime __reclose check on the enclosing struct.
			name: "reclose embeddings of selector conjunctions (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: a: int
h: inner: #A

v: {
	h.inner & {a: 1}
	extra: 2
}
`,
			out: `@experiment(explicitopen)

package foo

#A: a:    int
h: inner: #A

v: __reclose({
	(h.inner & {a: 1})...
	extra: 2
})
`,
		},

		{
			// Comprehension field values that may resolve to closed
			// structs via a selector conjunction or an and() call must
			// be opened like plain references.
			name: "open selector and call field values in comprehensions (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#HC: hc: {port: 1}
lib: v: #HC

#Service: {
	enable: bool
	egress?: [string]: {...}
	if enable {
		egress: lib.v & {hc: port: 1}
	}
	if enable {
		egress2: and([lib.v])
	}
}
`,
			out: `@experiment(explicitopen)

package foo

#HC: hc: {port: 1}
lib: v:  #HC

#Service: {
	enable: bool
	egress?: [string]: {...}
	if enable {
		egress: (lib.v & {hc: port: 1})...
	}
	if enable {
		egress2: and([lib.v])...
	}
}
`,
		},

		{
			// The default marker *X takes on X's closedness: a disjunction
			// with a defaulted definition operand needs a runtime __reclose
			// check when embedded, and must be opened as a comprehension
			// field value.
			name: "default marker embedding flags (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: {a: int}

v: {
	*#A | {}
	extra: 1
}

#S: {
	enable: bool
	x?: {...}
	if enable {
		x: *#A | {}
	}
}
`,
			out: `@experiment(explicitopen)

package foo

#A: {a: int}

v: __reclose({
	(*#A | {})...
	extra: 1
})

#S: {
	enable: bool
	x?:     {...}
	if enable {
		x: (*#A | {})...
	}
}
`,
		},

		{
			// The old comprehension opening also overrides an explicit
			// close() in a field value: sibling entries added elsewhere
			// remain allowed.
			name: "open close() field values in comprehensions (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#S: {
	enable: bool
	egress?: [string]: {...}
	if enable {
		egress: close({hc: {p: 1}})
	}
}
`,
			out: `@experiment(explicitopen)

package foo

#S: {
	enable: bool
	egress?: [string]: {...}
	if enable {
		egress: close({hc: {p: 1}})...
	}
}
`,
		},

		{
			// Embeddings inside comprehension values must not add a
			// wrapper to the enclosing struct: under the old semantics
			// they close the comprehension result only when the guard
			// fires, while a wrapper closes the enclosing struct
			// unconditionally, and does not even evaluate when the guard
			// references a field of the wrapped struct. The conditional
			// closing cannot be expressed at all — builtin wrappers
			// evaluate their argument without the enclosing struct's
			// fields, and an unwrapped strict embedding denies the
			// enclosing literal's own fields — so the embeddings are
			// simply opened, with a TODO comment flagging the
			// (permissive) semantic difference.
			name: "embeddings inside comprehension values (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: {a: int}
inst: #A

x: {
	c: bool
	if c {
		#A
	}
}

y: {
	c: bool
	if c {
		close({a: 1})
	}
}

z: {
	c: bool
	if c {
		inst
		extra: 1
	}
}
`,
			out: `@experiment(explicitopen)

package foo

#A:   {a: int}
inst: #A

x: {
	c: bool
	if c {
		// TODO(cue-fix): the old semantics closed the enclosing struct when the comprehension fired; this is no longer the case.
		#A...
	}
}

y: {
	c: bool
	if c {
		// TODO(cue-fix): the old semantics closed the enclosing struct when the comprehension fired; this is no longer the case.
		{a: 1}
	}
}

z: {
	c: bool
	if c {
		// TODO(cue-fix): the old semantics closed the enclosing struct when the comprehension fired; this is no longer the case.
		inst...
		extra: 1
	}
}
`,
		},

		{
			// Struct literal field values inside comprehensions must not
			// be wrapped: conjuncts inserted through comprehensions did
			// not close under the old semantics, so a wrapper would deny
			// fields that the old semantics allowed. The embeddings inside
			// the literal are still opened.
			name: "no wrappers on struct literal field values in comprehensions (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: {a: int}

#S: {
	enable: bool
	egress?: {...}
	if enable {
		egress: {#A, extra: 1}
	}
}
`,
			out: `@experiment(explicitopen)

package foo

#A: {a: int}

#S: {
	enable:  bool
	egress?: {...}
	if enable {
		egress: {#A..., extra: 1}
	}
}
`,
		},

		{
			// A hoisted close() must keep closing the struct when it is
			// the only element: under the old semantics {close(X)} is
			// equivalent to close(X).
			name: "keep closing of single close() embeds (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

o: {b: int}

s1: {close({c: 3})}
s2: {close(o)}
`,
			out: `@experiment(explicitopen)

package foo

o: {b: int}

s1: close({c: 3})
s2: close(__reclose({o...}))
`,
		},

		{
			// Embedding flags must not leak between sibling struct
			// literals in one scope: each literal's wrapper is decided by
			// its own embeddings. A literal embedded directly in another
			// still carries its flags to the enclosing literal.
			// TODO: the def flag from the first operand upgrades the
			// second operand's __reclose to __closeAll, unconditionally
			// closing a struct whose own embedding only warranted a
			// runtime check.
			name: "sibling struct literals in a conjunction (fixExplicitOpen)",
			exps: []string{"explicitopen"},
			in: `package foo

#A: {a: int, d: int}
o: {}

v: {#A} & {o, d: 2}

w: {
	{#A}
	extra: 1
}
`,
			out: `@experiment(explicitopen)

package foo

#A: {a: int, d: int}
o:  {}

v: {#A} & __closeAll({o..., d: 2})

w: __closeAll({
	{#A...}
	extra: 1
})
`,
		},

		{
			// Blank aliases bind nothing that can be referenced; they must be
			// dropped rather than converted to blank postfix aliases, which
			// Sanitize rejects, or to an invalid "let _ = self".
			name: "aliasv2 blank aliases",
			exps: []string{"aliasv2"},
			in: `package foo

obj: {[_=string]: int}
val: _={a: 1}
_=lbl: int
mixed: X=[_=string]: {n: X.a}
`,
			out: `@experiment(aliasv2)

package foo

obj: {[string]: int}
val: {a: 1}
lbl: int
mixed: [string]~(X): {n: X.a}
`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile("", tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			var opts []Option
			if tc.simplify {
				opts = append(opts, Simplify())
			}
			if len(tc.exps) > 0 {
				opts = append(opts, Experiments(tc.exps...))
			}
			File(f, opts...)

			b, err := format.Node(f)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}
			// Skip parsing validation for tests that use experiments that create
			// syntax that requires the same experiments to parse
			if len(tc.exps) == 0 {
				_, err = parser.ParseFile("rewritten", got, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestFixAliasV2InvalidLabelAlias checks that fixAliasV2 terminates on a
// label alias whose Expr is not a valid label. The parser cannot produce
// such an AST, but the public API accepts arbitrary ones; fixAliasV2 used
// to loop forever since the alias was flagged as a change but never
// rewritten.
func TestFixAliasV2InvalidLabelAlias(t *testing.T) {
	field := &ast.Field{
		Label: &ast.Alias{
			Ident: ast.NewIdent("X"),
			Expr:  ast.NewBinExpr(token.ADD, ast.NewIdent("a"), ast.NewIdent("b")),
		},
		Value: ast.NewIdent("int"),
	}
	f := &ast.File{Decls: []ast.Decl{field}}

	result, hasChanges := fixAliasV2(f)
	if hasChanges {
		t.Errorf("expected no changes for label alias with non-label Expr")
	}
	if _, ok := result.Decls[0].(*ast.Field).Label.(*ast.Alias); !ok {
		t.Errorf("expected label alias to be left untouched")
	}
}

// TestX is for debugging; DO NOT REMOVE.
func TestX(t *testing.T) {
	t.Skip("for debugging")

	astFile, parseErr := parser.ParseFile("", `
	#A: a: int
	X: {
		#A
	}
	`, parser.ParseComments)
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}

	file(astFile, "v0.15.0", Experiments("explicitopen"))

	out, fmtErr := format.Node(astFile)
	if fmtErr != nil {
		t.Fatalf("format: %v", fmtErr)
	}
	t.Error(string(out))
}
