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

package protobuf

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"github.com/kr/pretty"
)

var update = flag.Bool("update", false, "update the test output")

func TestParseDefinitions(t *testing.T) {
	testCases := []string{
		"networking/v1alpha3/gateway.proto",
		"mixer/v1/attributes.proto",
		"mixer/v1/config/client/client_config.proto",
	}
	for _, file := range testCases {
		t.Run(file, func(t *testing.T) {
			root := "testdata/istio.io/api"
			filename := filepath.Join(root, filepath.FromSlash(file))
			c := &Config{
				Paths: []string{"testdata", root},
			}

			out := &bytes.Buffer{}

			if f, err := Parse(filename, nil, c); err != nil {
				fmt.Fprintln(out, err)
			} else {
				b, _ := format.Node(f, format.Simplify())
				out.Write(b)
			}

			wantFile := filepath.Join("testdata", filepath.Base(file)+".out.cue")
			if *update {
				ioutil.WriteFile(wantFile, out.Bytes(), 0644)
				return
			}

			b, err := ioutil.ReadFile(wantFile)
			if err != nil {
				t.Fatal(err)
			}

			if desc := pretty.Diff(out.String(), string(b)); len(desc) > 0 {
				t.Errorf("files differ:\n%v", desc)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	cwd, _ := os.Getwd()
	root := filepath.Join(cwd, "testdata/istio.io/api")
	c := &Config{
		Root:   root,
		Module: "istio.io/api",
		Paths: []string{
			root,
			filepath.Join(cwd, "testdata"),
		},
	}

	b := NewBuilder(c)
	b.AddFile("networking/v1alpha3/gateway.proto", nil)
	b.AddFile("mixer/v1/attributes.proto", nil)
	b.AddFile("mixer/v1/mixer.proto", nil)
	b.AddFile("mixer/v1/config/client/client_config.proto", nil)

	files, err := b.Files()
	if err != nil {
		t.Fatal(err)
	}

	if *update {
		for _, f := range files {
			b, err := format.Node(f)
			if err != nil {
				t.Fatal(err)
			}
			_ = os.MkdirAll(filepath.Dir(f.Filename), 0755)
			err = ioutil.WriteFile(f.Filename, b, 0644)
			if err != nil {
				t.Fatal(err)
			}
		}
		return
	}

	gotFiles := map[string]*ast.File{}

	for _, f := range files {
		rel, err := filepath.Rel(cwd, f.Filename)
		if err != nil {
			t.Fatal(err)
		}
		gotFiles[rel] = f
	}

	filepath.Walk("testdata/istio.io/api", func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(path, ".cue") {
			return err
		}

		f := gotFiles[path]
		if f == nil {
			t.Errorf("did not produce file %q", path)
			return nil
		}
		delete(gotFiles, path)

		got, err := format.Node(f)
		if err != nil {
			t.Fatal(err)
		}

		want, err := ioutil.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("%s: files differ", path)
		}
		return nil
	})

	for filename := range gotFiles {
		t.Errorf("did not expect file %q", filename)
	}
}
