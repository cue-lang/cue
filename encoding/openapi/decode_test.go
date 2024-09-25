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
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

// TestDecode reads the testdata/*.txtar files, converts the contained
// JSON schema to CUE and compares it against the output.
//
// Set CUE_UPDATE=1 to update test files with the corresponding output.
func TestDecode(t *testing.T) {
	err := filepath.WalkDir("testdata/script", func(fullpath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(fullpath, ".txtar") {
			return nil
		}

		t.Run(fullpath, func(t *testing.T) {
			a, err := txtar.ParseFile(fullpath)
			if err != nil {
				t.Fatal(err)
			}

			cfg := &openapi.Config{PkgName: "foo"}

			var inFile *ast.File
			var out, errout []byte
			outIndex := -1

			for i, f := range a.Files {
				switch path.Ext(f.Name) {
				case ".json":
					var inExpr ast.Expr
					inExpr, err = json.Extract(f.Name, f.Data)
					inFile = internal.ToFile(inExpr)
				case ".yaml":
					inFile, err = yaml.Extract(f.Name, f.Data)
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
			ctx := cuecontext.New()
			in := ctx.BuildFile(inFile)
			if err := in.Err(); err != nil {
				t.Fatal(err)
			}

			gotFile, err := openapi.Extract(in, cfg)
			if err != nil && errout == nil {
				t.Fatal(errors.Details(err, nil))
			}
			got := []byte(nil)
			if err != nil {
				got = []byte(err.Error())
			}
			if diff := cmp.Diff(errout, got); diff != "" {
				t.Error(diff)
			}

			if gotFile != nil {
				// verify the generated CUE.
				v := ctx.BuildFile(gotFile, cue.Filename(fullpath))
				if err := v.Err(); err != nil {
					t.Fatal(errors.Details(err, nil))
				}

				b, err := format.Node(gotFile, format.Simplify())
				if err != nil {
					t.Fatal(err)
				}

				b = bytes.TrimSpace(b)
				out = bytes.TrimSpace(out)

				if diff := cmp.Diff(b, out); diff != "" {
					t.Error(diff)
					if cuetest.UpdateGoldenFiles {
						a.Files[outIndex].Data = b
						b = txtar.Format(a)
						err = os.WriteFile(fullpath, b, 0666)
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
	qt.Assert(t, qt.IsNil(err))
}
