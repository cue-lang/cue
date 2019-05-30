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

package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retr0h/go-gilt/copy"
)

func TestGetGo(t *testing.T) {
	// Leave the current working directory outside the testdata directory
	// so that Go loader finds the Go mod file and creates a proper path.
	// We need to trick the command to generate the data within the testdata
	// directory, though.
	tmp, err := ioutil.TempDir("", "cue_get_go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	cueTestRoot = tmp

	// We don't use runCommand here, as we are interested in generated packages.
	cmd := newGoCmd()
	cmd.SetArgs([]string{"./testdata/code/go/..."})
	err = cmd.Execute()
	if err != nil {
		log.Fatal(err)
	}

	// Packages will generate differently in modules versus GOPATH. Search
	// for the common ground to not have breaking text if people run these
	// test in GOPATH mode.
	root := ""
	filepath.Walk(tmp, func(path string, info os.FileInfo, err error) error {
		if root != "" {
			return filepath.SkipDir
		}
		if filepath.Base(path) == "cuelang.org" {
			root = filepath.Dir(path)
			return filepath.SkipDir
		}
		return nil
	})

	const dst = "testdata/pkg"

	if *update {
		os.RemoveAll(dst)
		err := copy.Dir(filepath.Join(root), dst)
		if err != nil {
			t.Fatal(err)
		}
		t.Skip("files updated")
	}

	prefix := "testdata/pkg/cuelang.org/go/cmd/cue/cmd/testdata/code/go/"
	filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		t.Run(path, func(t *testing.T) {
			want := loadFile(t, path)
			got := loadFile(t, filepath.Join(root, path[len(dst):]))

			if want != got {
				t.Errorf("contexts for file %s differ", path[len(prefix):])
			}
		})
		return nil
	})
}

func loadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("could not load file %s", path)
	}
	// Strip comments up till package clause. Local packages will generate
	// differently using GOPATH versuse modules.
	s := string(b)
	return s[strings.Index(s, "package"):]
}
