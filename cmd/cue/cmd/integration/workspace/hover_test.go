package workspace

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/lsp/rangeset"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestHover(t *testing.T) {
	registryFS, err := txtar.FS(txtar.Parse([]byte(`
-- _registry/example.com_foo_v0.0.1/cue.mod/module.cue --
module: "example.com/foo@v0"
language: version: "v0.11.0"
-- _registry/example.com_foo_v0.0.1/x/y.cue --
// docs for package1
package x

// random docs

// schema docs1
// schema docs2
#Schema: { // docs3
	//docs4

	//docs5

	// name docs1
	// name docs2
	name!: string // docs6
	// docs6

	// docs7
} // docs8
// docs9

// docs10

// docs11
z: y: x: _
-- _registry/example.com_foo_v0.0.1/x/z.cue --
// docs for package2
package x

// schema docs3
// schema docs4
#Schema: {
	// name docs3
	// name docs4
	name!: _

	// age docs1
	// age docs2
	age!: int
}
`)))

	qt.Assert(t, qt.IsNil(err))
	reg, cacheDir := newRegistry(t, registryFS)

	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
deps: {
	"example.com/foo@v0": {
		v: "v0.0.1"
	}
}
-- a/a.cue --
package a

import "example.com/foo/x"

data: x.#Schema
data: name: "bob"

f: x.z
g: f.y.x
`

	WithOptions(
		RootURIAsDefaultFolder(), Registry(reg), Modes(DefaultModes()&^Forwarded),
	).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		cacheURI := protocol.URIFromPath(cacheDir) + "/mod/extract"
		env.Await(
			LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
		)
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/example.com/foo@v0.0.1/x] importPath=example.com/foo/x@v0 Reloaded", cacheURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		testCases := map[position]string{
			fln("a/a.cue", 3, 1, `"example.com/foo/x"`): fmt.Sprintf(`
docs for package1
([y.cue line 2](%s/example.com/foo@v0.0.1/x/y.cue#L2))

docs for package2
([z.cue line 2](%s/example.com/foo@v0.0.1/x/z.cue#L2))`[1:],
				cacheURI, cacheURI),

			fln("a/a.cue", 5, 1, "#Schema"): fmt.Sprintf(`
schema docs1
schema docs2
([y.cue line 8](%v/example.com/foo@v0.0.1/x/y.cue#L8))

schema docs3
schema docs4
([z.cue line 6](%v/example.com/foo@v0.0.1/x/z.cue#L6))`[1:],
				cacheURI, cacheURI),

			fln("a/a.cue", 6, 1, "name"): fmt.Sprintf(`
name docs1
name docs2
([y.cue line 15](%v/example.com/foo@v0.0.1/x/y.cue#L15))

name docs3
name docs4
([z.cue line 9](%v/example.com/foo@v0.0.1/x/z.cue#L9))`[1:],
				cacheURI, cacheURI),

			fln("a/a.cue", 8, 1, "z"): "",
			fln("a/a.cue", 9, 1, "x"): fmt.Sprintf(`
docs11
([y.cue line 25](%v/example.com/foo@v0.0.1/x/y.cue#L25))`[1:],
				cacheURI),
		}

		ranges := rangeset.NewFilenameRangeSet()

		for p, expectation := range testCases {
			p.determinePos(mappers)
			// it's len(p.str)+1 because we want to go from the cursor
			// before the start of the str up to cursor after the end of
			// str. So |str to str|
			strLen := len(p.str) + 1
			ranges.Add(p.filename, p.offset, p.offset+strLen)
			for i := range strLen {
				pos := p.pos
				pos.Character += uint32(i)
				got, _ := env.Hover(protocol.Location{
					URI:   p.mapper.URI,
					Range: protocol.Range{Start: pos},
				})
				// This test is only concerned with doc-comment hovers:
				// hover appends an "Unified with:" section showing the
				// unified value at the position, which [TestHoverValue]
				// tests.
				if got == nil {
					qt.Assert(t, qt.Equals("", expectation), qt.Commentf("%v(+%d)", p, i))
				} else {
					qt.Assert(t, qt.Equals(stripUnification(got.Value), expectation), qt.Commentf("%v(+%d)", p, i))
				}
			}
		}

		// Test that all offsets not explicitly mentioned in
		// expectations, have no hovers (for the open files only).
		for filename, mapper := range mappers {
			if !env.Editor.HasBuffer(filename) {
				continue
			}
			for i := range len(mapper.Content) {
				if ranges.Contains(filename, i) {
					continue
				}
				pos, err := mapper.OffsetPosition(i)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := env.Hover(protocol.Location{
					URI: mapper.URI,
					Range: protocol.Range{
						Start: pos,
					},
				})
				if got != nil {
					qt.Assert(t, qt.Equals(stripUnification(got.Value), ""), qt.Commentf("%v:%v (0-based)", filename, pos))
				}
			}
		}
	})
}

// stripUnification removes the "Unified with:" section that hover can
// append, returning only the doc-comments part.
func stripUnification(s string) string {
	before, _, wasCut := strings.Cut(s, "Unified with:")
	if !wasCut {
		return s
	}
	return strings.TrimRight(before, "\n")
}

