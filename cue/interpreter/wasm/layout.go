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
	"encoding/binary"
	"fmt"
	"math"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
)

// typ is the type (or kind) of an external type.
type typ int8

//go:generate stringer -type=typ
const (
	typErr typ = iota
	typBool
	typUint8
	typUint16
	typUint32
	typUint64
	typInt8
	typInt16
	typInt32
	typInt64
	typFloat32
	typFloat64
	typStruct
)

// field represents a name struct field.
type field struct {
	typ
	from string // the field name
}

// positionedField represents a struct field with a known location.
type positionedField struct {
	field
	offset int // memory offset in the parent struct.

	inner *structLayout // IFF typ==typStruct
}

// structLayout describes the memory layout of a struct.
type structLayout struct {
	fields []positionedField
	size   int
	align  int
}

func sizeof(t typ) int {
	switch t {
	case typBool, typUint8, typInt8:
		return 1
	case typUint16, typInt16:
		return 2
	case typUint32, typInt32, typFloat32:
		return 4
	case typUint64, typInt64, typFloat64:
		return 8
	}
	panic("unreachable")
}

func cons[T any](head T, tail ...T) []T {
	return append([]T{head}, tail...)
}

// encode serializes v into Wasm memory according to the layout. it
// returns a slice of Wasm memories that hold v's binary form. The
// memory is allocated in the inst instance and must be freed by the
// caller.
func encode(inst *instance, v cue.Value, l *structLayout) []*memory {
	var mem []*memory
	acc := make([]byte, l.size)

	for _, f := range l.fields {
		arg := v.LookupPath(cue.ParsePath(f.from))

		switch f.typ {
		case typBool:
			b, _ := arg.Bool()
			if b {
				acc[f.offset] = 1
			} else {
				acc[f.offset] = 0
			}

		case typUint8:
			u, _ := arg.Uint64()
			acc[f.offset] = byte(u)
		case typUint16:
			u, _ := arg.Uint64()
			binary.LittleEndian.PutUint16(acc[f.offset:], uint16(u))
		case typUint32:
			u, _ := arg.Uint64()
			binary.LittleEndian.PutUint32(acc[f.offset:], uint32(u))
		case typUint64:
			u, _ := arg.Uint64()
			binary.LittleEndian.PutUint64(acc[f.offset:], u)

		case typInt8:
			u, _ := arg.Int64()
			acc[f.offset] = byte(u)
		case typInt16:
			u, _ := arg.Int64()
			binary.LittleEndian.PutUint16(acc[f.offset:], uint16(u))
		case typInt32:
			u, _ := arg.Int64()
			binary.LittleEndian.PutUint32(acc[f.offset:], uint32(u))
		case typInt64:
			u, _ := arg.Int64()
			binary.LittleEndian.PutUint64(acc[f.offset:], uint64(u))

		case typFloat32:
			x, _ := arg.Float64()
			binary.LittleEndian.PutUint32(acc[f.offset:], math.Float32bits(float32(x)))
		case typFloat64:
			x, _ := arg.Float64()
			binary.LittleEndian.PutUint64(acc[f.offset:], math.Float64bits(x))

		case typStruct:
			ms := encode(inst, arg, f.inner)
			copy(acc[f.offset:], ms[0].Bytes())
			mem = append(mem, ms...)

		default:
			panic(fmt.Sprintf("unsupported argument %v (kind %v)", v, v.IncompleteKind()))
		}
	}
	return cons(encBytes(inst, acc), mem...)
}

// decode takes the binary representation of a struct described by the
// layout and returns its Go representation as a map.
func decode(buf []byte, l *structLayout) map[string]any {
	m := make(map[string]any)

	for _, f := range l.fields {
		switch f.typ {
		case typBool:
			u := buf[f.offset]
			if u == 1 {
				m[f.from] = true
			} else {
				m[f.from] = false
			}

		case typUint8:
			u := buf[f.offset]
			m[f.from] = u
		case typUint16:
			u := binary.LittleEndian.Uint16(buf[f.offset:])
			m[f.from] = u
		case typUint32:
			u := binary.LittleEndian.Uint32(buf[f.offset:])
			m[f.from] = u
		case typUint64:
			u := binary.LittleEndian.Uint64(buf[f.offset:])
			m[f.from] = u

		case typInt8:
			u := buf[f.offset]
			m[f.from] = int8(u)
		case typInt16:
			u := binary.LittleEndian.Uint16(buf[f.offset:])
			m[f.from] = int16(u)
		case typInt32:
			u := binary.LittleEndian.Uint32(buf[f.offset:])
			m[f.from] = int32(u)
		case typInt64:
			u := binary.LittleEndian.Uint64(buf[f.offset:])
			m[f.from] = int64(u)

		case typFloat32:
			u := binary.LittleEndian.Uint32(buf[f.offset:])
			m[f.from] = math.Float32frombits(u)
		case typFloat64:
			u := binary.LittleEndian.Uint64(buf[f.offset:])
			m[f.from] = math.Float64frombits(u)

		case typStruct:
			to := f.offset + f.inner.size
			m[f.from] = decode(buf[f.offset:to], f.inner)

		default:
			panic(fmt.Sprintf("unsupported argument type: %v", f.typ))
		}
	}
	return m
}

