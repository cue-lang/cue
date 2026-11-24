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
	"encoding"
	"encoding/json"
	"math/big"
	"reflect"
	"strconv"
	"sync"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
)

// Decode initializes the value pointed to by x with [Value] v.
// An error is returned if x is nil or not a pointer.
//
// If x implements any of these interfaces, they will be used for decoding
// in the following order of precedence:
//  1. [Unmarshaler]
//  2. [json.Unmarshaler]
//  3. [encoding.TextUnmarshaler]
//
// If x is a struct, this same applies to all of its fields (matching the behavior of [json.Unmarshal])
//
// If x is a struct, Decode will validate the constraints specified in the field tags.
//
// If x contains a [Value], that part of x will be set to the value
// at the corresponding part of v. This allows decoding values
// that aren't entirely concrete into a Go type.
func (v Value) Decode(x any) error {
	var d decoder
	w := reflect.ValueOf(x)
	if w.Kind() != reflect.Pointer || w.IsNil() {
		d.addErr(errors.Newf(v.Pos(), "cannot decode into unsettable value"))
		return d.errs
	}
	d.decode(w.Elem(), v, false)
	return d.errs
}

// TODO(mvdan): move decoder to internal/core/convert as ToGoValue

type decoder struct {
	errs errors.Error
}

func (d *decoder) addErr(err error) {
	if err != nil {
		d.errs = errors.Append(d.errs, errors.Promote(err, ""))
	}
}

func incompleteError(v Value) errors.Error {
	return &valueError{
		v: v,
		err: &adt.Bottom{
			Code: adt.IncompleteError,
			Err:  errors.Newf(v.Pos(), "cannot convert non-concrete value %v", v),
		},
	}
}

func (d *decoder) clear(x reflect.Value) {
	if x.CanSet() {
		x.SetZero()
	}
}

var valueType = reflect.TypeFor[Value]()

type valueUnmarshaler interface {
	unmarshalValue(v Value) error
}

type cueUnmarshaler struct{ Unmarshaler }

func (u cueUnmarshaler) unmarshalValue(v Value) error {
	return u.UnmarshalCUE(v)
}

type jsonUnmarshaler struct{ json.Unmarshaler }

func (u jsonUnmarshaler) unmarshalValue(v Value) error {
	b, err := v.MarshalJSON()
	if err != nil {
		return err
	}
	return u.UnmarshalJSON(b)
}

type textUnmarshaler struct{ encoding.TextUnmarshaler }

func (u textUnmarshaler) unmarshalValue(v Value) error {
	switch x := u.TextUnmarshaler.(type) {
	case *big.Float:
		f, err := v.Float(nil)
		if err != nil {
			return errors.Wrapf(err, v.Pos(), "Decode")
		}
		*x = *f
		return nil
	default:
		b, err := v.Bytes()
		if err != nil {
			return errors.Wrapf(err, v.Pos(), "Decode")
		}
		return u.UnmarshalText(b)
	}
}

