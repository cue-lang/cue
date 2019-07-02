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
type OrderedMap []KeyValue

// KeyValue associates a value with a key.
type KeyValue struct {
	Key   string
	Value interface{}
}

func (m *OrderedMap) prepend(key string, value interface{}) {
	*m = append([]KeyValue{{key, value}}, (*m)...)
}

// set sets a key value pair. If a pair with the same key already existed, it
// will be replaced with the new value. Otherwise, the new value is added to
// the end.
func (m *OrderedMap) set(key string, value interface{}) {
	for i, v := range *m {
		if v.Key == key {
			(*m)[i].Value = value
			return
		}
	}
	*m = append(*m, KeyValue{key, value})
}

// exists reports whether a key-value pair exists for the given key.
func (m OrderedMap) exists(key string) bool {
	for _, v := range m {
		if v.Key == key {
			return true
		}
	}
	return false
}

// MarshalJSON implements Marshal
func (m OrderedMap) MarshalJSON() (b []byte, err error) {
	b = append(b, '{')
	for i, v := range m {
		if i > 0 {
			b = append(b, ",\n"...)
		}
		key, err := json.Marshal(v.Key)
		if je, ok := err.(*json.MarshalerError); ok {
			return nil, je.Err
		}
		b = append(b, key...)
		b = append(b, ": "...)

		value, jerr := json.Marshal(v.Value)
		if je, ok := err.(*json.MarshalerError); ok {
			err = jerr
			value, _ = json.Marshal(je.Err.Error())
		}
		b = append(b, value...)
	}
	b = append(b, '}')
	return b, err
}
