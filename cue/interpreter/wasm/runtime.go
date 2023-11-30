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
		alloc:    wInst.ExportedFunction("allocate"),
		free:     wInst.ExportedFunction("deallocate"),
	}
	return &inst, nil
}

// An instance is a Wasm module loaded into memory.
type instance struct {
	*module
	instance api.Module

	// alloc is a guest function that allocates guest memory on
	// behalf of the host.
	alloc api.Function

	// free is a guest function that frees guest memory on
	// behalf of the host.
	free api.Function
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

// Alloc returns a reference to newly allocated guest memory that spans
// the provided size.
func (i *instance) Alloc(size uint32) (*memory, error) {
	res, err := i.alloc.Call(i.ctx, uint64(size))
	if err != nil {
		return nil, fmt.Errorf("can't allocate memory: requested %d bytes", size)
	}
	return &memory{
		i:   i,
		ptr: uint32(res[0]),
		len: size,
	}, nil
}

// Free frees previously allocated guest memory.
func (i *instance) Free(m *memory) {
	i.free.Call(i.ctx, uint64(m.ptr), uint64(m.len))
}

// memory is a read and write reference to guest memory that the host
// requested.
type memory struct {
	i   *instance
	ptr uint32
	len uint32
}

// Bytes return a copy of the contents of the guest memory to the host.
func (m *memory) Bytes() []byte {
	bytes, ok := m.i.instance.Memory().Read(m.ptr, m.len)
	if !ok {
		panic(fmt.Sprintf("can't read %d bytes from Wasm address %#x", m.len, m.ptr))
	}
	return append([]byte{}, bytes...)
}

// Write writes into p guest memory referenced by n.
// p must fit into m.
func (m *memory) Write(p []byte) (int, error) {
	ok := m.i.instance.Memory().Write(m.ptr, p)
	if !ok {
		panic(fmt.Sprintf("can't write %d bytes to Wasm address %#x", len(p), m.ptr))
	}
	return len(p), nil
}

// Args returns a memory in the form of pair of arguments dirrectly
// passable to Wasm.
func (m *memory) Args() []uint64 {
	return []uint64{uint64(m.ptr), uint64(m.len)}
}