func (d *decoder) decode(x reflect.Value, v Value, isPtr bool) {
	if !x.IsValid() {
		d.addErr(errors.Newf(v.Pos(), "cannot decode into invalid value"))
		return
	}

	v, _ = v.Default()
	if v.v == nil {
		d.clear(x)
		return
	}

	if err := v.Err(); err != nil && !IsIncomplete(err) {
		d.addErr(err)
		return
	}
	if x.Type() == valueType {
		x.Set(reflect.ValueOf(v))
		return
	}

	unmarshaler, x := indirect(x, v.IsNull())
	if unmarshaler != nil {
		d.addErr(unmarshaler.unmarshalValue(v))
		return
	}

	kind := x.Kind()
	switch kind {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		// nullable types
		if v.IsNull() || !v.IsConcrete() {
			d.clear(x)
			return
		}

	default:
		// TODO: allow incomplete values.
		if !v.IsConcrete() {
			d.addErr(incompleteError(v))
			return
		}
	}

	if kind == reflect.Interface {
		value := d.interfaceValue(v)
		x.Set(reflect.ValueOf(value))
		return
	}

	switch kind {
	case reflect.Pointer:
		d.decode(x.Elem(), v, true)

	case reflect.Bool:
		b, err := v.Bool()
		d.addErr(err)
		x.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := v.Int64()
		d.addErr(err)
		if x.OverflowInt(i) {
			d.addErr(errors.Newf(v.Pos(), "integer %d overflows %s", i, kind))
			break
		}
		x.SetInt(i)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		i, err := v.Uint64()
		d.addErr(err)
		if x.OverflowUint(i) {
			d.addErr(errors.Newf(v.Pos(), "integer %d overflows %s", i, kind))
			break
		}
		x.SetUint(i)

	case reflect.Float32, reflect.Float64:
		f, err := v.Float64()
		d.addErr(err)
		if x.OverflowFloat(f) {
			d.addErr(errors.Newf(v.Pos(), "float %g overflows %s", f, kind))
			break
		}
		x.SetFloat(f)

	case reflect.String:
		s, err := v.String()
		d.addErr(err)
		x.SetString(s)

	case reflect.Array:
		d.clear(x)

		t := x.Type()
		n := x.Len()

		if t.Elem().Kind() == reflect.Uint8 && v.Kind() == BytesKind {
			b, err := v.Bytes()
			d.addErr(err)
			for i, c := range b {
				if i >= n {
					break
				}
				x.Index(i).SetUint(uint64(c))
			}
			break
		}

		var a []Value
		list, err := v.List()
		d.addErr(err)
		for list.Next() {
			a = append(a, list.Value())
		}

		for i, v := range a {
			if i >= n {
				break
			}
			d.decode(x.Index(i), v, false)
		}

	case reflect.Slice:
		t := x.Type()
		if t.Elem().Kind() == reflect.Uint8 && v.Kind() == BytesKind {
			b, err := v.Bytes()
			d.addErr(err)
			x.SetBytes(b)
			break
		}

		var a []Value
		list, err := v.List()
		d.addErr(err)
		for list.Next() {
			a = append(a, list.Value())
		}

		switch cap := x.Cap(); {
		case cap == 0, // force a non-nil list
			cap < len(a):
			x.Set(reflect.MakeSlice(t, len(a), len(a)))

		default:
			x.SetLen(len(a))
		}

		for i, v := range a {
			d.decode(x.Index(i), v, false)
		}

	case reflect.Struct:
		d.convertStruct(x, v)

	case reflect.Map:
		d.convertMap(x, v)

	default:
		d.clear(x)
	}
}

func (d *decoder) interfaceValue(v Value) (x any) {
	var err error
	v, _ = v.Default()
	switch v.Kind() {
	case NullKind:
		return nil

	case BoolKind:
		x, err = v.Bool()

	case IntKind:
		if i, err := v.Int64(); err == nil {
			return i
		}
		x, err = v.Int(nil)

	case FloatKind:
		if f, err := v.Float64(); err == nil {
			return f
		} // or big int or
		x, err = v.Float(nil)

	case StringKind:
		x, err = v.String()

	case BytesKind:
		x, err = v.Bytes()

	case ListKind:
		var a []any
		list, err := v.List()
		d.addErr(err)
		for list.Next() {
			a = append(a, d.interfaceValue(list.Value()))
		}
		if a == nil {
			a = []any{}
		}
		x = a

	case StructKind:
		m := map[string]any{}
		iter, err := v.Fields()
		d.addErr(err)
		for iter.Next() {
			m[iter.Selector().Unquoted()] = d.interfaceValue(iter.Value())
		}
		x = m

	default:
		err = incompleteError(v)
	}

	d.addErr(err)
	return x
}

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

