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

package build

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/modfiledata"
)

// TestPartiallyResolved tests that the resolve will detect the usage of
// imports that are referenced by previously resolved nodes.
func TestPartiallyResolved(t *testing.T) {
	const importPath = "mod.test/foo"
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
	}

	p := &Instance{
		Files: []*ast.File{f},
		Imports: []*Instance{{
			ImportPath: importPath,
			PkgName:    "foo",
		}},
	}

	err := p.resolveIdentifiers()

	if err != nil {
		t.Errorf("exected no error, found %v", err)
	}
}

// A file's version must not become the default for files added after it.
func TestAddFileLanguageVersion(t *testing.T) {
	inst := &Instance{}
	if err := inst.AddFile("a.cue", "@version(v0.17.0)\na: 1"); err != nil {
		t.Fatal(err)
	}
	if err := inst.AddFile("b.cue", "b: 2"); err != nil {
		t.Fatal(err)
	}
	if got := inst.Files[1].Pos().LanguageVersion(); got != cueversion.LanguageVersion() {
		t.Fatalf("second file version = %q; want current", got)
	}
}

// Selecting an export target must not change the source version or the
// default for subsequently added files.
func TestTargetLanguageVersion(t *testing.T) {
	inst := &Instance{}
	inst.SetTargetLanguageVersion("v0.16.0")
	if err := inst.AddFile("a.cue", "@version(v0.17.0)\na: 1"); err != nil {
		t.Fatal(err)
	}
	if got := inst.LanguageVersion(); got != "" {
		t.Fatalf("module version = %q", got)
	}
	if got := inst.TargetLanguageVersion(); got != "v0.16.0" {
		t.Fatalf("export version = %q", got)
	}
	if got := inst.Files[0].Pos().LanguageVersion(); got != "v0.17.0" {
		t.Fatalf("source version = %q", got)
	}
	if err := inst.AddFile("b.cue", "b: 2"); err != nil {
		t.Fatal(err)
	}
	if got := inst.Files[1].Pos().LanguageVersion(); got != cueversion.LanguageVersion() {
		t.Fatalf("default version = %q", got)
	}
}

func TestTargetLanguageVersionInModule(t *testing.T) {
	inst := &Instance{ModuleFile: &modfiledata.File{Language: &modfiledata.Language{Version: "v0.17.0"}}}
	inst.SetTargetLanguageVersion("v0.16.0")
	if got := inst.TargetLanguageVersion(); got != "v0.17.0" {
		t.Fatalf("module export version = %q", got)
	}
}
