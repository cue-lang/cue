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
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

type Config struct {
	Runtime    *Runtime
	Filename   string
	ImportPath string

	Counts *stats.Counts

	compile.Config
}

// hasOptions reports whether c changes what an instance builds to. A new
// option which does so must be reported here, or its builds are shared.
func (c *Config) hasOptions() bool {
	return c != nil && (c.Scope != nil || c.Imports != nil || c.ImportPath != "")
}

// Build builds b and all its transitive dependencies, insofar they have not
// been build yet. The options in cfg apply to b alone.
//
// Only a build without options is shared with later builds and importers of
// b. A build with options compiles a copy of b each time.
func (x *Runtime) Build(cfg *Config, b *build.Instance) (*adt.Vertex, errors.Error) {
	if err := b.Complete(); err != nil {
		return nil, b.Err
	}
	if cfg.hasOptions() {
		// Leave b as it is for its other builds and importers.
		c := *b
		return x.buildOwned(cfg, &c)
	}
	if v, errs := x.built(b); v != nil || errs != nil {
		return v, errs
	}
	v, errs := x.build(cfg, b)
	x.addBuild(v, b, errs)
	return v, errs
}

// buildOwned builds b, which no other build uses, so that no later build of b
// reuses it. It records the errors in b for the caller.
func (x *Runtime) buildOwned(cfg *Config, b *build.Instance) (*adt.Vertex, errors.Error) {
	if err := b.Complete(); err != nil {
		return nil, b.Err
	}
	v, errs := x.build(cfg, b)
	b.Err = errs
	x.addOwnedInst(v, b)
	return v, errs
}

// build builds b, which is complete, without looking for an earlier build of
// it nor recording it.
func (x *Runtime) build(cfg *Config, b *build.Instance) (v *adt.Vertex, errs errors.Error) {
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
		for s := range file.ImportSpecs() {
			errs = errors.Append(errs, x.buildSpec(b, s))
		}
	}

	errs = errors.Append(errs, b.ResolutionErr)

	var cc *compile.Config
	if cfg != nil {
		cc = &cfg.Config
	}
	if cfg != nil && cfg.ImportPath != "" {
		if cfg.ImportPath != b.ImportPath {
			// A different import path also becomes the package's identifier.
			b.CanonicalID = ""
		}
		b.ImportPath = cfg.ImportPath
		b.PkgName = ast.ParseImportPath(b.ImportPath).Qualifier
	}
	v, err := compile.Instance(cc, x, b)
	errs = errors.Append(errs, err)

	errs = errors.Append(errs, x.InjectImplementations(b, v))

	if errs != nil {
		v = adt.ToVertex(&adt.Bottom{Err: errs})
	}
	return v, errs
}

func (r *Runtime) Compile(cfg *Config, source interface{}) (*adt.Vertex, *build.Instance) {
	ctx := build.NewContext()
	var filename string
	if cfg != nil && cfg.Filename != "" {
		filename = cfg.Filename
	}
	p := ctx.NewInstance(filename, nil)
	//lint:ignore SA1019 AddFile is due to be renamed, not removed
	if err := p.AddFile(filename, source); err != nil {
		return nil, p
	}
	v, _ := r.buildOwned(cfg, p)
	return v, p
}

func (r *Runtime) CompileFile(cfg *Config, file *ast.File) (*adt.Vertex, *build.Instance) {
	ctx := build.NewContext()
	filename := file.Filename
	if cfg != nil && cfg.Filename != "" {
		filename = cfg.Filename
	}
	p := ctx.NewInstance(filename, nil)
	err := p.AddSyntax(file)
	if err != nil {
		return nil, p
	}
	p.PkgName = file.PackageName()
	v, _ := r.buildOwned(cfg, p)
	return v, p
}

func (x *Runtime) buildSpec(b *build.Instance, spec *ast.ImportSpec) (errs errors.Error) {
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return errors.Promote(err, "invalid import path")
	}

	pkg := b.LookupImport(path)
	if pkg == nil {
		if strings.Contains(path, ".") {
			return errors.Newf(spec.Pos(),
				"package %q imported but not defined in %s",
				path, b.ImportPath)
		} else if x.index.builtins == nil || x.index.builtins.importPaths[path] == nil {
			return errors.Newf(spec.Pos(),
				"builtin package %q undefined", path)
		}
		return nil
	}

	_, errs = x.Build(nil, pkg)
	return errs
}
