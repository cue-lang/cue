package workspace

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"golang.org/x/tools/txtar"
)

// TestEmbedChangeAttributeHover tests that when the file name within
// an @embed attribute is edited, hovers within the previously- and
// newly-embedded json files reflect the change.
func TestEmbedChangeAttributeHover(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)

out: field: {
  // does the field contain cows?
  cows: bool
}
-- data/data1.json --
{
  "field": {
    "cows": true
  }
}
-- data/data2.json --
{
  "field": {
    "cows": false
  }
}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.OpenFile("data/data1.json")
		env.OpenFile("data/data2.json")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		docComment := "does the field contain cows?"

		hover := func(p position) string {
			p.determinePos(mappers)
			got, _ := env.Hover(protocol.Location{
				URI:   p.mapper.URI,
				Range: protocol.Range{Start: p.pos},
			})
			if got == nil {
				return ""
			}
			return got.Value
		}

		// Before the change: data1.json is embedded, data2.json is not.
		if v := hover(fln("data/data1.json", 3, 1, `cows`)); !strings.Contains(v, docComment) {
			t.Errorf("before change: hover in data1.json = %q, want doc comment", v)
		}
		if v := hover(fln("data/data2.json", 3, 1, `cows`)); strings.Contains(v, docComment) {
			t.Errorf("before change: hover in data2.json = %q, want no doc comment", v)
		}

		// Change the embed attribute to point at data2.json instead.
		env.RegexpReplace("a.cue", "data1", "data2")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// After the change: data2.json is embedded, data1.json is not.
		if v := hover(fln("data/data2.json", 3, 1, `cows`)); !strings.Contains(v, docComment) {
			t.Errorf("after change: hover in data2.json = %q, want doc comment", v)
		}
		if v := hover(fln("data/data1.json", 3, 1, `cows`)); strings.Contains(v, docComment) {
			t.Errorf("after change: hover in data1.json = %q, want no doc comment", v)
		}
	})
}

// TestEmbedChangeAttributeBackAndForth changes the embed attribute
// from data1.json to data2.json and then back again. The second
// change relinks to the still-loaded (stale) phantom package for
// data1.json.
func TestEmbedChangeAttributeBackAndForth(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- data/data1.json --
{"field1": true}
-- data/data2.json --
{"field2": true}
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

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/data1.json)"): {
				fln("data/data1.json", 1, 1, ""),
			},
		}.run(t, env, mappers)

		env.RegexpReplace("a.cue", "data1", "data2")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/data1.json)"): {
				fln("data/data2.json", 1, 1, ""),
			},
		}.run(t, env, mappers)

		env.RegexpReplace("a.cue", "data2", "data1")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 3, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/data1.json)"): {
				fln("data/data1.json", 1, 1, ""),
			},
		}.run(t, env, mappers)
	})
}

// TestEmbedChangeAttributeDiagnostics changes the embed attribute to
// point at a file which does not exist, expecting a diagnostic, and
// then changes it back, expecting the diagnostic to clear.
func TestEmbedChangeAttributeDiagnostics(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- data/data1.json --
{"field1": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			NoDiagnostics(ForFile("a.cue")),
		)

		// Point the attribute at a file which does not exist.
		env.RegexpReplace("a.cue", "data1", "data3")
		env.Await(
			env.DoneWithChange(),
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)

		// And point it back at the file which does exist.
		env.RegexpReplace("a.cue", "data3", "data1")
		env.Await(
			env.DoneWithChange(),
			NoDiagnostics(ForFile("a.cue")),
		)
	})
}

// TestEmbedChangeAttributeGlob changes the glob pattern within an
// embed attribute so that it matches a different set of files.
func TestEmbedChangeAttributeGlob(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(glob=data/d1*.json)
-- data/d1a.json --
{"field1": true}
-- data/d2a.json --
{"field2": true}
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

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(glob=data/d1*.json)"): {
				fln("data/d1a.json", 1, 1, ""),
			},
		}.run(t, env, mappers)

		env.RegexpReplace("a.cue", `glob=data/d1`, "glob=data/d2")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(glob=data/d1*.json)"): {
				fln("data/d2a.json", 1, 1, ""),
			},
		}.run(t, env, mappers)
	})
}

// TestEmbedChangeAttributeIncrementalEdits simulates typing the new
// file name character by character (passing through intermediate
// states which name files that do not exist), followed by a save.
func TestEmbedChangeAttributeIncrementalEdits(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- data/data1.json --
{"field1": true}
-- data/dataX23.json --
{"fieldX23": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Type towards dataX23.json one edit at a time, without
		// awaiting quiescence in between.
		env.RegexpReplace("a.cue", `data1\.json`, "dataX.json")
		env.RegexpReplace("a.cue", `dataX\.json`, "dataX2.json")
		env.RegexpReplace("a.cue", `dataX2\.json`, "dataX23.json")
		env.SaveBuffer("a.cue")
		env.Await(
			env.DoneWithChange(),
			env.DoneWithSave(),
			NoDiagnostics(ForFile("a.cue")),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}
		// The attribute has changed length: recompute the a.cue mapper
		// from the current buffer content.
		mappers["a.cue"] = protocol.NewMapper(rootURI+"/a.cue", []byte(env.BufferText("a.cue")))

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/dataX23.json)"): {
				fln("data/dataX23.json", 1, 1, ""),
			},
		}.run(t, env, mappers)
	})
}

