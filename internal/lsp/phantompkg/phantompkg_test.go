// Copyright 2026 CUE Authors
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

package phantompkg_test

import (
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/lsp/phantompkg"
)

func TestPhantomPackage(t *testing.T) {
	// A declaration the file's author wrote is not a phantom, whatever
	// its name: it has a position.
	name := phantompkg.Name("x.cue")
	for _, src := range []string{"package p\n\na: 1\n", "package " + name + "\n\na: 1\n"} {
		file, err := parser.ParseFile("x.cue", src)
		qt.Assert(t, qt.IsNil(err))
		pkg := file.Decls[0].(*ast.Package)
		qt.Assert(t, qt.IsFalse(phantompkg.Is(pkg)))
		qt.Assert(t, qt.Equals(phantompkg.RemoveDecl(file), ast.Node(file)))
	}

	// Nor is a declaration without a name, or with another name which
	// has no position.
	qt.Assert(t, qt.IsFalse(phantompkg.Is(&ast.Package{})))
	qt.Assert(t, qt.IsFalse(phantompkg.Is(&ast.Package{Name: ast.NewIdent("p")})))
	qt.Assert(t, qt.IsFalse(phantompkg.Is(&ast.Package{Name: ast.NewIdent(name[1:] + "x")})))

	// An injected declaration is one, for any file name, and is removed
	// from a copy of the file: the file given is left as it was.
	qt.Assert(t, qt.Not(qt.Equals(phantompkg.Name("y.cue"), name)))
	file, err := parser.ParseFile("x.cue", "// A comment.\n\na: 1\n")
	qt.Assert(t, qt.IsNil(err))
	phantom := &ast.Package{Name: ast.NewIdent(name)}
	qt.Assert(t, qt.IsTrue(phantompkg.Is(phantom)))
	fields := len(file.Decls)
	file.Decls = append([]ast.Decl{phantom}, file.Decls...)

	removed, ok := phantompkg.RemoveDecl(file).(*ast.File)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Not(qt.Equals(removed, file)))
	qt.Assert(t, qt.HasLen(removed.Decls, fields))
	qt.Assert(t, qt.HasLen(file.Decls, fields+1))
	qt.Assert(t, qt.Equals(file.Decls[0], ast.Decl(phantom)))
}
