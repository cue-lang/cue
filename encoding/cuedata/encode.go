// Copyright 2025 CUE Authors
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

// Package cuedata implements support for encoding and
// decoding data-only CUE. It does not use the CUE evaluator.
//
// The encoding rules follow those of [cuelang.org/go/cue.Context.Encode]
// and [cuelang.org/go/cue.Value.Decode] as closely as possible, with the
// following caveats:
// - encoding and decoding to [cuelang.org/go/cue.Value] is not supported.
package cuedata

import (
	"cmp"
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// Encode converts a Go value to a CUE expression.
//
// Encode traverses the value v recursively. If an encountered value implements
// the [json.Marshaler] interface and is not a nil pointer, Encode calls its
// MarshalJSON method to produce JSON and convert that to CUE instead. If no
// MarshalJSON method is present but the value implements encoding.TextMarshaler
// instead, Encode calls its MarshalText method and encodes the result as a
// string.
//
// Otherwise, Encode uses the following type-dependent default encodings:
//
// Boolean values encode as CUE booleans.
//
// Floating point, integer, and *big.Int and *big.Float values encode as CUE
// numbers.
//
// String values encode as CUE strings coerced to valid UTF-8, replacing
// sequences of invalid bytes with the Unicode replacement rune as per Unicode's
// and W3C's recommendation.
//
// Array and slice values encode as CUE lists, except that []byte encodes as a
// bytes value, and a nil slice encodes as the null.
//
// Struct values encode as CUE structs. Each exported struct field becomes a
// member of the object, using the field name as the object key, unless the
// field is omitted for one of the reasons given below.
//
// The encoding of each struct field can be customized by the format string
// stored under the "json" key in the struct field's tag. The format string
// gives the name of the field, possibly followed by a comma-separated list of
// options. The name may be empty in order to specify options without overriding
// the default field name.
//
// The "omitempty" option specifies that the field should be omitted from the
// encoding if the field has an empty value, defined as false, 0, a nil pointer,
// a nil interface value, and any empty array, slice, map, or string.
//
// See the documentation for Go's json.Marshal for more details on the field
// tags and their meaning.
//
// Anonymous struct fields are usually encoded as if their inner exported
// fields were fields in the outer struct, subject to the usual Go visibility
// rules amended as described in the next paragraph. An anonymous struct field
// with a name given in its JSON tag is treated as having that name, rather than
// being anonymous. An anonymous struct field of interface type is treated the
// same as having that type as its name, rather than being anonymous.
//
// The Go visibility rules for struct fields are amended for when deciding which
// field to encode or decode. If there are multiple fields at the same level,
// and that level is the least nested (and would therefore be the nesting level
// selected by the usual Go rules), the following extra rules apply:
//
// 1) Of those fields, if any are JSON-tagged, only tagged fields are
// considered, even if there are multiple untagged fields that would otherwise
// conflict.
//
// 2) If there is exactly one field (tagged or not according to the first rule),
// that is selected.
//
// 3) Otherwise there are multiple fields, and all are ignored; no error occurs.
//
// Map values encode as CUE structs. The map's key type must either be a string,
// an integer type, or implement encoding.TextMarshaler. The map keys are sorted
// and used as CUE struct field names by applying the following rules, subject
// to the UTF-8 coercion described for string values above:
//
//   - keys of any string type are used directly
//   - encoding.TextMarshalers are marshaled
//   - integer keys are converted to strings
//
// Pointer values encode as the value pointed to. A nil pointer encodes as the
// null CUE value.
//
// Interface values encode as the value contained in the interface. A nil
// interface value encodes as the null CUE value.
//
// Channel, complex, and function values cannot be encoded in CUE. Attempting to
// encode such a value results in the returned value being an error, accessible
// through the Err method.
func Encode(v any) (ast.Expr, error) {
	return encodeValue(reflect.ValueOf(v))
}

func encodeValue(v reflect.Value) (ast.Expr, error) {
	if !v.IsValid() {
		return ast.NewNull(), nil
	}
	switch v := v.Interface().(type) {
	// TODO *big.Rat, *apd.Decimal
	case *big.Int:
		return ast.NewLit(token.INT, v.String()), nil
	case *big.Float:
		return ast.NewLit(token.FLOAT, v.Text('g', -1)), nil
	case json.Marshaler:
		b, err := v.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("cuedata: MarshalJSON: %v", err)
		}
		var x any
		if err := json.Unmarshal(b, &x); err != nil {
			return nil, fmt.Errorf("cuedata: internal json round-trip: %v", err)
		}
		return encodeValue(reflect.ValueOf(x))
	case encoding.TextMarshaler:
		b, err := v.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("cuedata: TextMarshaler: %v", err)
		}
		return ast.NewString(toValidUTF8(string(b))), nil
	}

	// Interfaces and pointers first: nil values must encode as null.
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return ast.NewNull(), nil
		}
		return encodeValue(v.Elem())
	}

	switch v.Kind() {
	case reflect.Bool:
		return ast.NewBool(v.Bool()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return ast.NewLit(token.INT, strconv.FormatInt(v.Int(), 10)), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return ast.NewLit(token.INT, strconv.FormatUint(v.Uint(), 10)), nil

	case reflect.Float32, reflect.Float64:
		return ast.NewLit(token.FLOAT, formatFloat(v.Float())), nil

	case reflect.String:
		return ast.NewString(toValidUTF8(v.String())), nil

	case reflect.Slice:
		// Special-case []byte.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if v.IsNil() {
				return ast.NewNull(), nil
			}
			return ast.NewLit(token.STRING, literal.Bytes.Quote(string(v.Bytes()))), nil
		}
		fallthrough
	case reflect.Array:
		if v.Len() == 0 {
			return &ast.ListLit{Elts: nil}, nil
		}
		els := make([]ast.Expr, v.Len())
		for i := 0; i < v.Len(); i++ {
			e, err := encodeValue(v.Index(i))
			if err != nil {
				return nil, err
			}
			els[i] = e
		}
		return &ast.ListLit{Elts: els}, nil

	case reflect.Map:
		if v.IsNil() {
			return ast.NewNull(), nil
		}
		keys, err := collectMapKeys(v)
		if err != nil {
			return nil, err
		}
		s := &ast.StructLit{
			Elts: make([]ast.Decl, 0, len(keys)),
		}
		for _, k := range keys {
			valExpr, err := encodeValue(k.val)
			if err != nil {
				return nil, err
			}
			f := &ast.Field{
				Value: valExpr,
			}
			if ast.IsValidIdent(k.label) {
				f.Label = ast.NewIdent(k.label)
			} else {
				f.Label = ast.NewString(k.label)
			}
			s.Elts = append(s.Elts, f)
		}
		return s, nil

	case reflect.Struct:
		// This code mimics the behavior of internal/core/convert.goConverter.convertRec
		sl := &ast.StructLit{
			Elts: make([]ast.Decl, 0, v.NumField()),
		}
		t := v.Type()
		for i := range t.NumField() {
			sf := t.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			fieldVal := v.Field(i)
			if isNil(fieldVal) {
				// TODO this seems somewhat questionable.
				continue
			}
			if tag, _ := sf.Tag.Lookup("json"); tag == "-" {
				continue
			}
			if isOmitEmpty(&sf) && fieldVal.IsZero() {
				continue
			}
			sub, err := encodeValue(fieldVal)
			if err != nil {
				// mimic behavior of encoding/json: skip fields of unsupported types
				continue
			}

			// leave errors like we do during normal evaluation or do we
			// want to return the error?
			name := getName(&sf)
			if name == "-" {
				continue
			}
			if sf.Anonymous && name == "" {
				// TODO better handling of embedded structs with clashing names.
				// cue.Context.Encode doesn't do very well here anyway (it just seems to
				// ignore the top level field, which is the wrong thing to do).
				if sub, ok := sub.(*ast.StructLit); ok {
					sl.Elts = append(sl.Elts, sub.Elts...)
				}
				continue
			}
			sl.Elts = append(sl.Elts, &ast.Field{
				Label: ast.NewIdent(name),
				Value: sub,
			})
		}
		return sl, nil

	default:
		return nil, fmt.Errorf("cuedata: unsupported Go kind %s", v.Kind())
	}
}

