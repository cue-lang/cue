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

package compile

import (
	"path/filepath"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/extern"
	"cuelang.org/go/internal/wasm"
)

func lookupExternAttr(f *ast.Field) (*internal.Attr, bool) {
	for _, a := range f.Attrs {
		key, body := a.Split()
		if key == "extern" {
			attr := internal.ParseAttrBody(a.At, body)
			return &attr, true
		}
	}
	return nil, false
}

func newExternFunc(c *compiler, dir string, attr internal.Attr) (b *adt.Builtin, err error) {
	sig, ok, err := attr.Lookup(0, "sig")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.Newf(token.NoPos, "missing sig key")
	}

	funcName, ok, err := attr.Lookup(0, "name")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.Newf(token.NoPos, "missing name key")
	}

	wasmFile, err := attr.String(0)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "missing file name: %w", err)
	}
	wasmFile = filepath.Join(dir, wasmFile)

	fnSig, err := extern.ParseOneFuncSig(sig)
	if err != nil {
		return nil, err
	}

	inst, err := loadWasmInstance(c, wasmFile)
	if err != nil {
		return nil, err
	}
	b, err = inst.Func(funcName, *fnSig)
	if err != nil {
		return nil, err
	}
	return b, err
}

func loadWasmInstance(c *compiler, filename string) (wasm.Instance, error) {
	if c.wasmInstances == nil {
		c.wasmInstances = make(map[string]wasm.Instance)
	}
	if inst, ok := c.wasmInstances[filename]; ok {
		return inst, nil
	}

	inst, err := wasm.CompileAndLoad(c.WasmRuntime, filename)
	if err != nil {
		return nil, err
	}
	c.wasmInstances[filename] = inst
	return inst, nil
}
