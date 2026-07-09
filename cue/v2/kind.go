// Copyright 2026 The CUE Authors
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

package cue

import (
	"strings"

	"cuelang.org/go/internal/core/adt"
)

// Kind reports the type of a CUE value.
type Kind int

const (
	// BottomKind represents all errors and the absence of a value.
	BottomKind Kind = 1 << iota

	NullKind
	BoolKind
	IntKind
	FloatKind
	StringKind
	BytesKind
	StructKind
	ListKind

	// FuncKind represents a function value, either built in or provided
	// by the host program via [NewFunc] and friends.
	FuncKind

	// NumberKind combines IntKind and FloatKind.
	NumberKind = IntKind | FloatKind

	// TopKind is the disjunction of all kinds except BottomKind.
	TopKind = NullKind | BoolKind | NumberKind | StringKind | BytesKind |
		StructKind | ListKind | FuncKind
)

// String returns the name of k.
func (k Kind) String() string {
	switch k {
	case BottomKind:
		return "_|_"
	case TopKind:
		return "_"
	}
	var parts []string
	if k&BottomKind != 0 {
		parts = append(parts, "_|_")
		k &^= BottomKind
	}
	for _, e := range kindNames {
		if k&e.kind == e.kind {
			parts = append(parts, e.name)
			k &^= e.kind
		}
	}
	switch len(parts) {
	case 0:
		return "_|_" // no bits set at all
	case 1:
		return parts[0]
	}
	return "(" + strings.Join(parts, "|") + ")"
}

// kindNames lists the names of the individual kinds, with NumberKind
// first so that an int|float combination renders as "number".
var kindNames = []struct {
	kind Kind
	name string
}{
	{NumberKind, "number"},
	{NullKind, "null"},
	{BoolKind, "bool"},
	{IntKind, "int"},
	{FloatKind, "float"},
	{StringKind, "string"},
	{BytesKind, "bytes"},
	{StructKind, "struct"},
	{ListKind, "list"},
	{FuncKind, "func"},
}

// kindPairs relates the kind representation of this package to that of
// the internal evaluator. The bit layouts differ, so kinds are mapped
// bit by bit.
var kindPairs = []struct {
	kind    Kind
	adtKind adt.Kind
}{
	{NullKind, adt.NullKind},
	{BoolKind, adt.BoolKind},
	{IntKind, adt.IntKind},
	{FloatKind, adt.FloatKind},
	{StringKind, adt.StringKind},
	{BytesKind, adt.BytesKind},
	{StructKind, adt.StructKind},
	{ListKind, adt.ListKind},
	{FuncKind, adt.FuncKind},
}

// kindFromADT maps an internal kind mask to a Kind. An empty mask maps
// to BottomKind.
func kindFromADT(k adt.Kind) Kind {
	var r Kind
	for _, p := range kindPairs {
		if k&p.adtKind != 0 {
			r |= p.kind
		}
	}
	if r == 0 {
		return BottomKind
	}
	return r
}
