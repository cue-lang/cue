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
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// A runtime is a Wasm runtime that can compile, load, and execute
// Wasm code.
type runtime struct {
	// ctx exists so that we have something to pass to Wazero
	// functions, but it's unused otherwise.
	ctx context.Context

	wazero.Runtime
}

func newRuntime() runtime {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	return runtime{
		ctx:     ctx,
		Runtime: r,
	}
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

// compileAndLoad is a convenience method that compiles a module then
// loads it into memory returning the loaded instance, or an error.
func (r *runtime) compileAndLoad(name string) (*instance, error) {
	m, err := r.compile(name)
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
	// mu serializes access the whole struct.
	mu sync.Mutex

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
	i.mu.Lock()
	defer i.mu.Unlock()

	f := i.instance.ExportedFunction(funcName)
	if f == nil {
		return nil, fmt.Errorf("can't find function %q in Wasm module %v", funcName, i.module.Name())
	}
	return f, nil
}

// Alloc returns a reference to newly allocated guest memory that spans
// the provided size.
func (i *instance) Alloc(size uint32) (*memory, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

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
	i.mu.Lock()
	defer i.mu.Unlock()

	i.free.Call(i.ctx, uint64(m.ptr), uint64(m.len))
}

// Free frees several previously allocated guest memories.
func (i *instance) FreeAll(ms []*memory) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, m := range ms {
		i.free.Call(i.ctx, uint64(m.ptr), uint64(m.len))
	}
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
	m.i.mu.Lock()
	defer m.i.mu.Unlock()

	p, ok := m.i.instance.Memory().Read(m.ptr, m.len)
	if !ok {
		panic(fmt.Sprintf("can't read %d bytes from Wasm address %#x", m.len, m.ptr))
	}
	return bytes.Clone(p)
}

// WriteAt writes p at the given relative offset within m.
// It panics if buf doesn't fit into m, or if off is out of bounds.
func (m *memory) WriteAt(p []byte, off int64) (int, error) {
	if (off < 0) || (off >= 1<<32-1) {
		panic(fmt.Sprintf("can't write %d bytes to Wasm address %#x", len(p), m.ptr))
	}

	m.i.mu.Lock()
	defer m.i.mu.Unlock()

	ok := m.i.instance.Memory().Write(m.ptr+uint32(off), p)
	if !ok {
		panic(fmt.Sprintf("can't write %d bytes to Wasm address %#x", len(p), m.ptr))
	}
	return len(p), nil
}

// Args returns a memory in the form of pair of arguments directly
// passable to Wasm.
func (m *memory) Args() []uint64 {
	return []uint64{uint64(m.ptr), uint64(m.len)}
}
