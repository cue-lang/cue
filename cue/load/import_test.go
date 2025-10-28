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

// TestRootModuleDirectory tests that when the module root is "/" (filesystem root),
// the package paths are constructed correctly. This is a regression test for an
// off-by-one error where paths like "mod.exampleschema" were generated instead of
// "mod.example/schema" due to missing path separators.
func TestRootModuleDirectory(t *testing.T) {
	// Create an overlay with files that simulate a module at the root directory
	overlay := map[string]Source{
		"/cue.mod/module.cue": FromString(`module: "mod.example@v0"
language: version: "v0.11.0"
`),
		"/root.cue":          FromString(`package config

#stringType: string`),
		"/schema/schema.cue": FromString(`package config

#Person: {
	name?: #stringType
}`),
	}

	conf := &Config{
		Dir:        "/",
		ModuleRoot: "/",
		Overlay:    overlay,
	}

	// Load the schema package
	insts := Instances([]string{"./schema"}, conf)
	if len(insts) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(insts))
	}

	inst := insts[0]
	if inst.Err != nil {
		t.Fatalf("unexpected error loading instance: %v", inst.Err)
	}

	// The import path should be "mod.example/schema@v0:config" (or similar),
	// not "mod.exampleschema@v0:config" which would indicate the bug.
	// The key test is that "mod.example" and "schema" are separated by a slash.
	wantModule := "mod.example@v0"
	if inst.Module != wantModule {
		t.Errorf("Module = %q, want %q", inst.Module, wantModule)
	}

	// Check that the path is correctly formed with proper separator
	wantPath := "mod.example/schema@v0:config"
	if inst.ImportPath != wantPath {
		t.Errorf("ImportPath = %q, want %q", inst.ImportPath, wantPath)
	}

	// Additional check: ensure the buggy form is NOT present
	if inst.ImportPath == "mod.exampleschema@v0:config" {
		t.Error("ImportPath has the buggy form without proper separator")
	}
}

// TestNonRootModuleDirectory tests that the normal case (non-root module directory)
// continues to work correctly after the fix for root directory handling.
func TestNonRootModuleDirectory(t *testing.T) {
	// Create an overlay with files in a non-root directory
	overlay := map[string]Source{
		"/tmp/testmod/cue.mod/module.cue": FromString(`module: "mod.example@v0"
language: version: "v0.11.0"
`),
		"/tmp/testmod/root.cue": FromString(`package config

#stringType: string`),
		"/tmp/testmod/schema/schema.cue": FromString(`package config

#Person: {
	name?: #stringType
}`),
	}

	conf := &Config{
		Dir:        "/tmp/testmod",
		ModuleRoot: "/tmp/testmod",
		Overlay:    overlay,
	}

	// Load the schema package
	insts := Instances([]string{"./schema"}, conf)
	if len(insts) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(insts))
	}

	inst := insts[0]
	if inst.Err != nil {
		t.Fatalf("unexpected error loading instance: %v", inst.Err)
	}

	// Verify the module and import paths are correct
	wantModule := "mod.example@v0"
	if inst.Module != wantModule {
		t.Errorf("Module = %q, want %q", inst.Module, wantModule)
	}

	wantPath := "mod.example/schema@v0:config"
	if inst.ImportPath != wantPath {
		t.Errorf("ImportPath = %q, want %q", inst.ImportPath, wantPath)
	}

	// Additional check: ensure the buggy form is NOT present
	if inst.ImportPath == "mod.exampleschema@v0:config" {
		t.Error("ImportPath has the buggy form without proper separator")
	}
}
