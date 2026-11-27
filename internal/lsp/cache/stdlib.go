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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	stdlib "cuelang.org/go/pkg"
	"cuelang.org/go/unstable/lsp/eval"
)

// newStdlibModule creates the sentinel [Module] that owns the
// workspace's standard library packages. It is deliberately not a
// member of [Workspace.modules]: standard library packages are
// immutable, so they take no part in module loading, file watching,
// or diagnostics. They are ordinary [Package]s in every other
// respect, members of the import graph like any other package.
func newStdlibModule(w *Workspace) *Module {
	return &Module{
		workspace: w,
		rootURI:   "cue-stdlib://",
		packages:  make(map[ast.ImportPath]*Package),
	}
}

// ensureStdlibPkg returns the [Package] for the standard library
// package with the given import path, loading it on first use from
// the definitions which [stdlib.Source] returns. It returns nil if
// importPath does not name a standard library package.
func (w *Workspace) ensureStdlibPkg(importPath ast.ImportPath) *Package {
	if pkg, found := w.stdlibModule.packages[importPath]; found {
		return pkg
	}
	if importPath.Version != "" {
		return nil
	}
	src, found := stdlib.Source(importPath.Path)
	if !found {
		return nil
	}
	filename := "cue-stdlib/" + importPath.Path + ".cue"
	file, err := parser.ParseFile(filename, src, parser.ParseComments)
	if err != nil {
		// The embedded definition files are generated and tested: they
		// always parse.
		w.debugLogf("cannot parse %v: %v", filename, err)
		return nil
	}
	pkg := w.stdlibModule.newPackage(importPath, nil)
	pkg.isCue = true
	pkg.isDirty = false
	pkg.eval = eval.New(eval.Config{
		IP:           importPath,
		ForPackage:   pkg.forPackage,
		PkgImporters: pkg.pkgImporters,
	}, file)
	w.debugLogf("Stdlib package %v Loaded", importPath)
	return pkg
}
