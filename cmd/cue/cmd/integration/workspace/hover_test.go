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
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=example.com/bar@v0 Loaded Package dirs=[%v/a] importPath=example.com/bar/a@v0", rootURI, rootURI),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v/example.com/foo@v0.0.1 module=example.com/foo@v0 Loaded Package dirs=[%v/example.com/foo@v0.0.1/x] importPath=example.com/foo/x@v0", cacheURI, cacheURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		testCases := map[position]string{
			fln("a/a.cue", 3, 1, `"example.com/foo/x"`): fmt.Sprintf(
				`docs for package1
([y.cue line 2](%s/example.com/foo@v0.0.1/x/y.cue#L2))

docs for package2
([z.cue line 2](%s/example.com/foo@v0.0.1/x/z.cue#L2))
`, cacheURI, cacheURI),

			fln("a/a.cue", 5, 1, "#Schema"): fmt.Sprintf(
				`schema docs1
schema docs2
([y.cue line 8](%v/example.com/foo@v0.0.1/x/y.cue#L8))

schema docs3
schema docs4
([z.cue line 6](%v/example.com/foo@v0.0.1/x/z.cue#L6))
`, cacheURI, cacheURI),

			fln("a/a.cue", 6, 1, "name"): fmt.Sprintf(`name docs1
name docs2
([y.cue line 15](%v/example.com/foo@v0.0.1/x/y.cue#L15))

name docs3
name docs4
([z.cue line 9](%v/example.com/foo@v0.0.1/x/z.cue#L9))
`, cacheURI, cacheURI),
		}

		ranges := rangeset.NewFilenameRangeSet()

		for p, expectation := range testCases {
			p.determinePos(mappers)
			ranges.Add(p.filename, p.offset, p.offset+len(p.str))
			for i := range p.str {
				pos := p.pos
				pos.Character += uint32(i)
				got, _ := env.Hover(protocol.Location{
					URI:   p.mapper.URI,
					Range: protocol.Range{Start: pos},
				})
				qt.Assert(t, qt.Equals(got.Value, expectation), qt.Commentf("%v(+%d)", p, i))
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
				qt.Assert(t, qt.IsNil(got), qt.Commentf("%v:%v (0-based)", filename, pos))
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
