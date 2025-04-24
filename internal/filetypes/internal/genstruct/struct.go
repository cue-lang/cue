// Copyright 2025 CUE Authors
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

// Package genstruct provides support for simple compact struct representations.
// It is a standalone package so it can be imported by both the filetypes
// generator code and the filetypes package itself.
package genstruct

import (
	"fmt"
	"io"
	"iter"
	"math/bits"
	"strings"
)

type initInfo struct {
	genInit func() string
	ident   string
}

// Struct represents a binary-marshalable struct.
type Struct struct {
	fields   []field
	size     int
	genInits []initInfo
}

func (s *Struct) GenInit(generated map[string]bool) string {
	var buf strings.Builder
	for _, info := range s.genInits {
		if !generated[info.ident] {
			generated[info.ident] = true
			buf.WriteString(info.genInit())
		}
	}
	return buf.String()
}

// Size returns the size of the struct in bytes.
func (s *Struct) Size() int {
	return s.size
}

type field struct {
	offset int
	size   int
}

// Accessor accesses a field within a byte slice.
type Accessor[T any] interface {
	Put(data []byte, x T)
	GenGet(dataExpr string) string
	GenPut(dataExpr, srcExpr string) string
	where() (offset, size int)
}

func AddInt[T ~int | ~uint64](s *Struct, maxVal T, goType string) Accessor[T] {
	a := intAccessor[T]{
		offset: s.size,
		size:   intSize(maxVal),
		goType: goType,
	}
	s.fields = append(s.fields, field{
		offset: a.offset,
		size:   a.size,
	})
	s.size += a.size
	return a
}

type intAccessor[T ~int | ~uint64] struct {
	offset int
	size   int
	goType string
}

func (a intAccessor[T]) Put(data []byte, x T) {
	if x < 0 {
		panic("negative values not supported")
	}
	PutUint64(data, a.offset, a.size, uint64(x))
}

func (a intAccessor[T]) GenGet(dataExpr string) string {
	return fmt.Sprintf("%s(genstruct.GetUint64(%s, %d, %d))", a.goType, dataExpr, a.offset, a.size)
}

func (a intAccessor[T]) GenPut(dataExpr string, srcExpr string) string {
	return fmt.Sprintf("genstruct.PutUint64(%s, %d, %d, uint64(%s))", dataExpr, a.offset, a.size, srcExpr)
}

func (a intAccessor[T]) where() (int, int) {
	return a.offset, a.size
}

// AddEnum adds an enum field to s that can have any of the given values.
// The go* parameters are used for generating code:
// - goValuesIdent is the Go identifier to use to hold the enum values;
// - goValueType holds the Go type to use;
// - goValues holds the Go expressions to use for each value corresponding to values.
// If defaultValue holds a member of values, it will be used as the default value
// when putting a value outside the set.
//
// If goValues is empty, "%#v" will be used instead.
func AddEnum[T comparable](s *Struct, values []T, defaultValue T, goValuesIdent, goValueType string, goValues []string) Accessor[T] {
	valueToIndex := make(map[T]int)
	for i, v := range values {
		valueToIndex[v] = i
	}

	a := enumAccessor[T]{
		a:                 AddInt(s, uint64(len(values)-1), ""),
		values:            values,
		valueToIndex:      valueToIndex,
		defaultValueIndex: -1,
		goValuesIdent:     goValuesIdent,
		goValueType:       goValueType,
		goValues:          goValues,
	}
	if i, ok := valueToIndex[defaultValue]; ok {
		a.defaultValueIndex = i
	}

	s.genInits = append(s.genInits, initInfo{
		genInit: a.genInit,
		ident:   a.goValuesIdent,
	})
	return a
}

type enumAccessor[T comparable] struct {
	a            Accessor[uint64]
	valueToIndex map[T]int
	values       []T

	defaultValueIndex int
	goValuesIdent     string
	goValueType       string
	goValues          []string
}

func (a enumAccessor[T]) where() (int, int) {
	return a.a.where()
}

func (a enumAccessor[T]) GenGet(dataExpr string) string {
	// e.g. DataFromEnum(data, 24, 4, somethings)
	offset, size := a.where()
	return fmt.Sprintf("genstruct.GetEnum(%s, %d, %d, %s)",
		dataExpr,
		offset,
		size,
		a.goValuesIdent,
	)
}

func (a enumAccessor[T]) GenPut(bytesExpr, srcExpr string) string {
	offset, size := a.where()
	return fmt.Sprintf("genstruct.PutEnum(%s, %d, %d, %s_rev, %d, %s)",
		bytesExpr,
		offset,
		size,
		a.goValuesIdent,
		a.defaultValueIndex,
		srcExpr,
	)
}

func (a enumAccessor[T]) genInit() string {
	var buf strings.Builder
	if len(a.goValues) > 0 {
		writeTable(&buf, a.goValuesIdent, a.goValueType, a.goValues)
	} else {
		writeTable(&buf, a.goValuesIdent, a.goValueType, a.values)
	}
	return buf.String()
}

func (a enumAccessor[T]) Put(data []byte, x T) {
	i, ok := a.valueToIndex[x]
	if !ok {
		panic(fmt.Errorf("set of value %#v outside enum set %v", x, a.values))
	}
	a.a.Put(data, uint64(i))
}

