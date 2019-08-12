// Copyright 2019 CUE Authors
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

package openapi

import "encoding/json"

// An OrderedMap is a set of key-value pairs that preserves the order in which
// items were added. It marshals to JSON as an object.
type OrderedMap struct {
	kvs []KeyValue
}

// KeyValue associates a value with a key.
type KeyValue struct {
	Key   string
	Value interface{}
}

// Pairs returns the KeyValue pairs associated with m.
func (m *OrderedMap) Pairs() []KeyValue {
	return m.kvs
}

// Set sets a key value pair. If a pair with the same key already existed, it
// will be replaced with the new value. Otherwise, the new value is added to
// the end.
func (m *OrderedMap) Set(key string, value interface{}) {
	for i, v := range m.kvs {
		if v.Key == key {
			m.kvs[i].Value = value
			return
		}
	}
	m.kvs = append(m.kvs, KeyValue{key, value})
}

// SetAll replaces existing key-value pairs with the given ones. The keys must
// be unique.
func (m *OrderedMap) SetAll(kvs []KeyValue) {
	m.kvs = kvs
}

// exists reports whether a key-value pair exists for the given key.
func (m *OrderedMap) exists(key string) bool {
	for _, v := range m.kvs {
		if v.Key == key {
			return true
		}
	}
	return false
}

// exists reports whether a key-value pair exists for the given key.
func (m *OrderedMap) getMap(key string) *OrderedMap {
	for _, v := range m.kvs {
		if v.Key == key {
			return v.Value.(*OrderedMap)
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (m *OrderedMap) MarshalJSON() (b []byte, err error) {
	// This is a pointer receiever to enforce that we only store pointers to
	// OrderedMap in the output.

	b = append(b, '{')
	for i, v := range m.kvs {
		if i > 0 {
			b = append(b, ",\n"...)
		}
		key, ferr := json.Marshal(v.Key)
		if je, ok := ferr.(*json.MarshalerError); ok {
			return nil, je.Err
		}
		b = append(b, key...)
		b = append(b, ": "...)

		value, jerr := json.Marshal(v.Value)
		if je, ok := jerr.(*json.MarshalerError); ok {
			err = jerr
			value, _ = json.Marshal(je.Err.Error())
		}
		b = append(b, value...)
	}
	b = append(b, '}')
	return b, err
}
