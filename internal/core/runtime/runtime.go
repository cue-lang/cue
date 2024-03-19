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
	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal"
)

// A Runtime maintains data structures for indexing and reuse for evaluation.
type Runtime struct {
	index *index

	loaded map[*build.Instance]interface{}

	// interpreters implement extern functionality. The map key corresponds to
	// the kind in a file-level @extern(kind) attribute.
	interpreters map[string]Interpreter

	version internal.EvaluatorVersion
}

func (r *Runtime) EvaluatorVersion() internal.EvaluatorVersion {
	return r.version
}

func (r *Runtime) SetBuildData(b *build.Instance, x interface{}) {
	r.loaded[b] = x
}

func (r *Runtime) BuildData(b *build.Instance) (x interface{}, ok bool) {
	x, ok = r.loaded[b]
	return x, ok
}

// New is a wrapper for NewVersioned(internal.DefaultVersion).
func New() *Runtime {
	r := &Runtime{}
	r.Init()
	return r
}

// NewVersioned creates a new Runtime using the given runtime version.
// The builtins registered with RegisterBuiltin are available for evaluation.
func NewVersioned(v internal.EvaluatorVersion) *Runtime {
	r := &Runtime{version: v}
	r.Init()
	return r
}

// SetVersion sets the version to use for the Runtime. This should only be set
// before first use.
func (r *Runtime) SetVersion(v internal.EvaluatorVersion) {
	r.version = v
}

// IsInitialized reports whether the runtime has been initialized.
func (r *Runtime) IsInitialized() bool {
	return r.index != nil
}

func (r *Runtime) Init() {
	if r.index != nil {
		return
	}
	r.index = newIndex()

	// TODO: the builtin-specific instances will ultimately also not be
	// shared by indexes.
	r.index.builtinPaths = sharedIndex.builtinPaths
	r.index.builtinShort = sharedIndex.builtinShort

	r.loaded = map[*build.Instance]interface{}{}
}
