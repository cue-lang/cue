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
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/extern"
	"cuelang.org/go/internal/pkg"

	"github.com/tetratelabs/wazero/api"
)

// typ represents the Go and CUE types that we can exchange with Wasm.
// It doesn't map directly to Wasm types, for example Wasm doesn't
// have a bool type, but we can use this type as a marker to remember
// to convert between a Wasm integer and a Go bool type.
type typ string

const (
	_bool    typ = "bool"
	_int8        = "int8"
	_int16       = "int16 "
	_int32       = "int32"
	_int64       = "int64"
	_uint8       = "uint8"
	_uint16      = "uint16"
	_uint32      = "uint32"
	_uint64      = "uint64"
	_float32     = "float32"
	_float64     = "float64"
)

func (t typ) kind() adt.Kind {
	switch t {
	case _bool:
		return adt.BoolKind
	case _int8, _int16, _int32, _int64, _uint8, _uint16, _uint32, _uint64:
		return adt.IntKind
	case _float32, _float64:
		return adt.FloatKind
	}
	return adt.BottomKind
}

func (t typ) value() adt.Value {
	if isScalar(t) {
		return compile.Predeclared(string(t)).(adt.Value)
	}
	return nil
}

func (t typ) param() pkg.Param {
	p := pkg.Param{
		Kind:  t.kind(),
		Value: t.value(),
	}
	return p
}

func params(f fnTyp) []pkg.Param {
	var params []pkg.Param
	for _, v := range f.Args {
		params = append(params, v.param())
	}
	return params
}

func isScalar(t typ) bool {
	return is32(t) || is64(t)
}

func is32(t typ) bool {
	switch t {
	case _bool, _int8, _int16, _int32, _uint8, _uint16, _uint32:
		return true
	}
	return false
}

func is64(t typ) bool {
	switch t {
	case _uint64, _int64:
		return true
	}
	return false
}

type fnTyp struct {
	Args []typ
	Ret  typ
}

func toFnTyp(f extern.FuncSig) fnTyp {
	var args []typ
	for _, a := range f.Args {
		args = append(args, typ(a))
	}

	return fnTyp{
		Args: args,
		Ret:  typ(f.Ret),
	}
}

func decodeRet(r uint64, t typ) any {
	switch t {
	case _bool:
		u := api.DecodeU32(r)
		if u == 1 {
			return true
		}
		return false
	case _int8, _int16, _int32:
		return api.DecodeI32(r)
	case _uint8, _uint16, _uint32:
		return api.DecodeU32(r)
	case _int64, _uint64:
		return r
	case _float32:
		return api.DecodeF32(r)
	case _float64:
		return api.DecodeF64(r)
	}
	panic("unsupported return type")
}

// loadArg load the i'th argument (which must be of type t)
// passed to a function call represented by the call context.
// It returns the argument as an uint64, so it can be passed
// directly to Wasm functions.
func loadArg(c *pkg.CallCtxt, i int, t typ) uint64 {
	switch t {
	case _bool:
		b := c.Bool(i)
		if b {
			return api.EncodeU32(1)
		}
		return api.EncodeU32(0)
	case _int8:
		return api.EncodeI32(int32(c.Int8(i)))
	case _int16:
		return api.EncodeI32(int32(c.Int16(i)))
	case _int32:
		return api.EncodeI32(c.Int32(i))
	case _int64:
		return api.EncodeI64(c.Int64(i))
	case _uint8:
		return api.EncodeU32(uint32(c.Uint8(i)))
	case _uint16:
		return api.EncodeU32(uint32(c.Uint16(i)))
	case _uint32:
		return api.EncodeU32(c.Uint32(i))
	case _uint64:
		return c.Uint64(i)
	case _float32:
		return api.EncodeF32(float32(c.Float64(i)))
	case _float64:
		return api.EncodeF64(c.Float64(i))
	}
	panic("unsupported argument type")
}
