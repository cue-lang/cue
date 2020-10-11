// Copyright 2020 CUE Authors
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

package runtime

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
)

// TestPartiallyResolved tests that the resolve will detect the usage of
// imports that are referenced by previously resolved nodes.
func TestPartiallyResolved(t *testing.T) {
	const importPath = "acme.com/foo"
	spec1 := &ast.ImportSpec{
		Path: ast.NewString(importPath),
	}
	spec2 := &ast.ImportSpec{
		Name: ast.NewIdent("bar"),
		Path: ast.NewString(importPath),
	}

	f := &ast.File{
		Decls: []ast.Decl{
			&ast.ImportDecl{Specs: []*ast.ImportSpec{spec1, spec2}},
			&ast.Field{
				Label: ast.NewIdent("X"),
				Value: &ast.Ident{Name: "foo", Node: spec1},
			},
			&ast.Alias{
				Ident: ast.NewIdent("Y"),
				Expr:  &ast.Ident{Name: "bar", Node: spec2},
			},
		},
		Imports: []*ast.ImportSpec{spec1, spec2},
	}

	err := resolveFile(nil, f, &build.Instance{
		Imports: []*build.Instance{{
			ImportPath: importPath,
			PkgName:    "foo",
		}},
	}, map[string]ast.Node{})

	if err != nil {
		t.Errorf("exected no error, found %v", err)
	}
}
