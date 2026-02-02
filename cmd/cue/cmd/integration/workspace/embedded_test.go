package workspace

import (
	"testing"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	. "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"github.com/go-quicktest/qt"
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
