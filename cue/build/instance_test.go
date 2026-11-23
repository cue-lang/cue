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
	"fmt"
	"slices"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
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

// A file shared by instances of one context, such as one in a parent
// directory, binds or clears its references to other files' fields for
// each instance, and reports its errors in each.
func TestSharedFile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		addFirst bool   // add the file to both instances before completing either
		wantB    string // what ref is bound to once instance b is complete
	}{
		{name: "CompleteFirst", wantB: "unresolved"},
		// Instance b keeps the binding made by instance a.
		{name: "AddFirst", addFirst: true, wantB: "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shared, _ := parser.ParseFile("shared.cue", `
package x

import "list"

let X = 1
let X = 2
out: X
ref: a
`)
			ref := shared.Decls[len(shared.Decls)-1].(*ast.Field).Value.(*ast.Ident)
			ctx := NewContext()
			newInst := func(name, src string) *Instance {
				inst := ctx.NewInstance("", nil)
				if err := inst.AddSyntax(shared); err != nil {
					t.Fatal(err)
				}
				if err := inst.AddFile(name, src); err != nil {
					t.Fatal(err)
				}
				return inst
			}
			instA := newInst("a.cue", "package x\na: 1\n")
			binding := func() string {
				switch {
				case ref.Node == nil && slices.Contains(shared.Unresolved, ref):
					return "unresolved"
				case ref.Node == instA.Files[1].Decls[1].(*ast.Field).Value:
					return "a"
				}
				return fmt.Sprint(ref.Node)
			}
			var instB *Instance
			if tc.addFirst {
				instB = newInst("b.cue", "package x\nb: 1\n")
			}
			instA.Complete()
			if got, want := binding(), "a"; got != want {
				t.Errorf("instance a: ref bound to %s; want %s", got, want)
			}
			if !tc.addFirst {
				instB = newInst("b.cue", "package x\nb: 1\n")
			}
			instB.Complete()
			if got := binding(); got != tc.wantB {
				t.Errorf("instance b: ref bound to %s; want %s", got, tc.wantB)
			}
			for _, inst := range []*Instance{instA, instB} {
				if got, want := fmt.Sprint(inst.Err), `alias "X" redeclared in same scope`; got != want {
					t.Errorf("Err = %q; want %q", got, want)
				}
				if got, want := fmt.Sprint(inst.ResolutionErr), `imported and not used: "list"`; got != want {
					t.Errorf("ResolutionErr = %q; want %q", got, want)
				}
			}

			// A file changed before it is added again is resolved again.
			shared.Decls = slices.Delete(shared.Decls, 3, 4) // let X = 2
			instC := newInst("c.cue", "package x\na: 1\n")
			if instC.Err != nil {
				t.Errorf("instance c: Err = %v; want nil", instC.Err)
			}
		})
	}
}