func align(x, n int) int {
	return (x + n - 1) & ^(n - 1)
}

// structLayoutVal returns the System V (C ABI) memory layout of the
// struct expressed by t.
func structLayoutVal(t cue.Value) *structLayout {
	if t.IncompleteKind() != adt.StructKind {
		panic("expected CUE struct")
	}

	var sl structLayout
	off, size := 0, 0
	for i, _ := t.Fields(cue.Attributes(true)); i.Next(); {
		f := i.Value()
		path := i.Selector().String()

		switch f.IncompleteKind() {
		case adt.StructKind:
			inner := structLayoutVal(f)
			off = align(off, inner.align)

			lval := positionedField{
				field: field{
					typ:  typStruct,
					from: path,
				},
				offset: off,
				inner:  inner,
			}
			sl.fields = append(sl.fields, lval)

			off += inner.size
		case cue.BoolKind, cue.IntKind, cue.FloatKind, cue.NumberKind:
			typ := typVal(f)
			size = sizeof(typ)
			off = align(off, size)

			lval := positionedField{
				field: field{
					typ:  typ,
					from: path,
				},
				offset: off,
			}
			sl.fields = append(sl.fields, lval)

			off += size
		default:
			panic(fmt.Sprintf("unsupported argument type %v (kind %v)", f, f.IncompleteKind()))
		}
	}

	// The alignment of a struct is the maximum alignment of its
	// constituent fields.
	maxalign := 0
	for _, f := range sl.fields {
		if f.typ == typStruct {
			if f.inner.align > maxalign {
				maxalign = f.inner.align
			}
			continue
		}
		if sizeof(f.typ) > maxalign {
			maxalign = sizeof(f.typ)
		}
	}
	sl.size = align(off, maxalign)
	sl.align = maxalign

	return &sl
}

func typVal(v cue.Value) typ {
	switch v.IncompleteKind() {
	case cue.BoolKind:
		return typBool
	case cue.IntKind, cue.FloatKind, cue.NumberKind:
		return typNum(v)
	default:
		panic(fmt.Sprintf("unsupported argument type %v (kind %v)", v, v.IncompleteKind()))
	}
	panic("unreachable")
}

func typNum(t cue.Value) typ {
	ctx := t.Context()

	_int8 := ctx.CompileString("int8")
	if _int8.Subsume(t) == nil {
		return typInt8
	}

	_uint8 := ctx.CompileString("uint8")
	if _uint8.Subsume(t) == nil {
		return typUint8
	}

	_int16 := ctx.CompileString("int16")
	if _int16.Subsume(t) == nil {
		return typInt16
	}

	_uint16 := ctx.CompileString("uint16")
	if _uint16.Subsume(t) == nil {
		return typUint16
	}

	_int32 := ctx.CompileString("int32")
	if _int32.Subsume(t) == nil {
		return typInt32
	}

	_uint32 := ctx.CompileString("uint32")
	if _uint32.Subsume(t) == nil {
		return typUint32
	}

	_int64 := ctx.CompileString("int64")
	if _int64.Subsume(t) == nil {
		return typInt64
	}

	_uint64 := ctx.CompileString("uint64")
	if _uint64.Subsume(t) == nil {
		return typUint64
	}

	_float32 := ctx.CompileString("float32")
	if _float32.Subsume(t) == nil {
		return typFloat32
	}

	_float64 := ctx.CompileString("float64")
	if _float64.Subsume(t) == nil {
		return typFloat64
	}

	panic("unreachable")
}