// convertMap keeps an existing map and overwrites any entry found in v,
// keeping other preexisting entries.
func (d *decoder) convertMap(x reflect.Value, v Value) {
	// Delete existing elements
	t := x.Type()

	// Map key must either have string kind, have an integer kind,
	// or be an [encoding.TextUnmarshaler].
	switch t.Key().Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		if !reflect.PointerTo(t.Key()).Implements(textUnmarshalerType) {
			d.addErr(errors.Newf(v.Pos(), "unsupported key type %v", t.Key()))
			return
		}
	}

	if x.IsNil() {
		x.Set(reflect.MakeMap(t))
	}

	var mapElem reflect.Value

	iter, err := v.Fields()
	d.addErr(err)
	for iter.Next() {
		key := iter.Selector().Unquoted()

		var kv reflect.Value
		kt := t.Key()
		if reflect.PointerTo(kt).Implements(textUnmarshalerType) {
			kv = reflect.New(kt)
			u, _ := reflect.TypeAssert[encoding.TextUnmarshaler](kv)
			err := u.UnmarshalText([]byte(key))
			d.addErr(err)
			kv = kv.Elem()
		} else {
			switch kt.Kind() {
			case reflect.String:
				kv = reflect.ValueOf(key).Convert(kt)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				n, err := strconv.ParseInt(key, 10, 64)
				d.addErr(err)
				if kt.OverflowInt(n) {
					d.addErr(errors.Newf(v.Pos(), "key integer %d overflows %s", n, kt))
					break
				}
				kv = reflect.ValueOf(n).Convert(kt)

			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				n, err := strconv.ParseUint(key, 10, 64)
				d.addErr(err)
				if kt.OverflowUint(n) {
					d.addErr(errors.Newf(v.Pos(), "key integer %d overflows %s", n, kt))
					break
				}
				kv = reflect.ValueOf(n).Convert(kt)

			default:
				panic("json: Unexpected key type") // should never occur
			}
		}

		if !mapElem.IsValid() {
			mapElem = reflect.New(t.Elem()).Elem()
		} else {
			mapElem.SetZero()
		}
		d.decode(mapElem, iter.Value(), false)

		if kv.IsValid() {
			x.SetMapIndex(kv, mapElem)
		}
	}
}

func (d *decoder) convertStruct(x reflect.Value, v Value) {
	t := x.Type()
	fields := cachedTypeFields(t)

	iter, err := v.Fields()
	d.addErr(err)
	for iter.Next() {
		key := iter.Selector().Unquoted()
		index, ok := fields.byName[key]
		if !ok {
			// Fall back to a case-insensitive match.
			index, ok = fields.byFoldedName[string(foldName(key))]
		}
		if !ok {
			continue
		}

		// Figure out field corresponding to key.
		subv := x
		for _, i := range index {
			if subv.Kind() == reflect.Pointer {
				if subv.IsNil() {
					// If a struct embeds a pointer to an unexported type,
					// it is not possible to set a newly allocated value
					// since the field is unexported.
					//
					// See https://golang.org/issue/21357
					if !subv.CanSet() {
						d.addErr(errors.Newf(v.Pos(),
							"cannot set embedded pointer to unexported struct: %v",
							subv.Type().Elem()))
						subv = reflect.Value{}
						break
					}
					subv.Set(reflect.New(subv.Type().Elem()))
				}
				subv = subv.Elem()
			}
			subv = subv.Field(i)
		}

		// TODO: make this an option
		//  else if d.disallowUnknownFields {
		// 	d.saveError(fmt.Errorf("json: unknown field %q", key))
		// }

		d.decode(subv, iter.Value(), false)
	}
}

// structFields maps the names of a Go struct type's fields to their indices,
// and their folded names too, except for fields with "case:strict".
type structFields struct {
	byName       map[string][]int
	byFoldedName map[string][]int
}

