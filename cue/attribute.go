// Copyright 2021 CUE Authors
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
	"fmt"
	"iter"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/export"
)

// Attribute returns the attribute data for the given key.
// The returned attribute will return an error for any of its methods if there
// is no attribute for the requested key.
func (v Value) Attribute(key string) Attribute {
	// look up the attributes
	if v.v == nil {
		return nonExistAttr(key)
	}
	// look up the attributes
	for _, a := range export.ExtractFieldAttrs(v.v) {
		k, _ := a.Split()
		if key != k {
			continue
		}
		return newAttr(internal.FieldAttr, a)
	}

	return nonExistAttr(key)
}

func newAttr(k internal.AttrKind, a *ast.Attribute) Attribute {
	return Attribute{*internal.ParseAttr(a)}
}

func nonExistAttr(key string) Attribute {
	a := internal.NewNonExisting(key)
	a.Name = key
	a.Kind = internal.FieldAttr
	return Attribute{a}
}

// Attributes reports all field attributes for the Value.
//
// To retrieve attributes of multiple kinds, you can bitwise-or kinds together.
// Use ValueKind to query attributes associated with a value.
func (v Value) Attributes(mask AttrKind) []Attribute {
	if v.v == nil {
		return nil
	}

	attrs := []Attribute{}

	if mask&FieldAttr != 0 {
		for _, a := range export.ExtractFieldAttrs(v.v) {
			attrs = append(attrs, newAttr(internal.FieldAttr, a))
		}
	}

	if mask&DeclAttr != 0 {
		for _, a := range export.ExtractDeclAttrs(v.v) {
			attrs = append(attrs, newAttr(internal.DeclAttr, a))
		}
	}

	return attrs
}

// AttrKind indicates the location of an attribute within CUE source.
type AttrKind int

const (
	// FieldAttr indicates a field attribute.
	// foo: bar @attr()
	FieldAttr AttrKind = AttrKind(internal.FieldAttr)

	// DeclAttr indicates a declaration attribute.
	// foo: {
	//     @attr()
	// }
	DeclAttr AttrKind = AttrKind(internal.DeclAttr)

	// A ValueAttr is a bit mask to request any attribute that is locally
	// associated with a field, instead of, for instance, an entire file.
	ValueAttr AttrKind = FieldAttr | DeclAttr

	// TODO: Possible future attr kinds
	// ElemAttr (is a ValueAttr)
	// FileAttr (not a ValueAttr)

	// TODO: Merge: merge namesake attributes.
)

// An Attribute contains metadata about a field.
//
// By convention, an attribute is split into positional arguments
// according to the rules below. However, these are not mandatory.
// To access the raw contents of an attribute, use [Attribute.Contents].
//
// Arguments are of the form key[=value] where key and value each
// consist of an arbitrary number of CUE tokens with balanced brackets
// ((), [], and {}). These are the arguments retrieved by the
// [Attribute] methods.
//
// Leading and trailing white space will be stripped from both key and
// value. If there is no value and the key consists of exactly one
// quoted string, it will be unquoted.
type Attribute struct {
	attr internal.Attr
}

// Format implements fmt.Formatter.
func (a Attribute) Format(w fmt.State, verb rune) {
	fmt.Fprintf(w, "@%s(%s)", a.attr.Name, a.attr.Body)
}

var _ fmt.Formatter = &Attribute{}

// Name returns the name of the attribute, for instance, "json" for @json(...).
func (a *Attribute) Name() string {
	return a.attr.Name
}

// Contents reports the full contents of an attribute within parentheses, so
// contents in @attr(contents).
func (a *Attribute) Contents() string {
	return a.attr.Body
}

// NumArgs reports the number of arguments parsed for this attribute.
func (a *Attribute) NumArgs() int {
	return len(a.attr.Fields)
}

// Arg reports the contents of the ith comma-separated argument of a.
//
// If the argument contains an unescaped equals sign, it returns a key-value
// pair. Otherwise it returns the contents in key.
//
// It also unquotes the value argument if it's a string.
func (a *Attribute) Arg(i int) (key, value string) {
	f := a.attr.Fields[i]
	if f.Key() == "" {
		return f.Value(), ""
	}
	return f.Key(), f.Value()
}

// AttributeArg represents an argument in an attribute.
type AttributeArg struct {
	// Key holds the key part of the argument. This will
	// be empty if there is no key part. Note that if the key
	// is quoted, Key will also be quoted.
	Key string

	// Value holds the value part of the of the argument.
	// Other than having surrounding white space trimmed,
	// this will hold the verbatim text of the argument's value:
	// it will not be unquoted if it's a literal string.
	Value string
}

// AsString returns the value part of the argument as a string,
// unquoting it if it's a valid CUE string literal.
func (a AttributeArg) AsString() string {
	return internal.MaybeUnquote(a.Value)
}

// Args returns an iterator over all the arguments from
// position pos onwards.
func (a *Attribute) Args(pos int) iter.Seq[AttributeArg] {
	return func(yield func(AttributeArg) bool) {
		n := a.NumArgs()
		for i := pos; i < n; i++ {
			f := &a.attr.Fields[i]
			if !yield(AttributeArg{
				Key:   f.Key(),
				Value: f.RawValue(),
			}) {
				return
			}
		}
	}
}

// RawArg reports the raw contents of the ith comma-separated argument of a,
// including surrounding spaces.
func (a *Attribute) RawArg(i int) string {
	return a.attr.Fields[i].Text()
}

// Kind reports the type of location within CUE source where the attribute
// was specified.
func (a *Attribute) Kind() AttrKind {
	return AttrKind(a.attr.Kind)
}

// Err returns the error associated with this Attribute or nil if this
// attribute is valid.
func (a *Attribute) Err() error {
	return a.attr.Err
}

// String reports the possibly empty string value at the given position or
// an error the attribute is invalid or if the position does not exist.
func (a *Attribute) String(pos int) (string, error) {
	return a.attr.String(pos)
}

// Int reports the integer at the given position or an error if the attribute is
// invalid, the position does not exist, or the value at the given position is
// not an integer.
func (a *Attribute) Int(pos int) (int64, error) {
	return a.attr.Int(pos)
}

// Flag reports whether an entry with the given name exists at position pos or
// onwards or an error if the attribute is invalid or if the first pos-1 entries
// are not defined.
func (a *Attribute) Flag(pos int, key string) (bool, error) {
	return a.attr.Flag(pos, key)
}

// Lookup searches for an entry of the form key=value from position pos onwards
// and reports the value if found. It reports an error if the attribute is
// invalid or if the first pos-1 entries are not defined.
func (a *Attribute) Lookup(pos int, key string) (val string, found bool, err error) {
	return a.attr.Lookup(pos, key)
}
