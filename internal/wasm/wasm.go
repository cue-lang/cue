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

// Package wasm defines an interface so a Wasm runtime could be injected
// into [cuelang.org/go/internal/core/compile] without making
// [cuelang.org/go/cue] depend on Wasm.
package wasm

// A Compiler is a Wasm runtime that can compile Wasm modules.
type Compiler interface {
	// Compile takes a Wasm module file and compiles it into the
	// internal representation used by the Wasm runtime. It returns
	// the compiled module, or any encountered errors.
	Compile(filename string) (Loadable, error)
}

// A Loadable is a compiled Wasm module that can be loaded into memory.
type Loadable interface {
	// Load takes a compiled Wasm module and loads it in memory.
	// It returns the memory instance, or any encountered errors.
	Load() (Instance, error)
}

type Func func(args ...any) (any, error)

type Instance interface {
	// Func searches the Wasm instance for the named function,
	// returning it if found, otherwise returning the encountered
	// error.
	//
	// The function uses the Wasm calling convention.
	Func(name string) (Func, error)
}
