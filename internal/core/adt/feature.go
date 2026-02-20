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
	"cmp"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"unique"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// A FeatureType indicates the type of label.
type FeatureType int8

const (
	InvalidLabelType FeatureType = iota
	StringLabel
	IntLabel
	DefinitionLabel
	HiddenLabel
	HiddenDefinitionLabel
	LetLabel
)

func (f FeatureType) IsDef() bool {
	return f == DefinitionLabel || f == HiddenDefinitionLabel
}

func (f FeatureType) IsHidden() bool {
	return f == HiddenLabel || f == HiddenDefinitionLabel
}

func (f FeatureType) IsLet() bool {
	return f == LetLabel
}

type feature struct {
	typ   FeatureType
	any   bool
	index int64  // integer value for IntLabel; unique ID for LetLabel; 0 otherwise
	str   string // identifier/string value; empty for IntLabel
	pkgID string // package path for hidden labels; empty otherwise
}

// A Feature is an encoded form of a label which comprises a compact
// representation of an integer or string label as well as a label type.
type Feature struct {
	h unique.Handle[feature]
}

// InvalidLabel is a Feature that represents an erroneous label.
// It is the zero value of Feature.
var InvalidLabel Feature

// These labels can be used for wildcard queries.
var (
	AnyDefinition = makeLabel(feature{typ: DefinitionLabel, any: true})
	AnyHidden     = makeLabel(feature{typ: HiddenLabel, any: true})
	AnyString     = makeLabel(feature{typ: StringLabel, any: true})
	AnyIndex      = makeLabel(feature{typ: IntLabel, any: true})
)

func makeLabel(f feature) Feature {
	return Feature{h: unique.Make(f)}
}

// value returns the feature data, handling the zero-value (InvalidLabel) case.
func (f Feature) value() feature {
	if f == InvalidLabel {
		return feature{}
	}
	return f.h.Value()
}

// IsValid reports whether f is a valid label.
func (f Feature) IsValid() bool { return f != InvalidLabel }

// Typ reports the type of label.
func (f Feature) Typ() FeatureType { return f.value().typ }

// IsAny reports whether f represents a wildcard label.
func (f Feature) IsAny() bool { return f.value().any }

// IsRegular reports whether a label represents a data field.
func (f Feature) IsRegular() bool {
	t := f.Typ()
	return t == IntLabel || t == StringLabel
}

// IsString reports whether a label represents a regular string field.
func (f Feature) IsString() bool { return f.Typ() == StringLabel }

// IsDef reports whether the label is a definition (an identifier starting with
// # or _#).
func (f Feature) IsDef() bool { return f.Typ().IsDef() }

// IsInt reports whether this is an integer index.
func (f Feature) IsInt() bool { return f.Typ() == IntLabel }

// IsHidden reports whether this label is hidden (an identifier starting with
// _ or _#).
func (f Feature) IsHidden() bool { return f.Typ().IsHidden() }

// IsLet reports whether this label is a let field (like `let X = value`).
func (f Feature) IsLet() bool { return f.Typ().IsLet() }

// Index reports the integer index associated with f.
// For integer labels, this is the actual index value.
// For let labels, this is the unique ID.
// For other labels, it returns 0.
func (f Feature) Index() int { return int(f.value().index) }

// Compare returns an integer comparing two features.
// It provides a deterministic ordering for sorting.
func (f Feature) Compare(g Feature) int {
	if f == g {
		return 0
	}
	fv, gv := f.value(), g.value()
	if c := cmp.Compare(fv.typ, gv.typ); c != 0 {
		return c
	}
	if fv.any != gv.any {
		if fv.any {
			return 1
		}
		return -1
	}
	if c := cmp.Compare(fv.index, gv.index); c != 0 {
		return c
	}
	if c := cmp.Compare(fv.str, gv.str); c != 0 {
		return c
	}
	return cmp.Compare(fv.pkgID, gv.pkgID)
}

// SelectorString reports the shortest string representation of f when used as a
// selector.
func (f Feature) SelectorString() string {
	switch f.Typ() {
	case IntLabel:
		if f.IsAny() {
			return "_"
		}
		return strconv.Itoa(f.Index())
	case StringLabel:
		if f.IsAny() {
			return "_"
		}
		s := f.value().str
		if ast.StringLabelNeedsQuoting(s) {
			return literal.Label.Quote(s)
		}
		return s
	default:
		return f.IdentString()
	}
}

// IdentString reports the identifier of f. The result is undefined if f
// is not an identifier label.
func (f Feature) IdentString() string {
	return f.value().str
}

// PkgID returns the package identifier, composed of the module and package
// name, associated with this identifier. It will return "" if this is not
// a hidden label.
func (f Feature) PkgID() string {
	if !f.IsHidden() {
		return ""
	}
	return f.value().pkgID
}

// StringValue reports the string value of f, which must be a string label.
func (f Feature) StringValue() string {
	if !f.IsString() {
		panic("not a string label")
	}
	return f.value().str
}

