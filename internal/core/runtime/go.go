// Copyright 2020 CUE Authors
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

package runtime

import (
	"reflect"

	"cuelang.org/go/internal/core/adt"
)

func (x *Runtime) StoreType(t reflect.Type, expr adt.Expr) {
	x.index.StoreType(t, expr)
}

func (x *Runtime) LoadType(t reflect.Type) (adt.Expr, bool) {
	v, ok := x.index.LoadType(t)
	if !ok {
		return nil, false
	}
	return v.(adt.Expr), true
}

func (x *index) StoreType(t reflect.Type, v interface{}) {
	x.typeCache.Store(t, v)
}

func (x *index) LoadType(t reflect.Type) (v interface{}, ok bool) {
	v, ok = x.typeCache.Load(t)
	return v, ok
}
