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

package builtin

import (
	"sort"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
)

type PackageFunc func(ctx *adt.OpContext) (*adt.Vertex, error)

// Register registers a builtin, the value of which will be built
// on first use. All builtins must be registered before first use of a runtime.
// This restriction may be eliminated in the future.
func Register(importPath string, f PackageFunc) {
	builtins[importPath] = f
	// TODO: remove at some point.
	cue.AddBuiltinPackage(importPath, f)
}

var builtins = map[string]PackageFunc{}

func ImportPaths() (a []string) {
	for s := range builtins {
		a = append(a, s)
	}
	sort.Strings(a)
	return a
}

// Get return the builder for the package with the given path.
// It will panic if the path does not exist.
func Get(path string) PackageFunc {
	return builtins[path]
}