func TestHoverValue(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "example.com/bar"
language: version: "v0.11.0"
-- a/a.cue --
package a

y: 5
x: y
z: int
x: z
w: len(x)
v: len()
u: {
	alpha: "aaaaaaaaaa",
	beta: "bbbbbbbbbb",
	gamma: "cccccccccc",
	delta: "dddddddddd",
	epsilon: "eeeeeeeeee",
	zeta: "ffffffffff"
}
t: u
t: _
`

	WithOptions(
		RootURIAsDefaultFolder(), Modes(DefaultModes()&^Forwarded),
	).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		env.Await(
			LogExactf(protocol.Debug, 1, false, "Workspace folder added: %v", rootURI),
		)
		env.OpenFile("a/a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/a] importPath=example.com/bar/a@v0 Reloaded", rootURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		cue := func(value string) string {
			return fmt.Sprintf("Unified with: `%s`", value)
		}
		testCases := map[position]string{
			// On x's key: the cursor declaration's value is a
			// reference, so its expansion is shown, along with x's
			// other declaration.
			fln("a/a.cue", 4, 1, "x"): cue("5 & int"),
			// On the reference y: a position within x's value, so
			// the unified value of x, exactly as for x's key.
			fln("a/a.cue", 4, 1, "y"): cue("5 & int"),
			// On z's declaration: z has no other declarations, and
			// its value contains nothing to expand (int is a
			// builtin), so there is nothing to show.
			fln("a/a.cue", 5, 1, "z: int"): "",
			// On the reference x within a call argument: the value
			// of x.
			fln("a/a.cue", 7, 1, "x"): cue("5 & int"),
			// On the callee: w has no other declarations, but the
			// cursor declaration's call argument contains a
			// reference, so the declaration is shown expanded.
			fln("a/a.cue", 7, 1, "len"): cue("len(5 & int)"),
			// In the interior of the call parens: no hover; a call's
			// argument region is not unified with anything.
			fln("a/a.cue", 8, 1, ")"): "",
			// On u's key: u has no other declarations.
			fln("a/a.cue", 9, 1, "u"): "",
			// On the key of t's second declaration: t's first
			// declaration remains, inlining u — too wide for one
			// line, so it renders as a fenced block on the lines
			// after the heading.
			fln("a/a.cue", 18, 1, "t"): "Unified with:\n```cue\n{\n  alpha:   \"aaaaaaaaaa\"\n  beta:    \"bbbbbbbbbb\"\n  gamma:   \"cccccccccc\"\n  delta:   \"dddddddddd\"\n  epsilon: \"eeeeeeeeee\"\n  zeta:    \"ffffffffff\"\n}\n```",
		}

		for p, expectation := range testCases {
			p.determinePos(mappers)
			got, _ := env.Hover(protocol.Location{
				URI:   p.mapper.URI,
				Range: protocol.Range{Start: p.pos},
			})
			if got == nil {
				qt.Assert(t, qt.Equals("", expectation), qt.Commentf("%v", p))
			} else {
				qt.Assert(t, qt.Equals(got.Value, expectation), qt.Commentf("%v", p))
			}
		}
	})
}

// Convenience constructor to make a new [position] with the given
// line number (1-based), for the n-th (1-based) occurrence of str
// within the given file.
func fln(filename string, i, n int, str string) position {
	return position{
		filename: filename,
		line:     i,
		n:        n,
		str:      str,
	}
}

type position struct {
	filename string
	line     int
	n        int
	str      string
	offset   int
	mapper   *protocol.Mapper
	pos      protocol.Position
}

func (p *position) String() string {
	return fmt.Sprintf(`fln(%q, %d, %d, %q)`, p.filename, p.line, p.n, p.str)
}

func (p *position) determinePos(mappers map[string]*protocol.Mapper) {
	if p.offset != 0 {
		return
	}
	if p.filename == "" {
		if len(mappers) == 1 {
			for name := range mappers {
				p.filename = name
			}
		} else {
			panic("no filename set and more than one file available")
		}
	}
	mapper := mappers[p.filename]
	p.mapper = mapper
	startOffset, err := mapper.PositionOffset(protocol.Position{Line: uint32(p.line) - 1})
	if err != nil {
		panic(fmt.Sprintf("invalid line %d (1-based): %v", p.line, err))
	}
	endOffset, err := mapper.PositionOffset(protocol.Position{Line: uint32(p.line)})
	if err != nil {
		panic(fmt.Sprintf("invalid line %d (1-based): %v", p.line, err))
	}
	line := string(mapper.Content[startOffset:endOffset])
	n := p.n
	column := 0
	for i := range line {
		if strings.HasPrefix(line[i:], p.str) {
			n--
			if n == 0 {
				column = i
				break
			}
		}
	}
	if n != 0 {
		panic("Failed to determine offset")
	}
	p.offset = startOffset + column
	p.pos, err = mapper.OffsetPosition(p.offset)
	if err != nil {
		panic(fmt.Sprintf("invalid offset %d: %v", p.offset, err))
	}
}
