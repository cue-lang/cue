package load

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

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
	cfg, err := (&Config{
		Dir:     filepath.Join(dir, "foo"),
		Overlay: overlay,
	}).complete()
	qt.Assert(t, qt.IsNil(err))

	fsys, err := newOverlayFS(cfg)
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
	mapFS := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`module: "example.com/test@v0"
language: version: "v0.12.0"
`),
		},
		"x.cue": &fstest.MapFile{
			Data: []byte(`package test

a: 1
`),
		},
	}
	cfg := &Config{
		FS: mapFS,
	}
	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	qt.Assert(t, qt.Equals(insts[0].PkgName, "test"))
}

func TestLoadFromFSSubdir(t *testing.T) {
	mapFS := fstest.MapFS{
		"mymod/cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`module: "example.com/test@v0"
language: version: "v0.12.0"
`),
		},
		"mymod/x.cue": &fstest.MapFile{
			Data: []byte(`package test

a: 1
`),
		},
	}
	cfg := &Config{
		FS:  mapFS,
		Dir: "/mymod",
	}
	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	qt.Assert(t, qt.Equals(insts[0].PkgName, "test"))
}

func TestLoadFromFSAndOverlayMutualExclusion(t *testing.T) {
	cfg := &Config{
		FS:      fstest.MapFS{},
		Overlay: map[string]Source{"/foo": FromString("bar")},
	}
	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.ErrorMatches(insts[0].Err, `.*cannot set both Config.FS and Config.Overlay.*`))
}

func TestLoadFromFSFromFSPath(t *testing.T) {
	mapFS := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`module: "example.com/test@v0"
language: version: "v0.12.0"
`),
		},
		"x.cue": &fstest.MapFile{
			Data: []byte(`package test

a: 1
`),
		},
	}
	cfg := &Config{
		FS: mapFS,
		FromFSPath: func(p string) string {
			return "/real/source" + p
		},
	}
	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	// Check that file positions use the mapped path.
	qt.Assert(t, qt.HasLen(insts[0].BuildFiles, 1))
	qt.Assert(t, qt.Equals(insts[0].BuildFiles[0].Filename, "/real/source/x.cue"))
	// Check that Dir and Root use the mapped display paths.
	qt.Assert(t, qt.Equals(insts[0].Dir, "/real/source/"))
	qt.Assert(t, qt.Equals(insts[0].Root, "/real/source/"))
}

func TestFSLocPopulatedInOSCase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cue.mod", "module.cue"), `module: "example.com/test@v0"
language: version: "v0.12.0"
`)
	writeFile(t, filepath.Join(dir, "x.cue"), `package test
a: 1
`)
	cfg := &Config{
		Dir: dir,
	}
	insts := Instances([]string{"."}, cfg)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	// FSLoc fields should be populated even without Config.FS.
	qt.Assert(t, qt.IsTrue(insts[0].DirLoc.IsSet()))
	qt.Assert(t, qt.IsTrue(insts[0].RootLoc.IsSet()))
	qt.Assert(t, qt.Equals(insts[0].DirLoc.String(), insts[0].Dir))
	qt.Assert(t, qt.Equals(insts[0].RootLoc.String(), insts[0].Root))
	qt.Assert(t, qt.IsNotNil(insts[0].DirLoc.FS))
	// BuildFile FilenameLoc should also be populated.
	qt.Assert(t, qt.HasLen(insts[0].BuildFiles, 1))
	qt.Assert(t, qt.IsTrue(insts[0].BuildFiles[0].FilenameLoc.IsSet()))
	qt.Assert(t, qt.Equals(insts[0].BuildFiles[0].FilenameLoc.String(), insts[0].BuildFiles[0].Filename))
}

func writeFile(t *testing.T, fpath string, content string) {
	err := os.MkdirAll(filepath.Dir(fpath), 0o777)
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(fpath, []byte(content), 0o666)
	qt.Assert(t, qt.IsNil(err))
}
