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

package pkg

import (
	"iter"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
)

// A Schema represents an arbitrary cue.Value that can hold non-concrete values.
// By default function arguments are checked to be concrete.
type Schema = cue.Value

// A Validator is the result of a builtin that is intended for use as
// a validator: the value reports whether the builtin's first argument
// is acceptable. It aliases bool, so it changes neither the Go API
// nor how the builtin is registered; it marks intent for tooling that
// reads the Go signatures, distinguishing validators from builtins
// whose bool result is an ordinary value.
type Validator = bool

// List represents a CUE list, which can be open or closed.
type List struct {
	node   *adt.Vertex
	isOpen bool
}

// Elems returns the elements of a list.
func (l *List) Elems() iter.Seq[*adt.Vertex] {
	return l.node.Elems()
}

// IsOpen reports whether a list is open ended.
func (l *List) IsOpen() bool {
	return l.isOpen
}

// Struct represents a CUE struct, which can be open or closed.
type Struct struct {
	node *adt.Vertex
}

// NumFields reports the number of string fields of s which are present,
// required but not yet present, and optional, excluding pattern constraints.
func (s *Struct) NumFields() (present, required, optional int) {
	for _, a := range s.node.Arcs {
		if !a.Label.IsString() {
			continue
		}
		switch a.ArcType {
		case adt.ArcRequired:
			required++
		case adt.ArcOptional:
			optional++
		default:
			present++
		}
	}
	return present, required, optional
}

// IsOpen reports whether s is open or has pattern constraints.
func (s *Struct) IsOpen() bool {
	if s.node.IsOpenStruct() {
		return true
	}
	// Check for pattern constraints which indicate openness.
	if s.node.PatternConstraints != nil && len(s.node.PatternConstraints.Pairs) > 0 {
		return true
	}
	// After removing OptionalTypes, we rely on other checks for openness.
	return false
}

// A ValidationError indicates an error that is only valid if a builtin is used
// as a validator.
type ValidationError struct {
	B *adt.Bottom
}

func (v ValidationError) Error() string { return v.B.Err.Error() }
