// Copyright 2023 CUE Authors
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

package modimports

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/internal/txtarfs"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"
)

func TestAllPackageFiles(t *testing.T) {
	files, err := filepath.Glob("testdata/*.txtar")
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			ar, err := txtar.ParseFile(f)
			qt.Assert(t, qt.IsNil(err))
			tfs := txtarfs.FS(ar)
			want, err := fs.ReadFile(tfs, "want")
			qt.Assert(t, qt.IsNil(err))
			iter := AllModuleFiles(tfs, ".")
			var out strings.Builder
			iter(func(pf ModuleFile, err error) bool {
				out.WriteString(pf.FilePath)
				if err != nil {
					fmt.Fprintf(&out, ": error: %v\n", err)
					return true
				}
				for _, imp := range pf.Syntax.Imports {
					fmt.Fprintf(&out, " %s", imp.Path.Value)
				}
				out.WriteString("\n")
				return true
			})
			if diff := cmp.Diff(string(want), out.String()); diff != "" {
				t.Fatalf("unexpected results (-want +got):\n%s", diff)
			}
			wantImports, err := fs.ReadFile(tfs, "want-imports")
			qt.Assert(t, qt.IsNil(err))
			out.Reset()
			imports, err := AllImports(AllModuleFiles(tfs, "."))
			if err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			} else {
				for _, imp := range imports {
					fmt.Fprintln(&out, imp)
				}
			}
			if diff := cmp.Diff(string(wantImports), out.String()); diff != "" {
				t.Fatalf("unexpected results for ImportsForModuleFiles (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPackageFiles(t *testing.T) {
	dirContents := txtar.Parse([]byte(`
-- x1.cue --
package x

import (
	"foo"
	"bar.com/baz"
)
-- x2.cue --
package x

import (
	"something.else"
)
-- foo/y.cue --
import "other"
-- omitted.go --
-- omitted --
-- y.cue --
package y
`))
	tfs := txtarfs.FS(dirContents)
	imps, err := AllImports(PackageFiles(tfs, "."))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{"bar.com/baz", "foo", "something.else"}))
	imps, err = AllImports(PackageFiles(tfs, "foo"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(imps, []string{"other"}))
}
