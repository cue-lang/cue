// Copyright 2023 CUE Authors
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

package wasm

import (
	"path/filepath"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	coreruntime "cuelang.org/go/internal/core/runtime"
)

// interpreter is a [cuecontext.ExternInterpreter] for Wasm files.
type interpreter struct{}

// New returns a new Wasm interpreter as a [cuecontext.ExternInterpreter]
// suitable for passing to [cuecontext.New].
func New() cuecontext.ExternInterpreter {
	return &interpreter{}
}

func (i *interpreter) Kind() string {
	return "wasm"
}

// InjectorForInstance returns a Wasm injector that services the specified
// build.Instance.
func (i *interpreter) InjectorForInstance(b *build.Instance, r *coreruntime.Runtime) (coreruntime.Injector, errors.Error) {
	return &compiler{
		b:           b,
		runtime:     r,
		wasmRuntime: newRuntime(),
		instances:   make(map[string]*instance),
	}, nil
}

// A compiler is a [coreruntime.Injector]
// that provides Wasm functionality to the runtime.
type compiler struct {
	b           *build.Instance
	runtime     *coreruntime.Runtime
	wasmRuntime runtime

	// mu serializes access to instances.
	mu sync.Mutex

	// instances maps absolute file names to compiled Wasm modules
	// loaded into memory.
	instances map[string]*instance
}

// InjectedValue searches for a Wasm function described by the given
// extern attribute and returns it as an [adt.Builtin].
func (c *compiler) InjectedValue(attr *coreruntime.ExternAttr, scope *adt.Vertex) (adt.Expr, errors.Error) {
	a := attr.Attr

	// Determine the function name from the parent field label,
	// with an explicit name= attribute taking precedence.
	var funcName string
	if f, ok := attr.Parent.(*ast.Field); ok {
		funcName, _, _ = ast.LabelName(f.Label)
	}
	if name, ok, _ := a.Lookup(1, "name"); ok {
		funcName = name
	}

	baseFile, err := fileName(a)
	if err != nil {
		return nil, errors.Promote(err, "invalid attribute")
	}
	// TODO: once we have position information, make this
	// error more user-friendly by returning the position.
	if !strings.HasSuffix(baseFile, "wasm") {
		return nil, errors.Newf(token.NoPos, "load %q: invalid file name", baseFile)
	}

	file, found := findFile(baseFile, c.b)
	if !found {
		return nil, errors.Newf(token.NoPos, "load %q: file not found", baseFile)
	}

	inst, err := c.instance(file)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "can't load Wasm module: %v", err)
	}

	args, err := argList(a)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "invalid function signature: %v", err)
	}
	builtin, err := generateCallThatReturnsBuiltin(funcName, scope, args, inst)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "can't instantiate function: %v", err)
	}
	return builtin, nil
}

// instance returns the instance corresponding to filename, compiling
// and loading it if necessary.
func (c *compiler) instance(filename string) (inst *instance, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, ok := c.instances[filename]
	if !ok {
		inst, err = c.wasmRuntime.compileAndLoad(filename)
		if err != nil {
			return nil, err
		}
		c.instances[filename] = inst
	}
	return inst, nil
}

// findFile searches the build.Instance for the given file name
// and reports its full name and whether it was found.
func findFile(name string, b *build.Instance) (path string, found bool) {
	for _, f := range b.OrphanedFiles {
		if filepath.Base(f.Filename) == name {
			return f.Filename, true
		}
	}
	return "", false
}

// fileName returns the file name of the external module specified in a,
// which must be an extern attribute.
func fileName(a *internal.Attr) (string, error) {
	file, err := a.String(0)
	if err != nil {
		return "", err
	}
	return file, nil
}
