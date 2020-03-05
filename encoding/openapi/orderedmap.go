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

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/encoding/json"
)

// An OrderedMap is a set of key-value pairs that preserves the order in which
// items were added. It marshals to JSON as an object.
//
// Deprecated: the API now returns an ast.File. This allows OpenAPI to be
// represented as JSON, YAML, or CUE data, in addition to being able to use
// all the ast-related tooling.
type OrderedMap ast.StructLit

// KeyValue associates a value with a key.
type KeyValue struct {
	Key   string
	Value interface{}
}

func (m *OrderedMap) len() int {
	return len(m.Elts)
}

// Pairs returns the KeyValue pairs associated with m.
func (m *OrderedMap) Pairs() []KeyValue {
	kvs := make([]KeyValue, len(m.Elts))
	for i, e := range m.Elts {
		kvs[i].Key = label(e)
		kvs[i].Value = e.(*ast.Field).Value
	}
	return kvs
}

func (m *OrderedMap) find(key string) *ast.Field {
	for _, v := range m.Elts {
		f, ok := v.(*ast.Field)
		if !ok {
			continue
		}
		s, _, err := ast.LabelName(f.Label)
		if err == nil && s == key {
			return f
		}
	}
	return nil
}

// Set sets a key value pair. If a pair with the same key already existed, it
// will be replaced with the new value. Otherwise, the new value is added to
// the end.
func (m *OrderedMap) Set(key string, expr ast.Expr) {
	if f := m.find(key); f != nil {
		f.Value = expr
		return
	}
	m.Elts = append(m.Elts, &ast.Field{
		Label: ast.NewString(key),
		Value: expr,
	})
}

// exists reports whether a key-value pair exists for the given key.
func (m *OrderedMap) exists(key string) bool {
	return m.find(key) != nil
}

// exists reports whether a key-value pair exists for the given key.
func (m *OrderedMap) getMap(key string) *OrderedMap {
	f := m.find(key)
	if f == nil {
		return nil
	}
	return (*OrderedMap)(f.Value.(*ast.StructLit))
}

// MarshalJSON implements json.Marshaler.
func (m *OrderedMap) MarshalJSON() (b []byte, err error) {
	// This is a pointer receiever to enforce that we only store pointers to
	// OrderedMap in the output.
	return json.Encode((*ast.StructLit)(m))
}
