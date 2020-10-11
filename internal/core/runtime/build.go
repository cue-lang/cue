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
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

// Build builds b and all its transitive dependencies, insofar they have not
// been build yet.
func (x *Runtime) Build(b *build.Instance) (v *adt.Vertex, errs errors.Error) {
	if v := x.GetNodeFromInstance(b); v != nil {
		return v, b.Err
	}
	// TODO: clear cache of old implementation.
	// if s := b.ImportPath; s != "" {
	// 	// Use cached result, if available.
	// 	if v, err := x.LoadImport(s); v != nil || err != nil {
	// 		return v, err
	// 	}
	// }

	errs = b.Err

	// Build transitive dependencies.
	for _, file := range b.Files {
		file.VisitImports(func(d *ast.ImportDecl) {
			for _, s := range d.Specs {
				errs = errors.Append(errs, x.buildSpec(b, s))
			}
		})
	}

	err := x.ResolveFiles(b)
	errs = errors.Append(errs, err)

	v, err = compile.Files(nil, x, b.ID(), b.Files...)
	errs = errors.Append(errs, err)

	if errs != nil {
		v = adt.ToVertex(&adt.Bottom{Err: errs})
		b.Err = errs
	}

	x.AddInst(b.ImportPath, v, b)

	return v, errs
}

func (x *Runtime) buildSpec(b *build.Instance, spec *ast.ImportSpec) (errs errors.Error) {
	info, err := astutil.ParseImportSpec(spec)
	if err != nil {
		return errors.Promote(err, "invalid import path")
	}

	pkg := b.LookupImport(info.ID)
	if pkg == nil {
		if strings.Contains(info.ID, ".") {
			return errors.Newf(spec.Pos(),
				"package %q imported but not defined in %s",
				info.ID, b.ImportPath)
		}
		return nil // TODO: check the builtin package exists here.
	}

	if v := x.index.importsByBuild[pkg]; v != nil {
		return pkg.Err
	}

	if _, err := x.Build(pkg); err != nil {
		return err
	}

	return nil
}
