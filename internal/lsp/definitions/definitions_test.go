package definitions_test

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/definitions"
	"github.com/go-quicktest/qt"
)

type testCase struct {
	name         string
	prog         string
	expectations map[*position][]*position
}

type testCases []testCase

func (tcs testCases) run(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := parser.ParseFile(tc.name, tc.prog, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))
			dfns := definitions.Analyse(ast)
			resolutions := dfns.ForFile(tc.name)
			t.Log(resolutions)

			allPositions := make(map[*position]struct{})
			for from, tos := range tc.expectations {
				allPositions[from] = struct{}{}
				for _, to := range tos {
					allPositions[to] = struct{}{}
				}
			}
			file := ast.Pos().File()
			for pos := range allPositions {
				pos.determineOffset(file, tc.prog)
			}

			// It's deliberate that we allow the expectations to be a
			// subset of what is resolved. However, for each expectation,
			// the resolutions must match exactly (reordering permitted).

			for posFrom, positionsWant := range tc.expectations {
				if positionsWant == nil {
					continue
				}
				offset := posFrom.offset
				qt.Assert(t, qt.IsTrue(len(resolutions) > offset))
				for _, pos := range positionsWant {
					if pos.filename == "" {
						pos.filename = tc.name
					}
				}

				positionsGot := resolutions[offset]
				qt.Assert(t, qt.Equals(len(positionsGot), len(positionsWant)))
				slices.SortFunc(positionsWant, cmpPositions)
				slices.SortFunc(positionsGot, cmpTokenPositions)
				for i, want := range positionsWant {
					got := positionsGot[i]
					if !(got.Filename == want.filename && got.Offset == want.offset) {
						t.Fatalf("Got %v; Want %s:%d", got, want.filename, want.offset)
					}
				}
			}
		})
	}
}

func cmpTokenPositions(a, b token.Position) int {
	// Deliberately avoid using the offset field as it may not be specified
	if c := cmp.Compare(a.Filename, b.Filename); c != 0 {
		return c
	}
	return cmp.Compare(a.Offset, b.Offset)
}

func cmpPositions(a, b *position) int {
	// Deliberately avoid using the offset field as it may not be specified
	if c := cmp.Compare(a.filename, b.filename); c != 0 {
		return c
	}
	return cmp.Compare(a.offset, b.offset)
}

type position struct {
	filename string
	line     int
	n        int
	str      string
	offset   int
}

// Convenience constructor to make a new [position] with the given
// line number (1-based), for the n-th (1-based) occurrence of str.
func ln(i, n int, str string) *position {
	return &position{
		line: i,
		n:    n,
		str:  str,
	}
}

func (p *position) determineOffset(file *token.File, prog string) {
	// lines is the cumulative offset of the start of each line
	lines := file.Lines()
	startOffset := lines[p.line-1]
	endOffset := file.Size()
	if len(lines) > p.line {
		endOffset = lines[p.line]
	}
	line := prog[startOffset:endOffset]
	n := p.n
	for i := range line {
		if strings.HasPrefix(line[i:], p.str) {
			n--
			if n == 0 {
				p.offset = startOffset + i
				return
			}
		}
	}
	panic("Failed to determine offset")
}

