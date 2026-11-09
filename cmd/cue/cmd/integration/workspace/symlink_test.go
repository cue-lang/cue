package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	I "cuelang.org/go/internal/golangorgx/gopls/test/integration"
	"cuelang.org/go/internal/golangorgx/gopls/test/integration/fake"
	"github.com/go-quicktest/qt"
)

func TestSymlinks(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- a.cue --
package a
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		rootFilePath := rootURI.FilePath()

		// Need to wind the mtime of a.cue back into the past so that
		// fs_cache will cache the file.
		aCueFilePath := filepath.Join(rootFilePath, "a.cue")
		then := time.Now().Add(-10 * time.Second)
		err := os.Chtimes(aCueFilePath, then, then)
		qt.Assert(t, qt.IsNil(err))

		env.OpenFile("a.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// The order of these events is very important. We need to
		// create the symlink and notify the server whilst the
		// file-buffer is still open. That keeps the module open and the
		// symlinked file (b.cue) will be added to the package within
		// the module.

		err = os.Symlink(aCueFilePath, filepath.Join(rootFilePath, "b.cue"))
		qt.Assert(t, qt.IsNil(err))
		env.CheckForFileChanges()

		env.Await(
			I.LogExactf(protocol.Debug, 2, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		// Closing a.cue will now cause a re-read of the file from disk
		// (as the overlay has gone), which exercises the code that
		// deals with two file URIs having the same underlying file on
		// disk.
		env.CloseBuffer("a.cue")
		env.Await(
			env.DoneWithClose(),
			I.LogExactf(protocol.Debug, 3, false, "Package dirs=[%v] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)
	})
}

// TestSymlinksDefinition checks that a file which is reachable via
// two paths is treated as two files, each with positions under its
// own path.
func TestSymlinksDefinition(t *testing.T) {
	t.Parallel()
	const files = `
-- cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"
-- real/x.cue --
package p

foo: 1
-- real/y.cue --
package p

bar: foo
-- c/c.cue --
package c

import "mod.example/x/link:p"

v: p.foo
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		rootFilePath := rootURI.FilePath()

		// Need to wind the mtime of x.cue back into the past so that
		// fs_cache will cache the file.
		xCueFilePath := filepath.Join(rootFilePath, "real", "x.cue")
		then := time.Now().Add(-10 * time.Second)
		err := os.Chtimes(xCueFilePath, then, then)
		qt.Assert(t, qt.IsNil(err))
		err = os.Mkdir(filepath.Join(rootFilePath, "link"), 0o777)
		qt.Assert(t, qt.IsNil(err))
		err = os.Symlink(xCueFilePath, filepath.Join(rootFilePath, "link", "x.cue"))
		qt.Assert(t, qt.IsNil(err))

		fooRange := protocol.Range{
			Start: protocol.Position{Line: 2, Character: 0},
			End:   protocol.Position{Line: 2, Character: 3},
		}

		// The package in real reads x.cue first, as real/x.cue.
		env.OpenFile("real/y.cue")
		env.Await(env.DoneWithOpen())
		got := env.Definition(protocol.Location{
			URI:   rootURI + "/real/y.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 5}},
		})
		qt.Check(t, qt.DeepEquals(got, []protocol.Location{{URI: rootURI + "/real/x.cue", Range: fooRange}}))

		// The package in link then reads the same file as link/x.cue.
		env.OpenFile("c/c.cue")
		env.Await(env.DoneWithOpen())
		got = env.Definition(protocol.Location{
			URI:   rootURI + "/c/c.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 5}},
		})
		qt.Check(t, qt.DeepEquals(got, []protocol.Location{{URI: rootURI + "/link/x.cue", Range: fooRange}}))
	})
}

func TestSymlinksAncestor(t *testing.T) {
	t.Parallel()
	const files = `
-- real/cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.11.0"

-- real/a.cue --
package a

y: x

// docs for x
x: _
`
	I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
		rootURI := env.Sandbox.Workdir.RootURI()
		rootFilePath := rootURI.FilePath()

		err := os.Symlink(filepath.Join(rootFilePath, "real"), filepath.Join(rootFilePath, "sym link"))
		qt.Assert(t, qt.IsNil(err))

		env.OpenFile("sym link/a.cue")
		env.Await(
			env.DoneWithOpen(),
			I.LogExactf(protocol.Debug, 1, false, "Package dirs=[%v/sym%%20link] importPath=mod.example/x@v0:a Reloaded", rootURI),
		)

		got, _ := env.Hover(protocol.Location{
			URI:   rootURI + "/sym%20link/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 3}},
		})
		qt.Assert(t, qt.IsNotNil(got))
		qt.Assert(t, qt.IsTrue(strings.HasPrefix(got.Value, "docs for x")))

		// add a newline (and comment so that the hover coords exist), before line 2
		env.EditBuffer("sym link/a.cue", fake.NewEdit(2, 0, 2, 0, "// hi\n"))
		env.Await(env.DoneWithChange())

		got, _ = env.Hover(protocol.Location{
			URI:   rootURI + "/sym%20link/a.cue",
			Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 3}},
		})
		qt.Assert(t, qt.IsNil(got))
	})
}

// TestNonCUEEntries checks that an entry of a package directory which
// cannot be a CUE file does not stop the package from loading.
func TestNonCUEEntries(t *testing.T) {
	t.Parallel()
	const files = `
-- mod/cue.mod/module.cue --
module: "mod.example/x"
language: version: "v0.16.0"
-- mod/a.cue --
@extern(embed)
package a

data: _ @embed(glob=link/*.json)

x: 1
y: x
-- real/data.json --
{"field": true}
`
	symlink := func(target string) func(t *testing.T, path string) {
		return func(t *testing.T, path string) {
			err := os.Symlink(target, path)
			qt.Assert(t, qt.IsNil(err))
		}
	}
	// Reading a FIFO blocks until something opens it to write.
	fifo := func(t *testing.T, path string) {
		if runtime.GOOS == "windows" {
			// Git Bash has a mkfifo, but it makes no FIFO that Go can see.
			t.Skip("no FIFOs on Windows")
		}
		if _, err := exec.LookPath("mkfifo"); err != nil {
			t.Skip(err)
		}
		err := exec.Command("mkfifo", path).Run()
		qt.Assert(t, qt.IsNil(err))
	}
	tests := []struct {
		entry  string
		create func(t *testing.T, path string)
	}{
		// The embed reaches outside the module through the link.
		{"link", symlink(filepath.Join("..", "real"))},
		{"missing", symlink("nonexistent")},
		{"loop", symlink("loop")},
		{"pipe", fifo},
		// Only on the command line does "-" stand for standard input.
		{"-", fifo},
		{"secret", func(t *testing.T, path string) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("cannot make a file unreadable")
			}
			err := os.WriteFile(path, []byte("secret"), 0)
			qt.Assert(t, qt.IsNil(err))
		}},
	}
	for _, test := range tests {
		t.Run(test.entry, func(t *testing.T) {
			t.Parallel()
			I.WithOptions(I.RootURIAsDefaultFolder()).Run(t, files, func(t *testing.T, env *I.Env) {
				rootURI := env.Sandbox.Workdir.RootURI()
				test.create(t, filepath.Join(rootURI.FilePath(), "mod", test.entry))

				env.OpenFile("mod/a.cue")
				env.Await(env.DoneWithOpen())
				got := env.Definition(protocol.Location{
					URI:   rootURI + "/mod/a.cue",
					Range: protocol.Range{Start: protocol.Position{Line: 6, Character: 3}},
				})
				qt.Assert(t, qt.DeepEquals(got, []protocol.Location{{
					URI: rootURI + "/mod/a.cue",
					Range: protocol.Range{
						Start: protocol.Position{Line: 5, Character: 0},
						End:   protocol.Position{Line: 5, Character: 1},
					},
				}}))
				if test.entry == "link" {
					env.Await(
						I.LogMatching(protocol.Debug, 1, false, `Package dirs=\[%v/mod/link\] importPath=mod\.example/x/link@v0:_.+ Reloaded`, rootURI),
					)
				}
			})
		})
	}
}
