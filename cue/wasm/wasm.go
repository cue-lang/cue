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

	"cuelang.org/go/internal/wasm"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var defaultCompiler compiler

func Compiler() wasm.Compiler {
	return &defaultCompiler
}

func init() {
	ctx := context.Background()
	defaultCompiler = compiler{
		ctx:     ctx,
		Runtime: newRuntime(ctx),
	}
}

type compiler struct {
	ctx context.Context
	wazero.Runtime
}

func (c *compiler) Compile(name string) (wasm.Loadable, error) {
	buf, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("can't compile Wasm module: %w", err)
	}

	mod, err := c.Runtime.CompileModule(c.ctx, buf)
	if err != nil {
		return nil, fmt.Errorf("can't compile Wasm module: %w", err)
	}
	return &module{
		compiler:       c,
		name:           name,
		CompiledModule: mod,
	}, nil
}

type module struct {
	*compiler
	name string
	wazero.CompiledModule
}

func (m *module) Load() (wasm.Instance, error) {
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

type instance struct {
	*module
	instance api.Module
}

func (i *instance) Func(name string) (wasm.Func, error) {
	wf := i.instance.ExportedFunction(name)
	if wf == nil {
		return nil, fmt.Errorf("can't find function %q in Wasm module %v", name, i.module.Name())
	}
	f := func(args ...any) (any, error) {
		var wargs []uint64
		for n, a := range args {
			x, ok := a.(uint64)
			if !ok {
				return nil, fmt.Errorf("Wasm call with non-uint64 arguments: argument %d is %T", n, a)
			}
			wargs = append(wargs, x)
		}
		res, err := wf.Call(i.ctx, wargs...)
		return res[0], err
	}
	return f, nil
}

func newRuntime(ctx context.Context) wazero.Runtime {
	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	return r
}