// isOmitEmpty means that the zero value is interpreted as undefined.
func isOmitEmpty(f *reflect.StructField) bool {
	isOmitEmpty := false
	switch f.Type.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Chan, reflect.Interface, reflect.Slice:
		// Note: it may be confusing to distinguish between an empty slice and
		// a nil slice. However, it is also surprising to not be able to specify
		// a default value for a slice. So for now we will allow it.
		isOmitEmpty = true

	default:
		// TODO: we can also infer omit empty if a type cannot be nil if there
		// is a constraint that unconditionally disallows the zero value.
	}
	tag, ok := f.Tag.Lookup("json")
	if ok {
		isOmitEmpty = false
		for _, f := range strings.Split(tag, ",")[1:] {
			if f == "omitempty" {
				return true
			}
		}
	}
	return isOmitEmpty
}

// TODO: should we allow mapping names in cue tags? This only seems like a good
// idea if we ever want to allow mapping CUE to a different name than JSON.
var tagsWithNames = []string{"json", "yaml", "protobuf"}

func getName(f *reflect.StructField) string {
	name := f.Name
	if f.Anonymous {
		name = ""
	}
	for _, s := range tagsWithNames {
		if tag, ok := f.Tag.Lookup(s); ok {
			if p := strings.IndexByte(tag, ','); p >= 0 {
				tag = tag[:p]
			}
			if tag != "" {
				name = tag
				break
			}
		}
	}
	return name
}

type mapKey struct {
	label string        // label accepted by ast.NewStruct
	val   reflect.Value // original reflect.Value key (for MapIndex)
}

func collectMapKeys(v reflect.Value) ([]mapKey, error) {
	out := make([]mapKey, 0, v.Len())

	for k, v := range v.Seq2() {
		label, err := encodeMapKeyLabel(k)
		if err != nil {
			return nil, err
		}
		out = append(out, mapKey{label, v})
	}
	slices.SortFunc(out, func(k1, k2 mapKey) int {
		return cmp.Compare(k1.label, k2.label)
	})
	return out, nil
}

func encodeMapKeyLabel(k reflect.Value) (string, error) {
	// Follow the rules described in the package docs.
	switch k.Kind() {
	case reflect.String:
		return toValidUTF8(k.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), nil
	}
	return "", fmt.Errorf("cuedata: unsupported map key type %s", k.Type())
}

func labelForString(s string) interface{} {
	s = toValidUTF8(s)
	if ast.IsValidIdent(s) {
		return s // unquoted identifier
	}
	return ast.NewString(s)
}

func labelString(l interface{}) string {
	switch x := l.(type) {
	case string:
		return x
	case *ast.BasicLit:
		return x.Value // includes quotes, good enough for sort order
	default:
		return fmt.Sprintf("%v", x)
	}
}

func isNil(x reflect.Value) bool {
	switch x.Kind() {
	// Only check for supported types; ignore func and chan.
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface:
		return x.IsNil()
	}
	return false
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
