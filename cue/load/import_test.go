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
	"testing"

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

// Test that ModuleRoot can be at the root of the filesystem when
// using an overlay, and the loading should work just fine.
func TestOverlayModuleRoot(t *testing.T) {
	// Find the root directory; "/" on Unix-like systems,
	// something like "C:\\" on Windows.
	root, _ := os.Getwd()
	for {
		parent := filepath.Dir(root)
		if parent == root {
			break // reached the top
		}
		root = parent
	}
	t.Logf("root directory: %s", root)

	rooted := func(path string) string {
		return filepath.Join(root, path)
	}
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
