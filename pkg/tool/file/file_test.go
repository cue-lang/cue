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
	"reflect"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

func parse(t *testing.T, kind, expr string) cue.Value {
	t.Helper()
	fset := token.NewFileSet()

	x, err := parser.ParseExpr(fset, "test", expr)
	if err != nil {
		t.Fatal(err)
	}
	i, err := cue.FromExpr(fset, x)
	if err != nil {
		t.Fatal(err)
	}
	return internal.UnifyBuiltin(i.Value(), kind).(cue.Value)
}
func TestRead(t *testing.T) {
	v := parse(t, "tool/file.Read", `{filename: "testdata/input.foo"}`)
	got, err := (*cmdRead).Run(nil, nil, v)
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
	got, err = (*cmdRead).Run(nil, nil, v)
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

	v := parse(t, "tool/file.Append", fmt.Sprintf(`{
		filename: "%s"
		contents: "This is a test."
	}`, name))
	_, err = (*cmdAppend).Run(nil, nil, v)
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

	v := parse(t, "tool/file.Create", fmt.Sprintf(`{
		filename: "%s"
		contents: "This is a test."
	}`, name))
	_, err = (*cmdCreate).Run(nil, nil, v)
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
	got, err := (*cmdGlob).Run(nil, nil, v)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"testdata/input.foo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v; want %v", got, want)
	}
}