func TestSimple(t *testing.T) {
	testCases{
		{
			name: "pointer chasing - implicit",
			prog: `x1: f: 3
x2: f: 4
y: x1
y: x2
z: y
out1: z
out2: z.f
`,
			expectations: map[*position][]*position{
				ln(3, 1, "x1"): {ln(1, 1, "x1")},
				ln(4, 1, "x2"): {ln(2, 1, "x2")},
				ln(5, 1, "y"):  {ln(3, 1, "y"), ln(4, 1, "y")},
				ln(6, 1, "z"):  {ln(5, 1, "z")},
				ln(7, 1, "z"):  {ln(5, 1, "z")},
				ln(7, 1, "f"):  {ln(1, 1, "f"), ln(2, 1, "f")},
			},
		},

		{
			name: "pointer chasing - explicit",
			prog: `x1: f: 3
x2: f: 4
y: x1 & x2
z: y
out1: z
out2: z.f
`,
			expectations: map[*position][]*position{
				ln(3, 1, "x1"): {ln(1, 1, "x1")},
				ln(3, 1, "x2"): {ln(2, 1, "x2")},
				ln(4, 1, "y"):  {ln(3, 1, "y")},
				ln(5, 1, "z"):  {ln(4, 1, "z")},
				ln(6, 1, "z"):  {ln(4, 1, "z")},
				ln(6, 1, "f"):  {ln(1, 1, "f"), ln(2, 1, "f")},
			},
		},

		{
			name: "embedding",
			prog: `x: y: z: 3
o: { p: 4, x.y }
`,
			expectations: map[*position][]*position{
				ln(2, 1, "x"): {ln(1, 1, "x")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
			},
		},
	}.run(t)
}

func TestInline(t *testing.T) {
	testCases{
		{
			name: "struct selector",
			prog: `a: {in: {x: 5}, out: in}.out.x`,
			expectations: map[*position][]*position{
				ln(1, 2, "in"):  {ln(1, 1, "in")},
				ln(1, 2, "out"): {ln(1, 1, "out")},
				ln(1, 2, "x"):   {ln(1, 1, "x")},
			},
		},
		{
			name: "list index",
			prog: `a: [7, {b: 3}, true][1].b`,
			// We do not attempt any sort of resolution via dynamic
			// indices.
			expectations: map[*position][]*position{
				ln(1, 1, "1"): {},
				ln(1, 2, "b"): {},
			},
		},
		{
			name: "disjunction internal",
			prog: `a: ({b: c, c: 3} | {c: 4}).c`,
			expectations: map[*position][]*position{
				ln(1, 1, "c"): {ln(1, 2, "c")},
				ln(1, 4, "c"): {ln(1, 2, "c"), ln(1, 3, "c")},
			},
		},
	}.run(t)
}

func TestCycles(t *testing.T) {
	testCases{
		{
			name: "cycle - simple 2",
			prog: `a: b
b: a`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "a"): {ln(1, 1, "a")},
			},
		},
		{
			name: "cycle - simple 3",
			prog: `a: b
b: c
c: a`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(2, 1, "c"): {ln(3, 1, "c")},
				ln(3, 1, "a"): {ln(1, 1, "a")},
			},
		},
		// These "structural" cycles are errors in the evaluator. But
		// there's no reason we can't resolve them.
		{
			name: "structural - simple",
			prog: `a: b: c: a`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
			},
		},
		{
			name: "structural - simple - selector",
			prog: `a: b: c: a.b`,
			expectations: map[*position][]*position{
				ln(1, 2, "a"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "structural - complex",
			prog: `y: [string]: b: y
x: y
x: c: x
`,
			expectations: map[*position][]*position{
				ln(1, 2, "y"): {ln(1, 1, "y")},
				ln(2, 1, "y"): {ln(1, 1, "y")},
				ln(3, 2, "x"): {ln(2, 1, "x"), ln(3, 1, "x")},
			},
		},
	}.run(t)
}

