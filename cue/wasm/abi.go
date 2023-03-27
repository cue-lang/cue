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
	"cuelang.org/go/internal/pkg"
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
		return predeclared(string(t)).(adt.Value)
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

func predeclared(s string) adt.Expr {
	switch s {
	case "bool":
		return &adt.BasicType{K: adt.BoolKind}
	case "int8", "int16", "int32", "int64",
		"uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return compile.LookupRange(s)
	}
	panic("unexpected type")
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
