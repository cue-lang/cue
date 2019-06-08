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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	build "cuelang.org/go/cue/build"
	"cuelang.org/go/cue/token"
)

const testdata = "./testdata/"

func getInst(pkg, cwd string) (*build.Instance, error) {
	c, _ := (&Config{}).complete()
	l := loader{cfg: c}
	p := l.importPkg(token.NoPos, pkg, cwd)
	return p, p.Err
}

func TestDotSlashImport(t *testing.T) {
	c, _ := (&Config{}).complete()
	l := loader{cfg: c}
	p := l.importPkg(token.NoPos, ".", testdata+"other")
	errl := p.Err
	if errl != nil {
		t.Fatal(errl)
	}
	if len(p.ImportPaths) != 1 || p.ImportPaths[0] != "./file" {
		t.Fatalf("testdata/other: Imports=%v, want [./file]", p.ImportPaths)
	}

	p1, err := getInst("./file", testdata+"other")
	if err != nil {
		t.Fatal(err)
	}
	if p1.PkgName != "file" {
		t.Fatalf("./file: Name=%q, want %q", p1.PkgName, "file")
	}
	dir := filepath.Clean(testdata + "other/file") // Clean to use \ on Windows
	if p1.Dir != dir {
		t.Fatalf("./file: Dir=%q, want %q", p1.PkgName, dir)
	}
}

func TestEmptyImport(t *testing.T) {
	p, err := getInst("", "")
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
	_, err := getInst(".", testdata+"empty")
	if _, ok := err.(*noCUEError); !ok {
		t.Fatal(`Import("testdata/empty") did not return NoCUEError.`)
	}
}

func TestIgnoredCUEFilesImport(t *testing.T) {
	_, err := getInst(".", testdata+"ignored")
	e, ok := err.(*noCUEError)
	if !ok {
		t.Fatal(`Import("testdata/ignored") did not return NoCUEError.`)
	}
	if !e.Ignored {
		t.Fatal(`Import("testdata/ignored") should have ignored CUE files.`)
	}
}

func TestMultiplePackageImport(t *testing.T) {
	_, err := getInst(".", testdata+"multi")
	mpe, ok := err.(*multiplePackageError)
	if !ok {
		t.Fatal(`Import("testdata/multi") did not return MultiplePackageError.`)
	}
	want := &multiplePackageError{
		Dir:      filepath.FromSlash("testdata/multi"),
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
