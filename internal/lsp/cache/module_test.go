// Copyright 2026 The CUE Authors
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

package cache

import (
	"path/filepath"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

func TestAscendantCuePackages(t *testing.T) {
	root := t.TempDir()
	// With the old module system, a package can have several dirs.
	dirs := func(pkgPath string) []protocol.DocumentURI {
		return []protocol.DocumentURI{
			protocol.URIFromPath(filepath.Join(root, "cue.mod", "gen", pkgPath)),
			protocol.URIFromPath(filepath.Join(root, "cue.mod", "pkg", pkgPath)),
		}
	}
	foo := &Package{
		importPath: ast.ParseImportPath("example.com/foo"),
		dirURIs:    dirs("example.com/foo"),
		isCue:      true,
	}
	sub := &Package{
		importPath: ast.ParseImportPath("example.com/foo/sub"),
		dirURIs:    dirs("example.com/foo/sub"),
	}
	m := &Module{packages: map[ast.ImportPath]*Package{
		foo.importPath: foo,
		sub.importPath: sub,
	}}

	// Each of foo's dirs encloses one of sub's dirs; foo is yielded once.
	var got []string
	for p := range m.ascendantCuePackages(sub) {
		got = append(got, p.importPath.String())
	}
	qt.Assert(t, qt.DeepEquals(got, []string{"example.com/foo"}))
}