// TestEmbedChangeAttributeInactiveDir changes the embed attribute to
// point at an existing file in a directory which contains no other
// active files.
func TestEmbedChangeAttributeInactiveDir(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- data/data1.json --
{"field1": true}
-- other/data3.json --
{"field3": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		env.RegexpReplace("a.cue", `data/data1\.json`, "other/data3.json")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			NoDiagnostics(ForFile("a.cue")),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}
		mappers["a.cue"] = protocol.NewMapper(rootURI+"/a.cue", []byte(env.BufferText("a.cue")))

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=other/data3.json)"): {
				fln("other/data3.json", 1, 1, ""),
			},
		}.run(t, env, mappers)
	})
}

// TestEmbedLateFileOnDisk starts with an embed attribute which names
// a file that does not exist, and then creates that file on disk (not
// in the editor), in a directory which contains no other active
// files.
//
// This test shows the current behaviour, which is bad: the creation
// of the file is ignored because its directory contains no active
// files, even though a loaded package has an embed attribute which
// matches the new file. So the embedding package is not reloaded, the
// "failed to stat" diagnostic does not clear, and the embed is never
// linked.
func TestEmbedLateFileOnDisk(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=other/data3.json)
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)

		// Now create the file on disk, without opening it in the
		// editor.
		env.WriteWorkspaceFile("other/data3.json", `{"field3": true}`)
		env.Await(
			env.DoneWithChangeWatchedFiles(),
			// The new file is ignored: no package is created for it,
			// the embedding package is not reloaded, and the stale
			// diagnostic remains.
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/other\] importPath=mod\.example/x/other@v0:_.+ Created`, rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}

		// Jump-to-definition on the attribute finds nothing.
		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=other/data3.json)"): {},
		}.run(t, env, mappers)
	})
}

// TestEmbedLateGlobOnDisk starts with a glob embed attribute which
// matches nothing, and then creates a matching file on disk (not in
// the editor), in a directory which contains no other active files.
//
// This test shows the current behaviour, which is bad: the creation
// of the file is ignored because its directory contains no active
// files, even though it matches the glob embed attribute of a loaded
// package. So the embedding package is not reloaded, the "no matches
// for glob pattern" diagnostic does not clear, and the embed is never
// linked.
func TestEmbedLateGlobOnDisk(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(glob=other/*.json)
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)

		env.WriteWorkspaceFile("other/data3.json", `{"field3": true}`)
		env.Await(
			env.DoneWithChangeWatchedFiles(),
			// The new file is ignored: no package is created for it,
			// the embedding package is not reloaded, and the stale
			// diagnostic remains.
			NoLogMatching(protocol.Debug, `Package dirs=\[%v/other\] importPath=mod\.example/x/other@v0:_.+ Created`, rootURI),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			Diagnostics(ForFile("a.cue"), env.AtRegexp("a.cue", "@embed")),
		)
	})
}

// TestEmbedChangeAttributeLateFile changes the embed attribute to
// point at a file which does not exist yet, and then creates that
// file.
func TestEmbedChangeAttributeLateFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- data/data1.json --
{"field1": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Point the attribute at a file which does not exist yet.
		env.RegexpReplace("a.cue", "data1", "data3")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Now create the file.
		env.CreateBuffer("data/data3.json", `{"field3": true}`)
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 3, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}
		mappers["data/data3.json"] = protocol.NewMapper(rootURI+"/data/data3.json", []byte(`{"field3": true}`))

		definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/data1.json)"): {
				fln("data/data3.json", 1, 1, ""),
			},
		}.run(t, env, mappers)
	})
}

// TestEmbedChangeAttributeFile changes the file name within an
// @embed attribute. The LSP state should be invalidated so that the
// embedding package links to the new embedded file's package.
func TestEmbedChangeAttributeFile(t *testing.T) {
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"

-- a.cue --
@extern(embed)
package a

out: _ @embed(file=data/data1.json)
-- data/data1.json --
{"field1": true}
-- data/data2.json --
{"field2": true}
`
	WithOptions(RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *Env) {
		rootURI := env.Sandbox.Workdir.RootURI()

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
			LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
		)

		mappersBefore := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappersBefore[file.Name] = mapper
		}
		// Query definitions before the change, so that lazy evaluation
		// state gets populated and must be invalidated by the change.
		testCasesBefore := definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/data1.json)"): {
				fln("data/data1.json", 1, 1, ""),
			},
		}
		testCasesBefore.run(t, env, mappersBefore)

		// Change the embed attribute to point at data2.json instead.
		env.RegexpReplace("a.cue", "data1", "data2")
		env.Await(
			env.DoneWithChange(),
			LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
			// A second phantom package (for data2.json) should now be
			// created and loaded.
			LogMatching(protocol.Debug, 2, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Created`, rootURI),
			LogMatching(protocol.Debug, 2, false, `Package dirs=\[%v/data\] importPath=mod\.example/x/data@v0:_.+ Reloaded`, rootURI),
		)

		mappers := make(map[string]*protocol.Mapper)
		for _, file := range txtar.Parse([]byte(files)).Files {
			mapper := protocol.NewMapper(rootURI+"/"+protocol.DocumentURI(file.Name), file.Data)
			mappers[file.Name] = mapper
		}
		// NB the a.cue mapper reflects the original content; the buffer
		// now says data2. The attribute's position/length is unchanged
		// because data1 -> data2 is a same-length replacement.

		// Jumping to definition from the embed attribute should now
		// arrive at data2.json.
		testCases := definitionsTestCases{
			fln("a.cue", 4, 1, "@embed(file=data/data1.json)"): {
				fln("data/data2.json", 1, 1, ""),
			},
		}
		testCases.run(t, env, mappers)
	})
}
