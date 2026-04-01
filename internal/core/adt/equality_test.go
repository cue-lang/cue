// Copyright 2026 CUE Authors
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

package adt

import (
	"testing"
)

// TestEqualTerminal_BuiltinValidator checks equality of BuiltinValidator values.
func TestEqualTerminal_BuiltinValidator(t *testing.T) {
	b1 := &Builtin{Name: "MaxFields"}
	b2 := &Builtin{Name: "MinFields"}

	// Use BasicType values as stand-in args: BasicType equality is purely
	// structural (Kind comparison), so it exercises the recursive Equal call
	// without requiring a fully-wired OpContext.
	intArg := &BasicType{K: IntKind}
	strArg := &BasicType{K: StringKind}

	tests := []struct {
		name string
		v, w Value
		want bool
	}{
		{
			name: "same builtin same args equal",
			v:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg}},
			w:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg}},
			want: true,
		},
		{
			name: "same builtin different args not equal",
			v:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg}},
			w:    &BuiltinValidator{Builtin: b1, Args: []Value{strArg}},
			want: false,
		},
		{
			name: "different builtins same args not equal",
			v:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg}},
			w:    &BuiltinValidator{Builtin: b2, Args: []Value{intArg}},
			want: false,
		},
		{
			name: "different arg counts not equal",
			v:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg}},
			w:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg, strArg}},
			want: false,
		},
		{
			name: "no-arg validators same builtin equal",
			v:    &BuiltinValidator{Builtin: b1},
			w:    &BuiltinValidator{Builtin: b1},
			want: true,
		},
		{
			name: "no-arg validators different builtins not equal",
			v:    &BuiltinValidator{Builtin: b1},
			w:    &BuiltinValidator{Builtin: b2},
			want: false,
		},
		{
			name: "validator vs non-validator not equal",
			v:    &BuiltinValidator{Builtin: b1, Args: []Value{intArg}},
			w:    &BasicType{K: StructKind},
			want: false,
		},
	}

	ctx := &OpContext{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := equalTerminal(ctx, tt.v, tt.w, 0)
			if got != tt.want {
				t.Errorf("equalTerminal(%T, %T) = %v, want %v", tt.v, tt.w, got, tt.want)
			}
		})
	}
}
