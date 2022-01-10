// Copyright 2019 CUE Authors
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

package file

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/task"
	"cuelang.org/go/internal/value"
)

func parse(t *testing.T, kind, expr string) cue.Value {
	t.Helper()

	x, err := parser.ParseExpr("test", expr)
	if err != nil {
		t.Fatal(err)
	}
	var r cue.Runtime
	i, err := r.CompileExpr(x)
	if err != nil {
		t.Fatal(err)
	}
	return value.UnifyBuiltin(i.Value(), kind)
}

func TestRead(t *testing.T) {
	v := parse(t, "tool/file.Read", `{filename: "testdata/input.foo"}`)
	got, err := (*cmdRead).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"contents": []byte("This is a test.")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v; want %v", got, want)
	}

	v = parse(t, "tool/file.Read", `{
		filename: "testdata/input.foo"
		contents: string
	}`)
	got, err = (*cmdRead).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]interface{}{"contents": "This is a test."}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v; want %v", got, want)
	}
}

func TestAppend(t *testing.T) {
	f, err := ioutil.TempFile("", "filetest")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	defer os.Remove(name)
	f.Close()
	name = filepath.ToSlash(name)

	v := parse(t, "tool/file.Append", fmt.Sprintf(`{
		filename: "%s"
		contents: "This is a test."
	}`, name))
	_, err = (*cmdAppend).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}

	b, err := ioutil.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(b), "This is a test."; got != want {
		t.Errorf("got %v; want %v", got, want)
	}
}

func TestCreate(t *testing.T) {
	f, err := ioutil.TempFile("", "filetest")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	defer os.Remove(name)
	f.Close()
	name = filepath.ToSlash(name)

	v := parse(t, "tool/file.Create", fmt.Sprintf(`{
		filename: "%s"
		contents: "This is a test."
	}`, name))
	_, err = (*cmdCreate).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}

	b, err := ioutil.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(b), "This is a test."; got != want {
		t.Errorf("got %v; want %v", got, want)
	}
}

func TestGlob(t *testing.T) {
	v := parse(t, "tool/file.Glob", fmt.Sprintf(`{
		glob: "testdata/input.*"
	}`))
	got, err := (*cmdGlob).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]interface{}{"files": []string{"testdata/input.foo"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v; want %v", got, want)
	}
}

func TestMkdir(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// simple dir creation
	d1 := filepath.Join(baseDir, "foo")
	v := parse(t, "tool/file.Mkdir", fmt.Sprintf(`{path: #"%s"#}`, d1))
	_, err = (*cmdMkdir).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(d1)
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.IsDir() {
		t.Fatal("not a directory")
	}

	// dir already exists
	v = parse(t, "tool/file.Mkdir", fmt.Sprintf(`{path: #"%s"#}`, d1))
	_, err = (*cmdMkdir).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}

	// create parents
	// set permissions
	d2 := filepath.Join(baseDir, "bar/x")
	v = parse(t, "tool/file.MkdirAll", fmt.Sprintf(`{path: #"%s"#, permissions: 0o700}`, d2))
	_, err = (*cmdMkdir).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(d2)
	if err != nil {
		t.Fatal(err)
	}
	if !fi2.IsDir() {
		t.Fatal("not a directory")
	}

	// file at same path
	f, err := ioutil.TempFile(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	v = parse(t, "tool/file.Mkdir", fmt.Sprintf(`{path: #"%s"#}`, f.Name()))
	_, err = (*cmdMkdir).Run(nil, &task.Context{Obj: v})
	if err == nil {
		t.Fatal("should not create directory at existing filepath")
	}
}

func TestMkdirTemp(t *testing.T) {
	// create temp dir
	v := parse(t, "tool/file.MkdirTemp", "{}")
	r, err := (*cmdMkdirTemp).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := r.(map[string]interface{})["path"]; !exists {
		t.Fatal("no directory path returned")
	}
	path := r.(map[string]interface{})["path"].(string)
	defer os.RemoveAll(path)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() {
		t.Fatal("not a directory")
	}

	// removes temp dir
	v2 := parse(t, "tool/file.RemoveAll", fmt.Sprintf(`{path: #"%s"#}`, path))
	_, err = (*cmdRemoveAll).Run(nil, &task.Context{Obj: v2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(path)
	if err == nil {
		t.Fatal(err)
	}

}
