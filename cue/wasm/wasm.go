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
	"context"
	"fmt"
	"os"

	"cuelang.org/go/internal/pkg"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// defaultRuntime is a global runtime for all Wasm modules used in a
// CUE process. It acts as a compilation cache, however, every module
// instance is independent. The same module loaded by two different
// CUE packages will not share memory, although it will share the
// excutable code produced by the runtime.
var defaultRuntime runtime

func init() {
	ctx := context.Background()
	defaultRuntime = runtime{
		ctx:     ctx,
		Runtime: newRuntime(ctx),
	}
}

// A runtime is a Wasm runtime that can compile, load, and execute
// Wasm code.
type runtime struct {
	// ctx exists so that we have something to pass to Wazero
	// functions, but it's unused otherwise.
	ctx context.Context

	wazero.Runtime
}

func newRuntime(ctx context.Context) wazero.Runtime {
	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	return r
}

// compile takes the name of a Wasm module, and returns its compiled
// form, or an error.
func (r *runtime) compile(name string) (*module, error) {
	buf, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("can't compile Wasm module: %w", err)
	}

	mod, err := r.Runtime.CompileModule(r.ctx, buf)
	if err != nil {
		return nil, fmt.Errorf("can't compile Wasm module: %w", err)
	}
	return &module{
		runtime:        r,
		name:           name,
		CompiledModule: mod,
	}, nil
}

// compileAndLoad is a convenience function that compile a module then
// loads it into memory returning the loaded instance, or an error.
func compileAndLoad(name string) (*instance, error) {
	m, err := defaultRuntime.compile(name)
	if err != nil {
		return nil, err
	}
	i, err := m.load()
	if err != nil {
		return nil, err
	}
	return i, nil
}

// A module is a compiled Wasm module.
type module struct {
	*runtime
	name string
	wazero.CompiledModule
}

// load loads the compiled module into memory, returning a new instance
// that can be called into, or an error. Different instances of the
// same module do not share memory.
func (m *module) load() (*instance, error) {
	cfg := wazero.NewModuleConfig().WithName(m.name)
	wInst, err := m.Runtime.InstantiateModule(m.ctx, m.CompiledModule, cfg)
	if err != nil {
		return nil, fmt.Errorf("can't instantiate Wasm module: %w", err)
	}

	inst := instance{
		module:   m,
		instance: wInst,
	}
	return &inst, nil
}

// An instance is a Wasm module loaded into memory.
type instance struct {
	*module
	instance api.Module
}

// load attempts to load the named function from the instance, returning
// it if found, or an error.
func (i *instance) load(funcName string) (api.Function, error) {
	f := i.instance.ExportedFunction(funcName)
	if f == nil {
		return nil, fmt.Errorf("can't find function %q in Wasm module %v", funcName, i.module.Name())
	}
	return f, nil
}

// callCtxFunc returns a function that wraps fn, which is assumed to
// be of type typ, into a function that knows how to load its arguments
// from CUE, call fn with the arguments, then pass its result
// back to CUE.
func (i *instance) callCtxFunc(fn api.Function, typ fnTyp) func(*pkg.CallCtxt) {
	return func(c *pkg.CallCtxt) {
		var args []uint64
		for k, t := range typ.Args {
			//
			// TODO: support more than abi=c here.
			//
			args = append(args, loadArg(c, k, t))
		}
		if c.Do() {
			results, err := fn.Call(i.ctx, args...)
			if err != nil {
				c.Err = err
				return
			}
			//
			// TODO: support more than abi=c here.
			//
			c.Ret = decodeRet(results[0], typ.Ret)
		}
	}
}
