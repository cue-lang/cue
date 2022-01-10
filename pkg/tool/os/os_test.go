// Copyright 2022 CUE Authors
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

package os

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"cuelang.org/go/internal/task"
)

func TestMkdir(t *testing.T) {
	d1 := "/tmp/foo"
	defer os.RemoveAll(d1)
	v := parse(t, "tool/os.Mkdir", fmt.Sprintf(`{path: "%s"}`, d1))
	_, err := (*mkdirCmd).Run(nil, &task.Context{Obj: v})
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
	// already exists
	v = parse(t, "tool/os.Mkdir", fmt.Sprintf(`{path: "%s"}`, d1))
	_, err = (*mkdirCmd).Run(nil, &task.Context{Obj: v})
	if err != nil {
		t.Fatal(err)
	}
	// create parents
	d2 := "/tmp/bar/x"
	defer os.RemoveAll(d2)
	v = parse(t, "tool/os.Mkdir", fmt.Sprintf(`{path: "%s", createParents: true}`, d2))
	_, err = (*mkdirCmd).Run(nil, &task.Context{Obj: v})
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
	f, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	v = parse(t, "tool/os.Mkdir", fmt.Sprintf(`{path: "%s"}`, f.Name()))
	_, err = (*mkdirCmd).Run(nil, &task.Context{Obj: v})
	if err == nil {
		t.Fatal("should not create directory at existing filepath")
	}
}
