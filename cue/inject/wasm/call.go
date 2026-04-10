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
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/pkg"
	"github.com/tetratelabs/wazero/api"
)

func encBool(b bool) uint64 {
	if b {
		return api.EncodeU32(1)
	}
	return api.EncodeU32(0)
}

// encNumber returns the Wasm/System V ABI representation of the number
// wrapped into val, which must conform to the type of typ.
func encNumber(typ cue.Value, val cue.Value) (r uint64) {
	ctx := val.Context()

	_int32 := ctx.CompileString("int32")
	if _int32.Subsume(typ) == nil {
		i, _ := val.Int64()
		return api.EncodeI32(int32(i))
	}

	_int64 := ctx.CompileString("int64")
	if _int64.Subsume(typ) == nil {
		i, _ := val.Int64()
		return api.EncodeI64(i)
	}

	_uint32 := ctx.CompileString("uint32")
	if _uint32.Subsume(typ) == nil {
		i, _ := val.Uint64()
		return api.EncodeU32(uint32(i))
	}

	_uint64 := ctx.CompileString("uint64")
	if _uint64.Subsume(typ) == nil {
		i, _ := val.Uint64()
		return i
	}

	_float32 := ctx.CompileString("float32")
	if _float32.Subsume(typ) == nil {
		f, _ := val.Float64()
		return api.EncodeF32(float32(f))
	}

	_float64 := ctx.CompileString("float64")
	if _float64.Subsume(typ) == nil {
		f, _ := val.Float64()
		return api.EncodeF64(f)
	}

	panic("encNumber: unsupported argument type")
}

func decBool(v uint64) bool {
	return api.DecodeU32(v) == 1
}

// decNumber decodes the Wasm/System V ABI encoding of the
// val number of type typ into a Go value.
func decNumber(typ cue.Value, val uint64) (r any) {
	ctx := typ.Context()

	_int32 := ctx.CompileString("int32")
	if _int32.Subsume(typ) == nil {
		return api.DecodeI32(val)
	}

	_uint32 := ctx.CompileString("uint32")
	if _uint32.Subsume(typ) == nil {
		return api.DecodeU32(val)
	}

	_int64 := ctx.CompileString("int64")
	if _int64.Subsume(typ) == nil {
		return int64(val)
	}

	_uint64 := ctx.CompileString("uint64")
	if _uint64.Subsume(typ) == nil {
		return val
	}

	_float32 := ctx.CompileString("float32")
	if _float32.Subsume(typ) == nil {
		return api.DecodeF32(val)
	}

	_float64 := ctx.CompileString("float64")
	if _float64.Subsume(typ) == nil {
		return api.DecodeF64(val)
	}

	panic(fmt.Sprintf("unsupported argument type %v (kind %v)", typ, typ.IncompleteKind()))
}

func encBytes(i *instance, b []byte) *memory {
	m, _ := i.Alloc(uint32(len(b)))
	m.WriteAt(b, 0)
	return m
}

// cABIFunc implements the Wasm/System V ABI translation. The named
// function, which must be loadable by the instance, and must be of
// the specified sig type, will be called by the runtime after its
// arguments will be converted according to the ABI. The result of the
// call will be then also be converted back into a Go value and handed
// to the runtime.
func cABIFunc(i *instance, name string, sig []cue.Value) func(*pkg.CallCtxt) {
	// Compute the layout of all encountered structs (arguments
	// and result) such that we will have it available at the time
	// of an actual call.
	argsTyp, resTyp := splitLast(sig)
	argLayouts := make([]*structLayout, 0, len(argsTyp))
	var retLayout *structLayout
	for _, typ := range argsTyp {
		switch typ.IncompleteKind() {
		case cue.StructKind:
			argLayouts = append(argLayouts, structLayoutVal(typ))
		default:
			argLayouts = append(argLayouts, nil)
		}
	}
	if resTyp.IncompleteKind() == cue.StructKind {
		retLayout = structLayoutVal(resTyp)
	}

	fn, _ := i.load(name)
	return func(c *pkg.CallCtxt) {
		argsTyp, resTyp := splitLast(sig)
		args := make([]uint64, 0, len(argsTyp))
		for k, typ := range argsTyp {
			switch typ.IncompleteKind() {
			case cue.BoolKind:
				args = append(args, encBool(c.Bool(k)))
			case cue.IntKind, cue.FloatKind, cue.NumberKind:
				args = append(args, encNumber(typ, c.Value(k)))
			case cue.StructKind:
				ms := encodeStruct(i, c.Value(k), argLayouts[k])
				defer i.FreeAll(ms)

				args = append(args, uint64(ms[0].ptr))
			default:
				panic(fmt.Sprintf("unsupported argument type %v (kind %v)", typ, typ.IncompleteKind()))
			}
		}

		var retMem *memory
		if resTyp.IncompleteKind() == cue.StructKind {
			retMem, _ = i.Alloc(uint32(retLayout.size))
			// TODO: add support for structs containing pointers.
			defer i.Free(retMem)
			args = append(args, uint64(retMem.ptr))
		}

		if c.Do() {
			res, err := fn.Call(i.ctx, args...)
			if err != nil {
				c.Err = err
				return
			}
			switch resTyp.IncompleteKind() {
			case cue.BoolKind:
				c.Ret = decBool(res[0])
			case cue.IntKind, cue.FloatKind, cue.NumberKind:
				c.Ret = decNumber(resTyp, res[0])
			case cue.StructKind:
				c.Ret = decodeStruct(retMem.Bytes(), retLayout)
			default:
				panic(fmt.Sprintf("unsupported result type %v (kind %v)", resTyp, resTyp.IncompleteKind()))
			}
		}
	}
}
