// Copyright 2022 CUE Authors
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

package internal

import (
	"cuelang.org/go/internal/core/adt"
)

// List represents a CUE list, which can be open or closed.
type List struct {
	runtime adt.Runtime
	node    *adt.Vertex
	isOpen  bool
}

// Elems returns the elements of a list.
func (l *List) Elems() []*adt.Vertex {
	return l.node.Elems()
}

// IsOpen reports whether a list is open ended.
func (l *List) IsOpen() bool {
	return l.isOpen
}

// Struct represents a CUE struct, which can be open or closed.
type Struct struct {
	R adt.Runtime
	V *adt.Vertex
}

// IsOpen reports whether a list is open ended.
func (l *Struct) IsOpen() bool {
	return !l.V.IsClosedStruct()
}

// A ValidationError indicates an error that is only valid if a builtin is used
// as a validator.
type ValidationError struct {
	B *adt.Bottom
}

func (v ValidationError) Error() string { return v.B.Err.Error() }
