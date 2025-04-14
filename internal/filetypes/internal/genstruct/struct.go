// Package genstruct provides support for simple compact struct representations.
// It is a standalone package so it can be imported by both the filetypes
// generator code and the filetypes package itself.
package genstruct

import (
	"encoding/binary"
	"fmt"
	"iter"
	"math/bits"
	"strings"
)

// Struct represents a binary-marshalable struct.
type Struct struct {
	fields   []field
	size     int
	genInits []func() string
}

func (s *Struct) GenInit() string {
	var buf strings.Builder
	for _, genInit := range s.genInits {
		buf.WriteString(genInit())
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
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(x))
	copy(data[a.offset:a.offset+a.size], buf[:a.size])
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
		a:                 AddInt(s, uint64(len(values)), ""),
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

	s.genInits = append(s.genInits, a.genInit)
	return a
}

type enumAccessor[T comparable] struct {
	a            Accessor[uint64]
	valueToIndex map[T]int
	values       []T

	defaultValueIndex  int
	goValuesIdent      string
	goValueType        string
	goValues           []string
	goDefaultValueExpr string
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
	fmt.Fprintf(&buf, "var (\n")
	fmt.Fprintf(&buf, "\t%s = []%s {\n",
		a.goValuesIdent,
		a.goValueType,
	)
	if len(a.goValues) > 0 {
		for _, v := range a.goValues {
			fmt.Fprintf(&buf, "\t\t%#v,\n", v)
		}
	} else {
		for _, v := range a.values {
			fmt.Fprintf(&buf, "\t\t%#v,\n", v)
		}
	}
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "\t%[1]s_rev = genstruct.IndexMap(%[1]s)\n", a.goValuesIdent)
	fmt.Fprintf(&buf, ")\n")
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
	s.genInits = append(s.genInits, a.genInit)
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
	// e.g. SetEnum(&x.Foo, data, 24, 4, somethings)
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
	fmt.Fprintf(&buf, "var (\n")
	fmt.Fprintf(&buf, "\t%s = []string {\n",
		a.goValuesIdent,
	)
	for _, v := range a.values {
		fmt.Fprintf(&buf, "\t\t%q,\n", v)
	}
	fmt.Fprintf(&buf, "\t}\n")
	fmt.Fprintf(&buf, "\t%[1]s_rev = genstruct.IndexMap(%[1]s)\n", a.goValuesIdent)
	fmt.Fprintf(&buf, ")\n")
	return buf.String()
}

func intSize[T ~int | ~uint64](i T) int {
	return (bits.Len64(uint64(i)) + 7) / 8
}