// AddSet adds a set field to s with the given possible values (index ordered).
// The variable with name goValuesIdent will be assigned the values,
// of type slice of goValueType.
func AddSet(s *Struct, values []string, goValuesIdent string) Accessor[iter.Seq[string]] {
	if len(values) == 0 {
		panic("empty set")
	}
	if len(values) > 64 {
		panic("more than 64 values in set")
	}
	valueToBit := make(map[string]uint64)
	for i, v := range values {
		valueToBit[v] = uint64(1 << i)
	}
	a := setAccessor{
		a:             AddInt(s, (uint64(1)<<len(values))-1, ""),
		values:        values,
		valueToBit:    valueToBit,
		goValuesIdent: goValuesIdent,
	}
	s.genInits = append(s.genInits, initInfo{
		genInit: a.genInit,
		ident:   a.goValuesIdent,
	})
	return a
}

type setAccessor struct {
	a             Accessor[uint64]
	valueToBit    map[string]uint64
	values        []string
	goValuesIdent string
}

func (a setAccessor) Put(data []byte, xs iter.Seq[string]) {
	var bits uint64
	for x := range xs {
		bit, ok := a.valueToBit[x]
		if !ok {
			panic(fmt.Errorf("set value %#v outside set", x))
		}
		bits |= bit
	}
	a.a.Put(data, bits)
}

func (a setAccessor) where() (int, int) {
	return a.a.where()
}

func (a setAccessor) GenGet(bytesExpr string) string {
	offset, size := a.where()
	return fmt.Sprintf("genstruct.GetSet(%s, %d, %d, %s)",
		bytesExpr,
		offset,
		size,
		a.goValuesIdent,
	)
}

func (a setAccessor) GenPut(bytesExpr string, srcExpr string) string {
	offset, size := a.a.where()
	return fmt.Sprintf("genstruct.PutSet(%s, %d, %d, %s, %s)",
		bytesExpr,
		offset,
		size,
		a.goValuesIdent+"_rev",
		srcExpr,
	)
}

func (a setAccessor) genInit() string {
	var buf strings.Builder
	writeTable(&buf, a.goValuesIdent, "string", a.values)
	return buf.String()
}

// AddEnumMap adds a field to s that contains a map of any of the
// possible keys, each of which can be any of the possible enum values.
// If defaultValue is a member of values, it will be used as a default
// value for values outside the set.
func AddEnumMap[T comparable](s *Struct, keys []string, values []T, defaultValue T, goIdent string) Accessor[iter.Seq2[string, T]] {
	if len(values) == 0 {
		panic("empty set")
	}
	// Note: one extra value (max is len(values) not len(values)-1) so
	// that we can encode "key-not-present" as the zero value.
	valueBits := bits.Len64(uint64(len(values)))
	totalBits := len(keys) * valueBits
	if totalBits > 64 {
		// TODO allow for arbitrary length
		panic("more than 2^64 possible values in map")
	}
	a := enumMapAccessor[T]{
		a:          AddInt(s, (uint64(1)<<totalBits)-1, ""),
		valueBits:  valueBits,
		keyIndex:   IndexMap(keys),
		valueIndex: IndexMap(values),
		keys:       keys,
		values:     values,
		goIdent:    goIdent,
	}
	s.genInits = append(s.genInits, initInfo{
		genInit: a.genInit,
		ident:   a.goIdent,
	})
	return a
}

type enumMapAccessor[T comparable] struct {
	a          Accessor[uint64]
	valueBits  int
	keyIndex   map[string]int
	valueIndex map[T]int
	keys       []string
	values     []T
	goIdent    string
}

func (a enumMapAccessor[T]) Put(data []byte, xs iter.Seq2[string, T]) {
	var bits uint64
	for key, val := range xs {
		ki, ok := a.keyIndex[key]
		if !ok {
			panic(fmt.Errorf("map key %#v outside possible range", key))
		}
		vi, ok := a.valueIndex[val]
		if !ok {
			panic(fmt.Errorf("map value %#v outside possible range", val))
		}
		shift := ki * a.valueBits
		// clear bits to guard against the possibility of having duplicate
		// keys in the sequence.
		bits &^= ((1 << a.valueBits) - 1) << shift // clear bits
		bits |= (uint64(vi) + 1) << shift
	}
	a.a.Put(data, bits)
}

func (a enumMapAccessor[T]) where() (int, int) {
	return a.a.where()
}

func (a enumMapAccessor[T]) GenGet(bytesExpr string) string {
	offset, size := a.where()
	return fmt.Sprintf("genstruct.GetEnumMap(%s, %d, %d, %s, %s)",
		bytesExpr,
		offset,
		size,
		a.goIdent+"_keys",
		a.goIdent+"_values",
	)
}

func (a enumMapAccessor[T]) GenPut(bytesExpr string, srcExpr string) string {
	offset, size := a.where()
	return fmt.Sprintf("genstruct.PutEnumMap(%s, %d, %d, %s, %s, -1, -1, %s)",
		bytesExpr,
		offset,
		size,
		a.goIdent+"_keys_rev",
		a.goIdent+"_values_rev",
		srcExpr,
	)
}

func (a enumMapAccessor[T]) genInit() string {
	var buf strings.Builder
	writeTable(&buf, a.goIdent+"_keys", "string", a.keys)
	writeTable(&buf, a.goIdent+"_values", "string", a.values)
	return buf.String()
}

func intSize[T ~int | ~uint64](i T) int {
	return (bits.Len64(uint64(i)) + 7) / 8
}

func writeTable[T any](w io.Writer, ident string, goType string, values []T) {
	fmt.Fprintf(w, "var (\n")
	fmt.Fprintf(w, "\t%s = []%s {\n", ident, goType)
	for _, v := range values {
		fmt.Fprintf(w, "\t\t%#v,\n", v)
	}
	fmt.Fprintf(w, "\t}\n")
	fmt.Fprintf(w, "\t%[1]s_rev = genstruct.IndexMap(%[1]s)\n", ident)
	fmt.Fprintf(w, ")\n")
}
