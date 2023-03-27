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
	"cuelang.org/go/internal/pkg"
)

// builtin attempts to load the named function of type typ from the
// instance, returning it as an *adt.Builtin if successful, otherwise
// returning any encountered errors.
func builtin(name string, typ fnTyp, i *instance) (*adt.Builtin, error) {
	b, err := loadBuiltin(name, typ, i)
	if err != nil {
		return nil, err
	}
	return pkg.ToBuiltin(b), nil
}

// loadBuiltin attempts to load the named function of type typ from
// the instance, returning it as an *pkg.Builtin if successful, otherwise
// returning any encountered errors.
func loadBuiltin(name string, typ fnTyp, i *instance) (*pkg.Builtin, error) {
	fn, err := i.load(name)
	if err != nil {
		return nil, err
	}
	b := &pkg.Builtin{
		Name:   name,
		Params: params(typ),
		Result: typ.Ret.kind(),
		Func:   i.callCtxFunc(fn, typ),
	}
	return b, nil
}
