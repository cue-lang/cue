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

package adt

import (
	"strconv"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// A Feature is an encoded form of a label which comprises a compact
// representation of an integer or string label as well as a label type.
type Feature uint32

// InvalidLabel is an encoding of an erroneous label.
const InvalidLabel Feature = 0x7 // 0xb111

// MaxIndex indicates the maximum number of unique strings that are used for
// labeles within this CUE implementation.
const MaxIndex int64 = 1<<28 - 1

// A StringIndexer coverts strings to and from an index that is unique for a
// given string.
type StringIndexer interface {
	// ToIndex returns a unique positive index for s (0 < index < 2^28-1).
	//
	// For each pair of strings s and t it must return the same index if and
	// only if s == t.
	StringToIndex(s string) (index int64)

	// ToString returns a string s for index such that ToIndex(s) == index.
	IndexToString(index int64) string
}

// SelectorString reports the shortest string representation of f when used as a
// selector.
func (f Feature) SelectorString(index StringIndexer) string {
	if f == 0 {
		return "_"
	}
	x := f.Index()
	switch f.Typ() {
	case IntLabel:
		return strconv.Itoa(int(x))
	case StringLabel:
		s := index.IndexToString(int64(x))
		if ast.IsValidIdent(s) && !internal.IsDefOrHidden(s) {
			return s
		}
		return strconv.Quote(s)
	default:
		return index.IndexToString(int64(x))
	}
}

// MakeLabel creates a label. It reports an error if the index is out of range.
func MakeLabel(p token.Pos, index int64, f FeatureType) (Feature, errors.Error) {
	if 0 > index || index > MaxIndex {
		return InvalidLabel,
			errors.Newf(p, "int label out of range (%d not >=0 and <= %d)",
				index, MaxIndex)
	}
	return Feature(index)<<indexShift | Feature(f), nil
}

// A FeatureType indicates the type of label.
type FeatureType int8

const (
	StringLabel           FeatureType = 0 // 0b000
	IntLabel              FeatureType = 1 // 0b001
	DefinitionLabel       FeatureType = 3 // 0b011
	HiddenLabel           FeatureType = 6 // 0b110
	HiddenDefinitionLabel FeatureType = 7 // 0b111

	// letLabel              FeatureType = 0b010

	fTypeMask Feature = 7 // 0b111

	indexShift = 3
)

// IsValid reports whether f is a valid label.
func (f Feature) IsValid() bool { return f != InvalidLabel }

// Typ reports the type of label.
func (f Feature) Typ() FeatureType { return FeatureType(f & fTypeMask) }

// IsRegular reports whether a label represents a data field.
func (f Feature) IsRegular() bool { return f.Typ() <= IntLabel }

// IsString reports whether a label represents a regular field.
func (f Feature) IsString() bool { return f.Typ() == StringLabel }

// IsDef reports whether the label is a definition (an identifier starting with
// # or #_.
func (f Feature) IsDef() bool {
	if f == InvalidLabel {
		// TODO(perf): do more mask trickery to avoid this branch.
		return false
	}
	return f.Typ()&DefinitionLabel == DefinitionLabel
}

// IsInt reports whether this is an integer index.
func (f Feature) IsInt() bool { return f.Typ() == IntLabel }

// IsHidden reports whether this label is hidden (an identifier starting with
// _ or #_).
func (f Feature) IsHidden() bool {
	if f == InvalidLabel {
		// TODO(perf): do more mask trickery to avoid this branch.
		return false
	}
	return f.Typ()&HiddenLabel == HiddenLabel
}

// Index reports the abstract index associated with f.
func (f Feature) Index() int { return int(f >> indexShift) }

// TODO: should let declarations be implemented as fields?
// func (f Feature) isLet() bool  { return f.typ() == letLabel }
