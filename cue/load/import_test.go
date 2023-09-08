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
	"cuelang.org/go/cue/token"
)

func testMod(dir string) string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(cwd, "testdata", dir)
}

func getInst(pkg, cwd string) (*build.Instance, error) {
	// Set ModuleRoot to cwd as well; otherwise we walk the parent directories
	// all the way to the root of the git repository, causing Go's test caching
	// to never kick in, as the .git directory almost always changes.
	// Moreover, it's extra work that isn't useful to the tests.
	c, err := (&Config{ModuleRoot: cwd, Dir: cwd}).complete()
	if err != nil {
		return nil, fmt.Errorf("unexpected error on Config.complete: %v", err)
	}
	l := loader{cfg: c}
	inst := l.newRelInstance(token.NoPos, pkg, c.Package)
	p := l.importPkg(token.NoPos, inst)[0]
	return p, p.Err
}

func TestEmptyImport(t *testing.T) {
	c, err := (&Config{
		ModuleRoot: ".",
	}).complete()
	if err != nil {
		t.Fatal(err)
	}
	l := loader{cfg: c}
	inst := l.newInstance(token.NoPos, "")
	p := l.importPkg(token.NoPos, inst)[0]
	if p.Err == nil {
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
	path := filepath.Join(testMod("testmod"), "empty")
	_, err := getInst(".", path)
	if _, ok := err.(*NoFilesError); !ok {
		t.Fatalf(`Import(%q) did not return NoCUEError.`, path)
	}
}

func TestMultiplePackageImport(t *testing.T) {
	path := filepath.Join(testMod("testmod"), "multi")
	_, err := getInst(".", path)
	mpe, ok := err.(*MultiplePackageError)
	if !ok {
		t.Fatalf(`Import(%q) did not return MultiplePackageError.`, path)
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
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	p, err := getInst(".", cwd)
	if err != nil {
		t.Fatal(err)
	}

	if p.DisplayPath != "." {
		t.Fatalf("DisplayPath=%q, want %q", p.DisplayPath, ".")
	}
}