func TestAliases(t *testing.T) {
	testCases{
		{
			name: "plain label - internal",
			prog: `l=a: {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "plain label - internal - implicit",
			prog: `l=a: b: 3
a: c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "plain label - internal - implicit - reversed",
			prog: `a: b: 3
l=a: c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(1, 1, "a"), ln(2, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "plain label - external",
			prog: `l=a: b: 3
c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},

		{
			name: "dynamic label - internal",
			prog: `l=(a): {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "(")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "dynamic label - internal - implicit",
			prog: `l=(a): b: 3
(a): c: l.b`,
			// We do not attempt to compute equivalence of
			// expressions. Therefore we don't consider the two `(a)`
			// keys to be the same.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, "(")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "dynamic label - internal - implicit - reversed",
			prog: `(a): b: 3
l=(a): c: l.b`,
			// Because we don't compute equivalence of expressions, we do
			// not link the two `(a)` keys, and so we cannot resolve the
			// b in l.b.
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "(")},
				ln(2, 1, "b"): {},
			},
		},
		{
			name: "dynamic label - external",
			prog: `l=(a): b: 3
c: l.b`,
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {ln(1, 1, ("("))},
				ln(2, 1, "b"): {ln(1, 1, ("b"))},
			},
		},

		{
			name: "pattern label - internal",
			prog: `l=[a]: {b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "[")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "pattern label - internal - implicit",
			prog: `l=[a]: b: 3
[a]: c: l.b`,
			// We do not attempt to compute equivalence of
			// patterns. Therefore we don't consider the two `[a]`
			// patterns to be the same. Because this style of alias is
			// only visible within the key's value, no part of l.b can be
			// resolved.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
			},
		},
		{
			name: "pattern label - internal - implicit - reversed",
			prog: `[a]: b: 3
l=[a]: c: l.b`,
			// Again, the two [a] patterns are not merged. The l of l.b
			// can be resolved, but not the b.
			expectations: map[*position][]*position{
				ln(2, 2, "l"): {ln(2, 1, "[")},
				ln(2, 1, "b"): {},
			},
		},
		{
			name: "pattern label - external",
			prog: `l=[a]: b: 3
c: l.b`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
			},
		},

		{
			name: "pattern expr - internal",
			prog: `[l=a]: {b: 3, c: l, d: l.b}`,
			// This type of alias binds l to the key. So c: l will work,
			// but for the b in d: l.b there is no resolution.
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "l")},
				ln(1, 3, "l"): {ln(1, 1, "l")},
				ln(1, 2, "b"): {},
			},
		},
		{
			name: "pattern expr - internal - implicit",
			prog: `[l=a]: b: 3
[a]: c: l`,
			// We do not attempt to compute equivalence of
			// patterns. Therefore we don't consider the two `[a]`
			// patterns to be the same. Because this style of alias is
			// only visible within the key's value, no part of l.b can be
			// resolved.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
			},
		},
		{
			name: "pattern expr - external",
			prog: `[l=a]: b: 3
c: l`,
			// This type of alias is only visible within the key's
			// value. So the use of l.b in c does not resolve.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
			},
		},

		{
			name: "expr - internal",
			prog: `a: l={b: 3, c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "expr - internal - explicit",
			prog: `a: l={b: 3} & {c: l.b}`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "expr - internal - explicit - paren",
			// The previous test case works because it's parsed like
			// this:
			prog: `a: l=({b: 3} & {c: l.b})`,
			expectations: map[*position][]*position{
				ln(1, 2, "l"): {ln(1, 1, "a")},
				ln(1, 2, "b"): {ln(1, 1, "b")},
			},
		},
		{
			name: "expr - external",
			prog: `a: l={b: 3}
c: l.b`,
			// This type of alias is only visible within the value.
			expectations: map[*position][]*position{
				ln(2, 1, "l"): {},
				ln(2, 1, "b"): {},
			},
		},
	}.run(t)
}

func TestDisjunctions(t *testing.T) {
	testCases{
		{
			name: "simple",
			prog: `d: {a: b: 3} | {a: b: 4, c: 5}
o: d.a.b
p: d.c
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b"), ln(1, 2, "b")},
				ln(3, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
			},
		},
		{
			name: "inline",
			prog: `d: ({a: b: 3} | {a: b: 4}) & {c: 5}
o: d.a.b
p: d.c
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a"), ln(1, 2, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b"), ln(1, 2, "b")},
				ln(3, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
			},
		},
		{
			name: "chained",
			prog: `d1: {a: 1} | {a: 2}
d2: {a: 3} | {a: 4}
o: (d1 & d2).a
`,
			expectations: map[*position][]*position{
				ln(3, 1, "a"): {ln(1, 1, "a"), ln(1, 2, "a"), ln(2, 1, "a"), ln(2, 2, "a")},
			},
		},
		{
			name: "selected",
			prog: `d: {x: 17} | string
r: d & {x: int}
out: r.x
`,
			expectations: map[*position][]*position{
				ln(2, 1, "d"): {ln(1, 1, "d")},
				ln(3, 1, "r"): {ln(2, 1, "r")},
				ln(3, 1, "x"): {ln(1, 1, "x"), ln(2, 1, "x")},
			},
		},
		{
			name: "scopes",
			prog: `c: {a: b} | {b: 3}
b: 7
d: c.b
`,
			expectations: map[*position][]*position{
				ln(1, 1, "b"): {ln(2, 1, "b")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
				ln(3, 1, "b"): {ln(1, 2, "b")},
			},
		},
		{
			name: "looping",
			prog: `a: {b: c.d, d: 3} | {d: 4}
c: a
`,
			expectations: map[*position][]*position{
				ln(1, 1, "c"): {ln(2, 1, "c")},
				ln(1, 1, "d"): {ln(1, 2, "d"), ln(1, 3, "d")},
				ln(2, 1, "a"): {ln(1, 1, "a")},
			},
		},
	}.run(t)
}

func TestComprehensions(t *testing.T) {
	testCases{
		{
			name: "if",
			prog: `a: 17
b: 3
if a < 10 {
	c: b
}`,
			expectations: map[*position][]*position{
				ln(3, 1, "a"): {ln(1, 1, "a")},
				ln(4, 1, "b"): {ln(2, 1, "b")},
			},
		},
		{
			name: "let",
			prog: `a: b: c: 17
let x=a.b
y: x.c
`,
			expectations: map[*position][]*position{
				ln(2, 1, "a"): {ln(1, 1, "a")},
				ln(2, 1, "b"): {ln(1, 1, "b")},
				ln(3, 1, "x"): {ln(2, 1, "x")},
				ln(3, 1, "c"): {ln(1, 1, "c")},
			},
		},
		{
			name: "for",
			prog: `a: { x: 1, y: 2, z: 3}
b: { x: 4, y: 5, z: 6}
o: {
	for k, v in a {
		(k): v * b[k]
	}
}`,
			expectations: map[*position][]*position{
				ln(4, 1, "k"): {},
				ln(4, 1, "v"): {},
				ln(4, 1, "a"): {ln(1, 1, "a")},
				ln(5, 1, "k"): {ln(4, 1, "k")},
				ln(5, 1, "v"): {ln(4, 1, "v")},
				ln(5, 1, "b"): {ln(2, 1, "b")},
				ln(5, 2, "k"): {ln(4, 1, "k")},
			},
		},
	}.run(t)
}
