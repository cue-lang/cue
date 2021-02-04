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

package openapi_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal/cuetest"
)

// TestDecode reads the testdata/*.txtar files, converts the contained
// JSON schema to CUE and compares it against the output.
//
// Set CUE_UPDATE=1 to update test files with the corresponding output.
func TestDecode(t *testing.T) {
	err := filepath.Walk("testdata/script", func(fullpath string, info os.FileInfo, err error) error {
		_ = err
		if !strings.HasSuffix(fullpath, ".txtar") {
			return nil
		}

		t.Run(fullpath, func(t *testing.T) {
			a, err := txtar.ParseFile(fullpath)
			if err != nil {
				t.Fatal(err)
			}

			cfg := &openapi.Config{PkgName: "foo"}

			r := &cue.Runtime{}
			var in *cue.Instance
			var out, errout []byte
			outIndex := -1

			for i, f := range a.Files {
				switch path.Ext(f.Name) {
				case ".json":
					in, err = json.Decode(r, f.Name, f.Data)
				case ".yaml":
					in, err = yaml.Decode(r, f.Name, f.Data)
				case ".cue":
					out = f.Data
					outIndex = i
				case ".err":
					errout = f.Data
				}
			}
			if err != nil {
				t.Fatal(err)
			}

			expr, err := openapi.Extract(in, cfg)
			if err != nil && errout == nil {
				t.Fatal(errors.Details(err, nil))
			}
			got := []byte(nil)
			if err != nil {
				got = []byte(err.Error())
			}
			if !cmp.Equal(errout, got) {
				t.Error(cmp.Diff(string(got), string(errout)))
			}

			if expr != nil {
				b, err := format.Node(expr, format.Simplify())
				if err != nil {
					t.Fatal(err)
				}

				// verify the generated CUE.
				if _, err = r.Compile(fullpath, b); err != nil {
					t.Fatal(errors.Details(err, nil))
				}

				b = bytes.TrimSpace(b)
				out = bytes.TrimSpace(out)

				if !cmp.Equal(b, out) {
					if cuetest.UpdateGoldenFiles {
						a.Files[outIndex].Data = b
						b = txtar.Format(a)
						err = ioutil.WriteFile(fullpath, b, 0644)
						if err != nil {
							t.Fatal(err)
						}
						return
					}
					t.Error(cmp.Diff(string(out), string(b)))
				}
			}
		})
		return nil
	})
	assert.NoError(t, err)
}
