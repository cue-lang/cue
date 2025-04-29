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

package genstruct

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"iter"
	"math/bits"
	"sort"
)

// GetEnum returns the enum value held in data
// at the given offset and size, where values holds all
// the possible values of the enum.
//
// This is designed to be used in the generated code target, not the generator.
func GetEnum[T any](data []byte, offset, size int, values []T) T {
	return values[GetUint64(data, offset, size)]
}

// PutEnum writes the enum value x to data at the given offset and size, using valueMap
// to determine the actual numeric value to be written. If defaultIndex is non-negative,
// it determines the value to use if x is not present.
func PutEnum[T comparable](data []byte, offset, size int, valueMap map[T]int, defaultIndex int, x T) error {
	i, ok := valueMap[x]
	if !ok {
		if defaultIndex >= 0 {
			i = defaultIndex
		} else {
			return fmt.Errorf("value %#v not found in enum", x)
		}
	}
	PutUint64(data, offset, size, uint64(i))
	return nil
}

// GetSet returns an iterator through all the items in the set stored
// in data[offset:offset+size], where values holds all the members of the
// set in index order.
func GetSet[T comparable](data []byte, offset, size int, values []T) iter.Seq[T] {
	return ElemsFromBits(GetUint64(data, offset, size), values)
}

// GetEnumMap returns an iterator through all the keys and values in
// data[offset:offset+size], where keys and values hold the possible
// keys and values respectively, in index order.
func GetEnumMap(data []byte, offset, size int, keys, values []string) iter.Seq2[string, string] {
	return func(yield func(k, v string) bool) {
		x := GetUint64(data, offset, size)
		valueBits := bits.Len64(uint64(len(values)))
		mask := uint64(1)<<valueBits - 1
		for x != 0 {
			i := bits.TrailingZeros64(x)
			// We've found a single bit, but that might be anywhere inside
			// the value. Round down to find the actual offset.
			keyIndex := i / valueBits
			shift := keyIndex * valueBits
			valueIndex := (x>>shift)&mask - 1
			if !yield(keys[keyIndex], values[valueIndex]) {
				return
			}
			x &^= mask << shift
		}
	}
}

// PutEnumMap puts the keys and values in x into data at the given offset and size, where valueBits holds
// the number of bits per value in the map and valueMap and keyMap give indexes for each possible
// key and value, and defaultKeyIndex/defaultValueIndex provide values to use in case a given
// value isn't one of the allowed keys or values.
func PutEnumMap(data []byte, offset, size int, valueBits int, valueMap, keyMap map[string]int, defaultKeyIndex, defaultValueIndex int, x iter.Seq2[string, string]) error {
	var bits uint64
	mask := (uint64(1) << valueBits) - 1
	for k, v := range x {
		ki, ok := keyMap[k]
		if !ok {
			if defaultKeyIndex == -1 {
				return fmt.Errorf("unknown key %#v", k)
			}
			ki = defaultKeyIndex
		}
		vi, ok := valueMap[v]
		if !ok {
			if defaultValueIndex == -1 {
				return fmt.Errorf("unknown value %#v", v)
			}
			vi = defaultValueIndex
		}
		shift := ki * valueBits
		bits &^= mask << shift
		bits |= (uint64(vi) + 1) << shift
	}
	PutUint64(data, offset, size, bits)
	return nil
}

// ElemsFromBits returns an iterator over all the items in the bitset
// x, where bit 1<<i implies that values[i] is in the set.
func ElemsFromBits[T comparable](x uint64, values []T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := range ones64(x) {
			if !yield(values[i]) {
				return
			}
		}
	}
}

// Ones64 returns an iterator over all the non-zero bits of x.
func ones64(x uint64) iter.Seq[int] {
	return func(yield func(int) bool) {
		for x != 0 {
			i := bits.TrailingZeros64(x)
			if !yield(i) {
				return
			}
			x &^= 1 << i
		}
	}
}

// PutSet writes the value of the set comprising all the items read from items
// to data[offset:offset+size], where values holds all the possible members of the
// set mapped to their respective bitmasks.
//
// This is designed to be used in the generated code target, not the generator.
func PutSet[T comparable](data []byte, offset, size int, values map[T]int, items iter.Seq[T]) error {
	var bits uint64
	for x := range items {
		i, ok := values[x]
		if !ok {
			return fmt.Errorf("value %#v not found in set", x)
		}
		bits |= 1 << i
	}
	PutUint64(data, offset, size, bits)
	return nil
}

// IndexMap returns a map from value to index in the value.
// It assumes that all values are distinct.
func IndexMap[T comparable](values []T) map[T]int {
	m := make(map[T]int)
	for i, x := range values {
		m[x] = i
	}
	return m
}

// PutUint64 writes x to data at the given offset and size.
// It assumes that size is large enough to hold the value.
func PutUint64(data []byte, offset, size int, x uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], x)
	copy(data[offset:], buf[:size])
}

// GetUint64 returns the numeric value at the given offset and size
// within data.
func GetUint64(data []byte, offset, size int) uint64 {
	// Note: use a temporary buffer because we always
	// need 8 bytes to be available for reading the uint64
	// data.
	var buf [8]byte
	copy(buf[:size], data[offset:])
	return binary.LittleEndian.Uint64(buf[:])
}

// FindRecord searches for the record with the given key in data, where
// each record in data is of the given size and is prefixed with a key
// of the same length as key. Records are sorted lexicographically
// by key.
//
// On success, it returns the part of the record following the key.
func FindRecord(data []byte, recordSize int, key []byte) ([]byte, bool) {
	i, ok := sort.Find(len(data)/recordSize, func(i int) int {
		return bytes.Compare(key, recordAt(data, recordSize, i)[:len(key)])
	})
	if ok {
		return recordAt(data, recordSize, i)[len(key):], true
	}
	return nil, false
}

// SortRecords sorts the records in the given data as required
// by [FindRecord].
func SortRecords(data []byte, recordSize, keySize int) {
	r := &records{
		data:       data,
		recordSize: recordSize,
		keySize:    keySize,
		buf:        make([]byte, recordSize),
	}
	sort.Sort(r)
}

// records implements [sort.Interface] for a slice of records.
type records struct {
	data       []byte
	recordSize int
	keySize    int
	buf        []byte
}

func (r *records) Len() int {
	return len(r.data) / r.recordSize
}

func (r *records) Less(i, j int) bool {
	return bytes.Compare(r.keyAt(i), r.keyAt(j)) < 0
}

func (r *records) Swap(i, j int) {
	copy(r.buf, r.at(i))
	copy(r.at(i), r.at(j))
	copy(r.at(j), r.buf)
}

func (r *records) keyAt(i int) []byte {
	return r.at(i)[:r.keySize]
}

func (r *records) at(i int) []byte {
	return recordAt(r.data, r.recordSize, i)
}

func recordAt(data []byte, recordSize int, i int) []byte {
	start := i * recordSize
	return data[start : start+recordSize]
}
