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
	"slices"
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

// Here we test that every unique ImportPath has exactly one
// build.Instance, regardless of whether that build.Instance is
// constructed by a package import, or directly as a result of
// arguments to load.Instances.
func TestImportPaths(t *testing.T) {
	insts := Instances([]string{"./..."}, &Config{
		Package:    "*",
		ModuleRoot: filepath.Join(pkgRootDir, testdata("testmod")),
		Dir:        testdata("testmod", "importpaths"),
	})

	uniqueInstances := make(map[*build.Instance]struct{})
	byImportPath := make(map[string]*build.Instance)
	var walkImports func(inst *build.Instance)
	walkImports = func(inst *build.Instance) {
		uniqueInstances[inst] = struct{}{}

		existing := byImportPath[inst.ImportPath]
		if existing == nil {
			byImportPath[inst.ImportPath] = inst
		} else if existing == inst {
			return
		} else {
			t.Fatalf("Got 2 different instances for the same import path: %q", inst.ImportPath)
		}

		for _, ipt := range inst.Imports {
			walkImports(ipt)
		}
	}

	// packages: a:a, b:b, c:c, d:d, d:e,
	qt.Assert(t, qt.HasLen(insts, 5))

	for _, inst := range insts {
		// All files have explicit packages, so there should be no use
		// of the "empty" package "_".
		qt.Assert(t, qt.Not(qt.Equals(inst.PkgName, "_")))
		walkImports(inst)
	}

	importPaths := make([]string, 0, len(byImportPath))
	for path, inst := range byImportPath {
		importPaths = append(importPaths, path)
		qt.Assert(t, qt.HasLen(inst.BuildFiles, 1))

		// require every import path has a different instance:
		_, found := uniqueInstances[inst]
		qt.Assert(t, qt.IsTrue(found))
		delete(uniqueInstances, inst)
	}
	slices.Sort(importPaths)

	qt.Assert(t, qt.DeepEquals(importPaths, []string{
		"mod.test/test/importpaths/a@v0:a",
		"mod.test/test/importpaths/b@v0:b",
		"mod.test/test/importpaths/c@v0:c",
		"mod.test/test/importpaths/d",      // a/a.cue imports it this way
		"mod.test/test/importpaths/d@v0",   // b/b.cue imports it this way
		"mod.test/test/importpaths/d@v0:d", // c/c.cue imports it this way
		"mod.test/test/importpaths/d@v0:e",
	}))
}