// typeFields returns the fields of the Go struct type t to decode into.
func typeFields(t reflect.Type) structFields {
	fields, _ := convert.GoFields(t, convert.JSONTagName)
	byName := make(map[string][]int, len(fields))
	byFoldedName := make(map[string][]int, len(fields))
	for _, f := range fields {
		byName[f.Name] = f.Index
		if f.JSON.CaseStrict {
			continue
		}
		// The first field in index order wins.
		folded := string(foldName(f.Name))
		if _, ok := byFoldedName[folded]; !ok {
			byFoldedName[folded] = f.Index
		}
	}
	return structFields{byName, byFoldedName}
}

var fieldCache sync.Map // map[reflect.Type]structFields

// cachedTypeFields is like [typeFields] but uses a cache to avoid repeated work.
func cachedTypeFields(t reflect.Type) structFields {
	if f, ok := fieldCache.Load(t); ok {
		return f.(structFields)
	}
	f, _ := fieldCache.LoadOrStore(t, typeFields(t))
	return f.(structFields)
}

// foldName returns a folded form of name, such that two names fold to the
// same bytes exactly when they are equal under [strings.EqualFold].
// It is kept small enough to be inlined, so that arr does not escape.
func foldName(name string) []byte {
	var arr [32]byte // large enough for most names
	return appendFoldedName(arr[:0], name)
}

func appendFoldedName(out []byte, name string) []byte {
	for _, r := range name {
		out = utf8.AppendRune(out, foldRune(r))
	}
	return out
}

// foldRune returns the smallest rune in the same fold set as r.
func foldRune(r rune) rune {
	for {
		r2 := unicode.SimpleFold(r)
		if r2 <= r {
			return r2
		}
		r = r2
	}
}

// indirect walks down v allocating pointers as needed,
// until it gets to a non-pointer.
// If it encounters a [valueUnmarshaler], indirect stops and returns that.
// If decodingNull is true, indirect stops at the first settable pointer so it
// can be set to nil.
func indirect(v reflect.Value, decodingNull bool) (valueUnmarshaler, reflect.Value) {
	// Issue #24153 indicates that it is generally not a guaranteed property
	// that you may round-trip a reflect.Value by calling Value.Addr().Elem()
	// and expect the value to still be settable for values derived from
	// unexported embedded struct fields.
	//
	// The logic below effectively does this when it first addresses the value
	// (to satisfy possible pointer methods) and continues to dereference
	// subsequent pointers as necessary.
	//
	// After the first round-trip, we set v back to the original value to
	// preserve the original RW flags contained in reflect.Value.
	v0 := v
	haveAddr := false

	// If v is a named type and is addressable,
	// start with its address, so that if the type has pointer methods,
	// we find them.
	if v.Kind() != reflect.Pointer && v.Type().Name() != "" && v.CanAddr() {
		haveAddr = true
		v = v.Addr()
	}
	for {
		// Load value from interface, but only if the result will be
		// usefully addressable.
		if v.Kind() == reflect.Interface && !v.IsNil() {
			e := v.Elem()
			if e.Kind() == reflect.Pointer && !e.IsNil() && (!decodingNull || e.Elem().Kind() == reflect.Pointer) {
				haveAddr = false
				v = e
				continue
			}
		}

		if v.Kind() != reflect.Pointer {
			break
		}

		if decodingNull && v.CanSet() {
			break
		}

		// Prevent infinite loop if v is an interface pointing to its own address:
		//     var v interface{}
		//     v = &v
		if v.Elem().Kind() == reflect.Interface && v.Elem().Elem() == v {
			v = v.Elem()
			break
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().NumMethod() > 0 && v.CanInterface() {
			switch u := v.Interface().(type) {
			case Unmarshaler:
				return cueUnmarshaler{u}, v
			case json.Unmarshaler:
				return jsonUnmarshaler{u}, v
			case encoding.TextUnmarshaler:
				if !decodingNull {
					return textUnmarshaler{u}, v
				}
			}
		}

		if haveAddr {
			v = v0 // restore original value after round-trip Value.Addr().Elem()
			haveAddr = false
		} else {
			v = v.Elem()
		}
	}
	return nil, v
}
