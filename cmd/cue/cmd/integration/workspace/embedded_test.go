package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/lsp/rangeset"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestEmbedSimple(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data.json)
-- data/data.json --
{"field": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
		)
	})
}

func TestEmbedMissingExtern(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
package a

out: _ @embed(file=data/data.json)
-- data/data.json --
{"field": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
		)
	})
}

func TestEmbedMissingFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/missing.json)
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
		)
	})
}

func TestEmbedLateFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/late.json)
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
		)
		env.CreateBuffer("data/late.json", `{"field": 5}`)
		// embeds are "upstream", so we do reload the downstream a.cue -
		// it's the same as if one of its imports had changed.
		env.Await(
			env.DoneWithOpen(),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)
	})
}

func TestEmbedDeleteFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data.json)
-- data/data.json --
{"field": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
		)
		err := env.Sandbox.Workdir.RemoveFile(env.Ctx, "data/data.json")
		qt.Assert(t, qt.IsNil(err))
		env.CheckForFileChanges()
		// We should see a delete of the embed pkg, and a reload of the x@v0:a
		env.Await(
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Deleted`, rootURI),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)
	})
}

func TestEmbedMissingGlob(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(glob=data/*.json)
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
		)
	})
}

func TestEmbedLateGlob(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(glob=data/*.json)
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
		)
		env.CreateBuffer("data/late1.json", `{"field": 5}`)
		// embeds are "upstream", so we do reload the downstream a.cue -
		// it's the same as if one of its imports had changed.
		env.Await(
			env.DoneWithOpen(),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		env.CreateBuffer("data/late2.json", `{"field": 6}`)
		// We will create+load the late2.json package, and reload a.cue, but we won't reload late1.json.
		env.Await(
			env.DoneWithOpen(),
			LogMatching(protocol.Debug, 2, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
			LogExactf(protocol.Debug, 3, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)
	})
}

func TestEmbedDeleteGlob(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(glob=data/*.json)
-- data/data1.json --
{"field": true}
-- data/data2.json --
{"field": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Module dir=%v module=mod.example/x@v0 Loading packages [mod.example/x@v0:a]", rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			// Both embedded files/packages get loaded
			LogMatching(protocol.Debug, 2, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
			LogMatching(protocol.Debug, 2, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
		)
		err := env.Sandbox.Workdir.RemoveFile(env.Ctx, "data/data1.json")
		qt.Assert(t, qt.IsNil(err))
		env.CheckForFileChanges()
		env.Await(
			// One embedded file gets deleted.
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Deleted`, rootURI),
			// The survivor does *not* get reloaded (no need).
			LogMatching(protocol.Debug, 2, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
			// And the downstream will get reloaded.
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)
	})
}

func TestEmbedHover(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: s @embed(file=data/file.json)

out: field: {
  // does the field contain cows?
  cows: bool
}

glob: {"data/d1.json": s} @embed(glob=data/d*.json)
-- b.cue --
package a

s: {
  // how many fields do we have?
  fieldCount: int
}

s: field: {
  // does the field contain sheep?
  sheep: bool
}
-- data/file.json --
{
  "field": {
    "sheep": true,
    "cows": false
  },
  "fieldCount": "wrong"
}
-- data/d1.json --
{
  "field": {
    "sheep": false,
    "cows": true
  },
  "fieldCount": 6
}
-- data/d2.json --
{
  "field": {
    "cows": false
  },
  "fieldCount": -1
}
`

	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("data/file.json")
		env.OpenFile("data/d1.json")
		env.OpenFile("data/d2.json")
		env.Await(
			env.DoneWithOpen(),
			LogMatching(protocol.Debug, fmt.Sprintf(`Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI), 3, false),
			LogMatching(protocol.Debug, fmt.Sprintf(`Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI), 3, false),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		testCases := map[position]string{
			fln("data/file.json", 3, 1, `sheep`): fmt.Sprintf(`
does the field contain sheep?
([b.cue line 10](%s/b.cue#L10))`[1:],
				rootURI),
			fln("data/file.json", 4, 1, `cows`): fmt.Sprintf(`
does the field contain cows?
([a.cue line 8](%s/a.cue#L8))`[1:],
				rootURI),
			fln("data/file.json", 6, 1, `fieldCount`): fmt.Sprintf(`
how many fields do we have?
([b.cue line 5](%s/b.cue#L5))`[1:],
				rootURI),

			fln("data/d1.json", 3, 1, `sheep`): fmt.Sprintf(`
does the field contain sheep?
([b.cue line 10](%s/b.cue#L10))`[1:],
				rootURI),
			fln("data/d1.json", 6, 1, `fieldCount`): fmt.Sprintf(`
how many fields do we have?
([b.cue line 5](%s/b.cue#L5))`[1:],
				rootURI),
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

func TestEmbedDefinitions(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: s @embed(file=data/file.json)

out: field: {
  // does the field contain cows?
  cows: bool
}

glob: {"data/d1.json": s} @embed(glob=data/d*.json)

x1: out.field
x2: glob."data/d2.json".field
-- b.cue --
package a

s: {
  // how many fields do we have?
  fieldCount: int
}

s: field: {
  // does the field contain sheep?
  sheep: bool
}
-- data/file.json --
{
  "field": {
    "sheep": true,
    "cows": false
  },
  "fieldCount": "wrong"
}
-- data/d1.json --
{
  "field": {
    "sheep": false,
    "cows": true
  },
  "fieldCount": 6
}
-- data/d2.json --
{
  "field": {
    "cows": false
  },
  "fieldCount": -1
}
`

	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		testCases := map[position][]position{
			fln("a.cue", 13, 1, "field"): {
				fln("a.cue", 6, 1, "field"),
				fln("b.cue", 8, 1, "field"),
				fln("data/file.json", 2, 1, "field"),
			},

			fln("a.cue", 14, 1, "field"): {
				fln("data/d2.json", 2, 1, "field"),
			},

			fln("a.cue", 4, 1, "@embed(file=data/file.json)"): {
				fln("data/file.json", 1, 1, ""),
			},

			fln("a.cue", 11, 1, "@embed(glob=data/d*.json)"): {
				fln("data/d1.json", 1, 1, ""),
				fln("data/d2.json", 1, 1, ""),
			},
		}

		for posFrom, posWants := range testCases {
			posFrom.determinePos(mappers)
			for i := range posWants {
				posWant := &posWants[i]
				posWant.determinePos(mappers)
			}

			strLen := len(posFrom.str) + 1
			for i := range strLen {
				pos := posFrom.pos
				pos.Character += uint32(i)

				posGots := env.Definition(protocol.Location{
					URI:   posFrom.mapper.URI,
					Range: protocol.Range{Start: pos},
				})
				qt.Assert(t, qt.Equals(len(posGots), len(posWants)))

				for i, posWant := range posWants {
					posGot := posGots[i]
					posWantLoc := protocol.Location{
						URI: posWant.mapper.URI,
						Range: protocol.Range{
							Start: posWant.pos,
							End:   posWant.pos,
						},
					}
					posWantLoc.Range.End.Character += uint32(len(posWant.str))
					qt.Assert(t, qt.Equals(posGot, posWantLoc))
				}
			}

		}
	})
}
