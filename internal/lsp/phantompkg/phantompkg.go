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

// Package phantompkg holds the convention by which the language server
// gives every file a package declaration.
//
// A phantom package declaration is one the language server injects into
// the syntax tree of a file which declares no package, so that every
// file it works with has one. Its name is derived from the file's name
// and has no position, which is what tells it from a declaration the
// file's author wrote.
//
// The package depends on nothing of the Language Server Protocol, so
// that code which only reads such syntax trees can tell the two apart
// without linking the protocol's implementation.
package phantompkg

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal"
)

// Name returns the package name injected into the file with the given
// name when it has no package declaration of its own.
func Name(filename string) string {
	return fmt.Sprintf("_%x", sha256.Sum256([]byte(filename)))
}

// nameLength is the length of every name [Name] returns.
var nameLength = len(Name(""))

// Is reports whether the package declaration was created to be
// injected into a file's AST for files which have no package
// declaration themselves.
func Is(pkgDecl *ast.Package) bool {
	name := pkgDecl.Name
	return name != nil && !name.Pos().IsValid() && len(name.Name) == nameLength && name.Name[0] == '_'
}

// RemoveDecl removes any phantom package declaration from the provided
// AST. The file given is left as it is: when there is a declaration to
// remove, a copy without it is returned.
func RemoveDecl(file *ast.File) ast.Node {
	pkgDecl, i := internal.Package(file)
	if pkgDecl == nil || !Is(pkgDecl) {
		return file
	}

	decls := slices.Clone(file.Decls)
	decls = slices.Delete(decls, i, i+1)
	fileCopy := *file
	fileCopy.Decls = decls
	return &fileCopy
}
