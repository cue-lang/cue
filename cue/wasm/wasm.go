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

// Package wasm provides optional Wasm support to CUE.
//
// When Wasm is enabled via [cuelang.org/go/cue.Wasm], CUE can make
// use of user-defined functions provided as Wasm modules.
//
// To make use of Wasm modules in CUE, use an extern attribute in your
// CUE code, like this:
//
//	myAdd: _ @extern("foo.wasm", abi=c, name=add, sig="func(int32, int32): int32")
//
// The first parameter should specify the Wasm module to import the
// function from and is interpreted relative to location of the CUE
// file itself. Name is the Wasm name of the function you are importing,
// which may be different from its CUE name, for example the Wasm add
// function is imported as myAdd in CUE.
//
// Sig specifies the type signature of the imported Wasm function.
// Functions signatures belong to the following grammar:
//
//	ident	:= "bool" |  "int8" |  "int16" |  "int32" |  "int64"
//	                  | "uint8" | "uint16" | "uint32" | "uint64"
//	                  | "float32" | "float64" .
//	list	:= ident [ { "," ident } ]
//	func	:= "func" "(" [ list ] ")" ":" ident .
//
// The extern attribute is ignored when Wasm support is not enabled
// in CUE, therefore in that mode, the type of myAdd above would be
// _. When Wasm is enabled, myAdd will be a callable CUE function of
// the specified homonymous argument and return types.
//
// The Wasm module must contain an exported function with the specified
// name, signature, and abi. Currently only the C abi is supported and
// must be explicitely specified in the attribute.
//
// The Wasm module is instantiated in a sandbox with no access to the
// outside world.
//
// It is critical that functions made available to CUE via this mechanism
// are pure and total, that is, they always terminate and they have
// no side effects with their behavior being determined purely by their
// arguments.
package wasm

import (
	"context"
	"fmt"
	"os"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/extern"
	"cuelang.org/go/internal/pkg"
	"cuelang.org/go/internal/wasm"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var defaultRuntime runtime

// Runtime returns a Wasm runtime that can be used by CUE by passing
// its return value to [cuelang.org/go/cue.Wasm].
func Runtime() wasm.Runtime {
	return &defaultRuntime
}

func init() {
	ctx := context.Background()
	defaultRuntime = runtime{
		ctx:     ctx,
		Runtime: newRuntime(ctx),
	}
}

type runtime struct {
	ctx context.Context
	wazero.Runtime
}

func (r *runtime) Compile(name string) (wasm.Loadable, error) {
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

type module struct {
	*runtime
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

func (i *instance) Func(name string, fSig extern.FuncSig) (*adt.Builtin, error) {
	fsig := toFnTyp(fSig)
	b, err := loadBuiltin(i, name, fsig)
	if err != nil {
		return nil, err
	}
	return pkg.ToBuiltin(b), nil
}

func loadFunc(i *instance, name string) (api.Function, error) {
	f := i.instance.ExportedFunction(name)
	if f == nil {
		return nil, fmt.Errorf("can't find function %q in Wasm module %v", name, i.module.Name())
	}
	return f, nil
}

func loadBuiltin(i *instance, name string, fSig fnTyp) (*pkg.Builtin, error) {
	fn, err := loadFunc(i, name)
	if err != nil {
		return nil, err
	}
	b := &pkg.Builtin{
		Name:   name,
		Params: params(fSig),
		Result: fSig.Ret.kind(),
		Func:   toCallCtxFunc(i, fn, fSig),
	}
	return b, nil
}

func toCallCtxFunc(i *instance, fn api.Function, fSig fnTyp) func(*pkg.CallCtxt) {
	return func(c *pkg.CallCtxt) {
		var args []uint64
		for k, t := range fSig.Args {
			args = append(args, loadArg(c, k, t))
		}
		if c.Do() {
			results, err := fn.Call(i.ctx, args...)
			if err != nil {
				c.Err = err
				return
			}
			c.Ret = decodeRet(results[0], fSig.Ret)
		}
	}
}

func newRuntime(ctx context.Context) wazero.Runtime {
	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	return r
}
