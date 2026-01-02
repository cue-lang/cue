package load

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/source"
	"github.com/go-quicktest/qt"
)

func TestIOFS(t *testing.T) {
	dir := t.TempDir()
	onDiskFiles := []string{
		"foo/bar/a",
		"foo/bar/b",
		"foo/baz",
		"arble",
	}
	for _, f := range onDiskFiles {
		writeFile(t, filepath.Join(dir, f), f)
	}
	overlayFiles := []string{
		"foo/bar/a",
		"foo/bar/c",
		"other/x",
	}
	overlay := map[string]Source{}
	for _, f := range overlayFiles {
		overlay[filepath.Join(dir, f)] = FromString(f + " overlay")
	}

	fsys, err := newFileSystem(&Config{
		Dir:     filepath.Join(dir, "foo"),
		Overlay: overlay,
	})
	qt.Assert(t, qt.IsNil(err))
	ffsys := fsys.ioFS(dir, "v0.12.0")
	err = fstest.TestFS(ffsys, append(slices.Clip(onDiskFiles), overlayFiles...)...)
	qt.Assert(t, qt.IsNil(err))
	checked := make(map[string]bool)
	for _, f := range overlayFiles {
		data, err := fs.ReadFile(ffsys, f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(data), f+" overlay"))
		checked[f] = true
	}
	for _, f := range onDiskFiles {
		if checked[f] {
			continue
		}
		data, err := fs.ReadFile(ffsys, f)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(string(data), f))
	}
}

func TestLoadFromFS(t *testing.T) {
	cfs := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`
				module: "example.com"
				language: version: "v0.8.0"
			`),
		},
		"main.cue": &fstest.MapFile{
			Data: []byte(`
				package main

				import "example.com/lib"

				x: lib.y
			`),
		},
		"lib/lib.cue": &fstest.MapFile{
			Data: []byte(`
				package lib

				y: 42
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: ".",
	}

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))

	ctx := cuecontext.New()
	val := ctx.BuildInstance(insts[0])
	qt.Assert(t, qt.IsNil(val.Err()))

	x := val.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsTrue(x.Exists()))
	n, err := x.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(n, int64(42)))
}

func TestLoadFromFS_IgnoresHostFS(t *testing.T) {
	tmp := t.TempDir()

	// Host FS file that must NOT be used
	err := os.WriteFile(filepath.Join(tmp, "main.cue"), []byte(`
		package main
		x: 999
	`), 0o644)
	qt.Assert(t, qt.IsNil(err))

	cfs := fstest.MapFS{
		"main.cue": &fstest.MapFile{
			Data: []byte(`
				package main
				x: 42
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: ".", // IMPORTANT: virtual root, not tmp
	}

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))

	val := cuecontext.New().BuildInstance(insts[0])
	x := val.LookupPath(cue.ParsePath("x"))
	n, err := x.Int64()

	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(n, int64(42)))
}

func TestLoadFromFS_SubdirAsDir(t *testing.T) {
	cfs := fstest.MapFS{
		"app/main.cue": &fstest.MapFile{
			Data: []byte(`
				package main
				x: 1
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: "app",
	}

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))

	val := cuecontext.New().BuildInstance(insts[0])
	x := val.LookupPath(cue.ParsePath("x"))
	n, err := x.Int64()

	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(n, int64(1)))
}

func TestLoadFromFS_MissingModulePackage(t *testing.T) {
	cfs := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`
				module: "example.com"
				language: version: "v0.8.0"
			`),
		},
		"main.cue": &fstest.MapFile{
			Data: []byte(`
				package main
				import "example.com/foo"
				x: foo.y
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: ".",
	}

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNotNil(insts[0].Err))

	qt.Assert(t, qt.StringContains(
		insts[0].Err.Error(),
		"cannot find package",
	))
}

func TestLoadFromFS_MissingModule(t *testing.T) {
	cfs := fstest.MapFS{
		"main.cue": &fstest.MapFile{
			Data: []byte(`
				package main
				import "example.com/foo"
				x: foo.y
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: ".",
	}

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNotNil(insts[0].Err))

	qt.Assert(t, qt.StringContains(
		insts[0].Err.Error(),
		"imports are unavailable because there is no cue.mod/module.cue file",
	))
}

func TestLoadFromFS_OverlayOverrides(t *testing.T) {
	cfs := fstest.MapFS{
		"main.cue": &fstest.MapFile{
			Data: []byte(`
				package main
				x: 1
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: ".",
		Overlay: map[string]Source{
			"@fs/main.cue": FromString(`
				package main
				x: 99
			`),
		},
	}

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))

	val := cuecontext.New().BuildInstance(insts[0])
	x := val.LookupPath(cue.ParsePath("x"))
	n, err := x.Int64()

	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(n, int64(99)))
}

func TestLoadFromFS_EncodingUsesVirtualFS(t *testing.T) {
	called := false

	cfs := fstest.MapFS{
		"main.cue": &fstest.MapFile{
			Data: []byte(`
				package main
				x: 1
			`),
		},
	}

	cfg := &Config{
		FS:  cfs,
		Dir: ".",
	}

	// Trap host FS access
	old := source.OsOpen
	source.OsOpen = func(name string) (fs.File, error) {
		called = true
		return nil, fs.ErrNotExist
	}
	defer func() { source.OsOpen = old }()

	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))

	qt.Assert(t, qt.IsFalse(called))
}

type assertNoSyntheticFS struct {
	t *testing.T
}

func (fsys assertNoSyntheticFS) Open(name string) (fs.File, error) {
	if strings.HasPrefix(name, loadFSRoot) {
		fsys.t.Fatalf("fs.FS.Open called with synthetic path %q", name)
	}
	return nil, fs.ErrNotExist
}

func TestLoaderDoesNotLeakSyntheticFSPrefix(t *testing.T) {
	ctx := cuecontext.New()

	cfg := &Config{
		FS:  assertNoSyntheticFS{t: t},
		Dir: ".",
	}

	for _, inst := range Instances([]string{"whatever.cue"}, cfg) {
		_ = ctx.BuildInstance(inst)
	}
}

func writeFile(t *testing.T, fpath string, content string) {
	err := os.MkdirAll(filepath.Dir(fpath), 0o777)
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(fpath, []byte(content), 0o666)
	qt.Assert(t, qt.IsNil(err))
}
