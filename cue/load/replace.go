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

package load

import (
	"io/fs"

	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/mod/modconfig"
)

// newReplacingRegistry returns a registry that serves replaced modules from
// their replacement sources. It returns reg unchanged when repls is nil.
//
// The wrapping logic lives in [modpkgload.NewReplacingRegistry] so that it
// can be shared with the module loader used by `cue mod tidy`. A
// [modconfig.Registry] satisfies [modpkgload.FullRegistry] (and vice versa),
// so the registry round-trips through the shared wrapper unchanged.
func newReplacingRegistry(reg modconfig.Registry, repls *modpkgload.Replacements, openDir func(string) (fs.FS, error)) modconfig.Registry {
	return modpkgload.NewReplacingRegistry(reg, repls, openDir)
}
