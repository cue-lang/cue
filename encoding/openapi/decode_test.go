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
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuetest"
)

// TestDecode reads the testdata/script/*.txtar files, converts the
// contained JSON (or YAML) schema to CUE and compares it against the
// output.
//
// The first .json or .yaml file in the txtar archive (with no / in
// the name) is used as the input. The first .cue file in the archive
// (with no / in the name) is used as the expected output.
//
// A "#targetLanguageVersion: <version>" line in the archive comment selects
// the CUE language version to extract for and to verify the output at.
//
// Set CUE_UPDATE=1 to update test files with the corresponding output.
func TestDecode(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir("testdata/script", func(fullpath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(fullpath, ".txtar") {
			return nil
		}

		t.Run(fullpath, func(t *testing.T) {
			t.Parallel()
			a, err := txtar.ParseFile(fullpath)
			if err != nil {
				t.Fatal(err)
			}

			cfg := &openapi.Config{PkgName: "foo"}
			for line := range strings.Lines(string(a.Comment)) {
				if v, ok := strings.CutPrefix(line, "#targetLanguageVersion:"); ok {
					cfg.TargetLanguageVersion = strings.TrimSpace(v)
				}
			}

			var inFile *ast.File
			var out, errout []byte
			outIndex := -1

			for i, f := range a.Files {
				if strings.Contains(f.Name, "/") {
					continue
				}
				switch path.Ext(f.Name) {
				case ".json":
					if inFile == nil {
						var inExpr ast.Expr
						inExpr, err = json.Extract(f.Name, f.Data)
						inFile = internal.ToFile(inExpr, false)
					}
				case ".yaml":
					if inFile == nil {
						inFile, err = yaml.Extract(f.Name, f.Data)
					}
				case ".cue":
					if out == nil {
						out = f.Data
						outIndex = i
					}
				case ".err":
					if errout == nil {
						errout = f.Data
					}
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
			qt.Assert(t, qt.DeepEquals(got, errout))

			if gotFile != nil {
				// verify the generated CUE.
				v := ctx.BuildFile(gotFile, cue.Filename(fullpath))
				if err := v.Err(); err != nil {
					t.Fatal(errors.Details(err, nil))
				}

				b, err := format.Node(gotFile)
				if err != nil {
					t.Fatal(err)
				}
				// The syntax must parse at the language version it targets.
				_, err = parser.ParseFile(fullpath, b,
					parser.Version(cfg.TargetLanguageVersion))
				if err != nil {
					t.Fatal(errors.Details(err, nil))
				}

				b = bytes.TrimSpace(b)
				out = bytes.TrimSpace(out)

				if cuetest.UpdateGolden(t, a.Files[outIndex].Name, out, b) {
					a.Files[outIndex].Data = b
					cuetest.WriteGoldenFile(t, fullpath, txtar.Format(a))
				}
			}
		})
		return nil
	})
	qt.Assert(t, qt.IsNil(err))
}

// TestExtractNilConfig checks that [openapi.Extract] accepts a nil config,
// as [openapi.ExtractV2] and [openapi.Generate] do.
func TestExtractNilConfig(t *testing.T) {
	t.Parallel()
	v := cuecontext.New().CompileString(`
openapi: "3.0.0"
info: {title: "t", version: "1"}
components: schemas: Foo: type: "string"
`)
	// TODO: a nil config panics.
	defer func() {
		qt.Assert(t, qt.Matches(fmt.Sprint(recover()), `.*nil pointer dereference`))
	}()
	_, err := openapi.Extract(v, nil)
	qt.Assert(t, qt.IsNil(err))
}
