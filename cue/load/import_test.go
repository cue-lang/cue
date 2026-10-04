// Copyright 2018 The CUE Authors
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

package load

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"cuelang.org/go/cue/build"
	"github.com/go-quicktest/qt"
)

func testdata(elems ...string) string {
	return filepath.Join(append([]string{"testdata"}, elems...)...)
}

var pkgRootDir, _ = os.Getwd()

func getInst(pkg, cwd string) (*build.Instance, error) {
	insts := Instances([]string{pkg}, &Config{
		// Set ModuleRoot as well; otherwise we walk the parent directories
		// all the way to the root of the git repository, causing Go's test caching
		// to never kick in, as the .git directory almost always changes.
		// Moreover, it's extra work that isn't useful to the tests.
		//
		// Note that we can't set ModuleRoot to cwd because if ModuleRoot is
		// set, the logic will only look for a module file in that exact directory.
		// So we set it to the module root actually used by all the callers of getInst: ./testdata/testmod.
		ModuleRoot: filepath.Join(pkgRootDir, testdata("testmod")),
		Dir:        cwd,
	})
	if len(insts) != 1 {
		return nil, fmt.Errorf("expected one instance, got %d", len(insts))
	}
	inst := insts[0]
	return inst, inst.Err
}

func TestEmptyImport(t *testing.T) {
	path := testdata("testmod", "hello")
	p, err := getInst("", path)
	if err == nil {
		t.Fatal(`Import("") returned nil error.`)
	}
	if p == nil {
		t.Fatal(`Import("") returned nil package.`)
	}
	if p.DisplayPath != "" {
		t.Fatalf("DisplayPath=%q, want %q.", p.DisplayPath, "")
	}
}

func TestEmptyFolderImport(t *testing.T) {
	path := testdata("testmod", "empty")
	_, err := getInst(".", path)
	if _, ok := err.(*NoFilesError); !ok {
		t.Fatalf(`Import(%q) did not return NoCUEError, but instead %#v (%v)`, path, err, err)
	}
}

func TestMultiplePackageImport(t *testing.T) {
	path := testdata("testmod", "multi")
	_, err := getInst(".", path)
	mpe, ok := err.(*MultiplePackageError)
	if !ok {
		t.Fatalf(`Import(%q) did not return MultiplePackageError, but instead %#v (%v)`, path, err, err)
	}
	mpe.Dir = ""
	want := &MultiplePackageError{
		Packages: []string{"main", "test_package"},
		Files:    []string{"file.cue", "file_appengine.cue"},
	}
	if !reflect.DeepEqual(mpe, want) {
		t.Errorf("got %#v; want %#v", mpe, want)
	}
}

func TestLocalDirectory(t *testing.T) {
	p, err := getInst(".", testdata("testmod", "hello"))
	if err != nil {
		t.Fatal(err)
	}

	if p.DisplayPath != "." {
		t.Fatalf("DisplayPath=%q, want %q", p.DisplayPath, ".")
	}
}

// rooted joins path to the root directory of the filesystem;
// "/" on Unix-like systems, something like "C:\\" on Windows.
func rooted(path string) string {
	return filepath.Join(filepath.VolumeName(pkgRootDir)+string(filepath.Separator), path)
}

// Test that ModuleRoot can be at the root of the filesystem when
// using an overlay, and the loading should work just fine.
func TestOverlayModuleRoot(t *testing.T) {
	conf := &Config{
		Dir:        rooted(""),
		ModuleRoot: rooted(""),
		Overlay: map[string]Source{
			rooted("cue.mod/module.cue"): FromString(`
module: "mod.test@v0"
language: version: "v0.11.0"
`),
			rooted("root.cue"):       FromString(`package root`),
			rooted("pkgdir/pkg.cue"): FromString(`package pkgname`),
		},
	}
	insts := Instances([]string{"."}, conf)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	qt.Assert(t, qt.Equals(insts[0].Module, "mod.test@v0"))
	qt.Assert(t, qt.Equals(insts[0].ImportPath, "mod.test@v0:root"))

	insts = Instances([]string{"./pkgdir"}, conf)
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	qt.Assert(t, qt.Equals(insts[0].Module, "mod.test@v0"))
	qt.Assert(t, qt.Equals(insts[0].ImportPath, "mod.test/pkgdir@v0:pkgname"))
}

// TestSiblingDirWithModuleRootPrefix checks that a directory whose path
// has the module root as a string prefix, but not as a path prefix,
// is reported as outside of the module.
func TestSiblingDirWithModuleRootPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		// TODO: the wrong results asserted below do not reproduce on Windows.
		t.Skip("the sibling directory is already seen as outside the module on Windows")
	}
	const timedOut = "Instances did not return"
	tests := []struct {
		name      string
		arg       string
		parentMod bool
		want      string
	}{{
		name: "Unrelated",
		arg:  "../other",
		want: `cannot determine import path for "../other" (dir outside of root)`,
	}, {
		name: "SharedPrefix",
		arg:  "../foobar",
		want: timedOut, // TODO: should fail with "dir outside of root"
	}, {
		name: "SharedPrefixPattern",
		arg:  "../foobar/...",
		want: timedOut, // TODO: should fail with "dir outside of root"
	}, {
		name:      "SharedPrefixParentModule",
		arg:       "../foobar",
		parentMod: true,
		want:      `cannot determine import path for "../foobar" (directory is in a nested module)`, // TODO: should fail with "dir outside of root"
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			overlay := map[string]Source{
				rooted("r/foo/cue.mod/module.cue"): FromString(`module: "mod.test@v0", language: version: "v0.11.0"`),
				rooted("r/foo/x.cue"):              FromString(`package x`),
				rooted("r/foobar/x.cue"):           FromString(`package x`),
				rooted("r/other/x.cue"):            FromString(`package x`),
			}
			if test.parentMod {
				overlay[rooted("r/cue.mod/module.cue")] = FromString(`module: "parent.test@v0", language: version: "v0.11.0"`)
			}
			done := make(chan string, 1)
			go func() {
				insts := Instances([]string{test.arg}, &Config{Dir: rooted("r/foo"), Overlay: overlay})
				done <- fmt.Sprint(insts[0].Err)
			}()
			got := timedOut
			select {
			case got = <-done:
			case <-time.After(2 * time.Second):
			}
			qt.Assert(t, qt.Equals(got, test.want))
		})
	}
}
