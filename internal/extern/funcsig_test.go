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

package extern_test

import (
	"reflect"
	"testing"

	"cuelang.org/go/internal/extern"
)

type inOut struct {
	in  string
	out string
	fn  extern.FuncSig
}

var goodFuncDefs = []inOut{
	{
		"func(): bool",
		"func(): bool",
		extern.FuncSig{
			Args: nil,
			Ret:  "bool",
		},
	}, {
		"func(int32, int32): int32",
		"func(int32, int32): int32",
		extern.FuncSig{
			Args: []string{"int32", "int32"},
			Ret:  "int32",
		},
	}, {
		"func(int32, int32): int32   ",
		"func(int32, int32): int32",
		extern.FuncSig{
			Args: []string{"int32", "int32"},
			Ret:  "int32",
		},
	}, {
		"    func(int32, int32): int32",
		"func(int32, int32): int32",
		extern.FuncSig{
			Args: []string{"int32", "int32"},
			Ret:  "int32",
		},
	}, {
		"    func(int32, int32): int32    ",
		"func(int32, int32): int32",
		extern.FuncSig{
			Args: []string{"int32", "int32"},
			Ret:  "int32",
		},
	}, {
		"func(float64, float64): float64",
		"func(float64, float64): float64",
		extern.FuncSig{
			Args: []string{"float64", "float64"},
			Ret:  "float64",
		},
	}, {
		"func(float64,    float64):    float64",
		"func(float64, float64): float64",
		extern.FuncSig{
			Args: []string{"float64", "float64"},
			Ret:  "float64",
		},
	}, {
		"func(uint64): bool",
		"func(uint64): bool",
		extern.FuncSig{
			Args: []string{"uint64"},
			Ret:  "bool",
		},
	}, {
		"func(uint64): uint64",
		"func(uint64): uint64",
		extern.FuncSig{
			Args: []string{"uint64"},
			Ret:  "uint64",
		},
	},
}

func TestParseOneFuncSigSuccess(t *testing.T) {
	for _, v := range goodFuncDefs {
		fn, err := extern.ParseOneFuncSig(v.in)
		if err != nil {
			t.Errorf("failed to parse %q: %v", v.in, err)
		}
		if v.out != fn.String() {
			t.Errorf("want %q, got %q", v.out, fn)
		}
		if !reflect.DeepEqual(*fn, v.fn) {
			t.Errorf("want %#v, got %#v", v.fn, fn)
		}
	}
}

type inErr struct {
	in     string
	errStr string
}

var badFuncDefs = []inErr{
	{
		"",
		`expected keyword "func", got EOF`,
	}, {
		"$",
		`expected keyword "func", got unexpected token`,
	}, {
		")",
		`expected keyword "func", got ')'`,
	}, {
		"int32",
		`expected keyword "func", got identifier`,
	}, {
		"func",
		`expected '(', got EOF`,
	}, {
		"func(",
		`expected ')', got EOF`,
	}, {
		"func(int32",
		`expected ')', got EOF`,
	}, {
		"func(int32,",
		`expected identifier, got EOF`,
	}, {
		"func(int32,)",
		`expected identifier, got ')'`,
	}, {
		"func(int32)",
		`expected ':', got EOF`,
	}, {
		"func(int32):",
		`expected identifier, got EOF`,
	}, {
		"func(,): bool",
		`expected identifier, got ','`,
	}, {
		"func(, uint32): bool",
		`expected identifier, got ','`,
	}, {
		"func(): :",
		`expected identifier, got ':'`,
	}, {
		"func(): func",
		`expected identifier, got keyword "func"`,
	}, {
		"func($): func",
		`expected ')', got unexpected token`,
	}, {
		"func(): bool ,",
		`expected EOF, got ","`,
	}, {
		"func(): bool bool",
		`expected EOF, got "bool"`,
	},
}

func TestParseOneFuncSigError(t *testing.T) {
	for _, v := range badFuncDefs {
		_, err := extern.ParseOneFuncSig(v.in)
		if err == nil {
			t.Errorf("unexpected success parsing %q, expected %q", v.in, v.errStr)
		}
		if v.errStr != err.Error() {
			t.Errorf("want %q, got %q", v.errStr, err.Error())
		}
	}
}
