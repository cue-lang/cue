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

type orderedMap []kvPair

type kvPair struct {
	key   string
	value interface{}
}

func (m *orderedMap) Prepend(key string, value interface{}) {
	*m = append([]kvPair{{key, value}}, (*m)...)
}

func (m *orderedMap) Set(key string, value interface{}) {
	for i, v := range *m {
		if v.key == key {
			(*m)[i].value = value
			return
		}
	}
	*m = append(*m, kvPair{key, value})
}

func (m *orderedMap) Exists(key string) bool {
	for _, v := range *m {
		if v.key == key {
			return true
		}
	}
	return false
}

func (m *orderedMap) MarshalJSON() (b []byte, err error) {
	b = append(b, '{')
	for i, v := range *m {
		if i > 0 {
			b = append(b, ",\n"...)
		}
		key, err := json.Marshal(v.key)
		if je, ok := err.(*json.MarshalerError); ok {
			return nil, je.Err
		}
		b = append(b, key...)
		b = append(b, ": "...)

		value, err := json.Marshal(v.value)
		if je, ok := err.(*json.MarshalerError); ok {
			// return nil, je.Err
			value, _ = json.Marshal(je.Err.Error())
		}
		b = append(b, value...)
	}
	b = append(b, '}')
	return b, nil
}
