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

package pkg

import (
	"maps"
	"path"
	"slices"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// Register registers src as the CUE definitions of the standard library
// package with the given import path, as returned by [Source], and p as
// its builtins unless p is nil.
func Register(importPath string, p *Package, src string) {
	sources[importPath] = src
	if p == nil {
		return
	}
	inst := &build.Instance{
		ImportPath: importPath,
		PkgName:    path.Base(importPath),
	}
	f := func(r adt.Runtime) (*adt.Vertex, errors.Error) {
		ctx := adt.NewContext(r, nil)

		return p.MustCompile(ctx, importPath), nil
	}
	runtime.RegisterBuiltin(inst, f)
}

// sources maps import paths to CUE definitions. It is only written
// during package initialization, so reads need no locking.
var sources = map[string]string{}

// Source returns the CUE definitions registered for importPath.
func Source(importPath string) (string, bool) {
	src, ok := sources[importPath]
	return src, ok
}

// SourceImportPaths returns the sorted import paths of all registered
// definitions.
func SourceImportPaths() []string {
	return slices.Sorted(maps.Keys(sources))
}