// RawString reports the underlying string value of f without interpretation.
func (f Feature) RawString() string {
	return f.value().str
}

// ToValue converts a label to a value, which will be a Num for integer labels
// and a String for string labels. It panics when f is not a regular label.
func (f Feature) ToValue(ctx *OpContext) Value {
	if !f.IsRegular() {
		panic("not a regular label")
	}
	if f.IsInt() {
		return ctx.NewInt64(int64(f.Index()))
	}
	return ctx.NewString(f.value().str)
}

// StringLabel converts s to a string label.
func (c *OpContext) StringLabel(s string) Feature {
	return MakeStringLabel(s)
}

// MakeStringLabel creates a label for the given string.
func MakeStringLabel(s string) Feature {
	return makeLabel(feature{typ: StringLabel, str: s})
}

// MakeIdentLabel creates a label for the given identifier.
func MakeIdentLabel(s, pkgpath string) Feature {
	t := StringLabel
	var pkgID string
	switch {
	case strings.HasPrefix(s, "_#"):
		t = HiddenDefinitionLabel
		pkgID = pkgpath
	case strings.HasPrefix(s, "#"):
		t = DefinitionLabel
	case strings.HasPrefix(s, "_"):
		t = HiddenLabel
		pkgID = pkgpath
	}
	return makeLabel(feature{typ: t, str: s, pkgID: pkgID})
}

// HiddenKey constructs the uniquely identifying string for a hidden field and
// its package.
func HiddenKey(s, pkgPath string) string {
	return fmt.Sprintf("%s\x00%s", s, pkgPath)
}

// MakeNamedLabel creates a feature for the given name and feature type.
func MakeNamedLabel(t FeatureType, s string) Feature {
	return makeLabel(feature{typ: t, str: s})
}

// MakeLetLabel creates a label for the given let identifier s.
//
// A let declaration is always logically unique within its scope and will never
// unify with a let field of another struct. This is enforced by ensuring that
// the let identifier is unique across an entire configuration. This, in turn,
// is done by assigning a unique index to each let label.
func MakeLetLabel(s string) Feature {
	id := nextUniqueID.Add(1)
	return makeLabel(feature{typ: LetLabel, index: int64(id), str: s})
}

var nextUniqueID atomic.Uint64

// MakeIntLabel creates an integer label.
func MakeIntLabel(t FeatureType, i int64) Feature {
	if i < 0 {
		panic("index out of range")
	}
	return makeLabel(feature{typ: t, index: i})
}

const msgGround = "invalid non-ground value %s (must be concrete %s)"

// LabelFromValue converts a CUE value to a Feature label.
// It handles integer and string values, reporting errors via c for
// non-ground, out-of-range, or otherwise invalid values.
func LabelFromValue(c *OpContext, src Expr, v Value) Feature {
	v, _ = c.getDefault(v)

	if isError(v) {
		return InvalidLabel
	}
	switch v.Kind() {
	case IntKind, NumberKind:
		x, _ := Unwrap(v).(*Num)
		if x == nil {
			c.addErrf(IncompleteError, Pos(v), msgGround, v, "int")
			return InvalidLabel
		}
		i, err := x.X.Int64()
		if err != nil || x.K != IntKind {
			if src == nil {
				src = v
			}
			c.AddErrf("invalid index %v: %v", src, err)
			return InvalidLabel
		}
		if i < 0 {
			switch src.(type) {
			case nil, *Num, *UnaryExpr:
				c.AddErrf("invalid index %v (index must be non-negative)", x)
			default:
				c.AddErrf("index %v out of range [%v]", src, x)
			}
			return InvalidLabel
		}
		return MakeIntLabel(IntLabel, i)

	case StringKind:
		x, _ := Unwrap(v).(*String)
		if x == nil {
			c.addErrf(IncompleteError, Pos(v), msgGround, v, "string")
			return InvalidLabel
		}
		return MakeStringLabel(x.Str)

	default:
		if src != nil {
			c.AddErrf("invalid index %s (invalid type %v)", src, v.Kind())
		} else {
			c.AddErrf("invalid index type %v", v.Kind())
		}
		return InvalidLabel
	}
}

// MaxIntLabel is the maximum allowed integer label index.
// Although the Feature representation can hold any int64, we limit
// to this value to catch unreasonable indices early.
const MaxIntLabel = 1<<28 - 2

// TryMakeIntLabel is like MakeIntLabel but returns an error if the index
// is out of range.
func TryMakeIntLabel(src ast.Node, i int64, t FeatureType) (Feature, errors.Error) {
	if 0 > i || i > MaxIntLabel {
		p := token.NoPos
		if src != nil {
			p = src.Pos()
		}
		return InvalidLabel,
			errors.Newf(p, "int label out of range (%d not >=0 and <= %d)",
				i, MaxIntLabel)
	}
	return MakeIntLabel(t, i), nil
}
