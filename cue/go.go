// Copyright 2019 CUE Authors
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
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"sync"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"github.com/cockroachdb/apd"
)

// This file contains functionality for converting Go to CUE.
//
// The code in this file is a prototype implementation and is far from
// optimized.

func init() {
	internal.FromGoValue = func(instance, x interface{}) interface{} {
		return convertValue(instance.(*Instance), x)
	}

	internal.FromGoType = func(instance, x interface{}) interface{} {
		return convertType(instance.(*Instance), x)
	}
}

func convertValue(inst *Instance, x interface{}) Value {
	ctx := inst.index.newContext()
	v := convert(ctx, baseValue{}, x)
	return newValueRoot(ctx, v)
}

func convertType(inst *Instance, x interface{}) Value {
	ctx := inst.index.newContext()
	v := convertGoType(inst, reflect.TypeOf(x))
	return newValueRoot(ctx, v)

}

// parseTag parses a CUE expression from a cue tag.
func parseTag(ctx *context, obj *structLit, field label, tag string) value {
	if p := strings.Index(tag, ","); p >= 0 {
		tag = tag[:p]
	}
	if tag == "" {
		return &top{}
	}
	expr, err := parser.ParseExpr(ctx.index.fset, "<field:>", tag)
	if err != nil {
		field := ctx.labelStr(field)
		return ctx.mkErr(baseValue{}, "invalid tag %q for field %q: %v", tag, field, err)
	}
	v := newVisitor(ctx.index, nil, nil, obj)
	return v.walk(expr)
}

// TODO: should we allow mapping names in cue tags? This only seems like a good
// idea if we ever want to allow mapping CUE to a different name than JSON.
var tagsWithNames = []string{"json", "yaml", "protobuf"}

func getName(f *reflect.StructField) string {
	name := f.Name
	for _, s := range tagsWithNames {
		if tag, ok := f.Tag.Lookup(s); ok {
			if p := strings.Index(tag, ","); p >= 0 {
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

// isOptional indicates whether a field should be marked as optional.
func isOptional(f *reflect.StructField) bool {
	isOptional := false
	switch f.Type.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Chan, reflect.Interface, reflect.Slice:
		// Note: it may be confusing to distinguish between an empty slice and
		// a nil slice. However, it is also surprizing to not be able to specify
		// a default value for a slice. So for now we will allow it.
		isOptional = true
	}
	if tag, ok := f.Tag.Lookup("cue"); ok {
		// TODO: only if first field is not empty.
		isOptional = false
		for _, f := range strings.Split(tag, ",")[1:] {
			switch f {
			case "opt":
				isOptional = true
			case "req":
				return false
			}
		}
	} else if tag, ok = f.Tag.Lookup("json"); ok {
		isOptional = false
		for _, f := range strings.Split(tag, ",")[1:] {
			if f == "omitempty" {
				return true
			}
		}
	}
	return isOptional
}

// isOmitEmpty means that the zero value is interpreted as undefined.
func isOmitEmpty(f *reflect.StructField) bool {
	isOmitEmpty := false
	switch f.Type.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Chan, reflect.Interface, reflect.Slice:
		// Note: it may be confusing to distinguish between an empty slice and
		// a nil slice. However, it is also surprizing to not be able to specify
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

// parseJSON parses JSON into a CUE value. b must be valid JSON.
func parseJSON(ctx *context, b []byte) evaluated {
	expr, err := parser.ParseExpr(ctx.index.fset, "json", b)
	if err != nil {
		panic(err) // cannot happen
	}
	v := newVisitor(ctx.index, nil, nil, nil)
	return v.walk(expr).evalPartial(ctx)
}

func isZero(v reflect.Value) bool {
	x := v.Interface()
	if x == nil {
		return true
	}
	switch k := v.Kind(); k {
	case reflect.Struct, reflect.Array:
		// we never allow optional values for these types.
		return false

	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Slice:
		// Note that for maps we preserve the distinction between a nil map and
		// an empty map.
		return v.IsNil()

	case reflect.String:
		return v.Len() == 0

	default:
		return x == reflect.Zero(v.Type()).Interface()
	}
}

func convert(ctx *context, src source, x interface{}) evaluated {
	switch v := x.(type) {
	case nil:
		// Interpret a nil pointer as an undefined value that is only
		// null by default, but may still be set: *null | _.
		return makeNullable(&top{src.base()}).(evaluated)

	case *big.Int:
		n := newNum(src, intKind)
		n.v.Coeff.Set(v)
		if v.Sign() < 0 {
			n.v.Coeff.Neg(&n.v.Coeff)
			n.v.Negative = true
		}
		return n

	case *big.Rat:
		// should we represent this as a binary operation?
		n := newNum(src, numKind)
		ctx.Quo(&n.v, apd.NewWithBigInt(v.Num(), 0), apd.NewWithBigInt(v.Denom(), 0))
		if !v.IsInt() {
			n.k = floatKind
		}
		return n

	case *big.Float:
		n := newNum(src, floatKind)
		n.v.SetString(v.String())
		return n

	case *apd.Decimal:
		n := newNum(src, floatKind|intKind)
		n.v.Set(v)
		if !n.isInt(ctx) {
			n.k = floatKind
		}
		return n

	case json.Marshaler:
		b, err := v.MarshalJSON()
		if err != nil {
			return ctx.mkErr(src, err)
		}

		return parseJSON(ctx, b)

	case encoding.TextMarshaler:
		b, err := v.MarshalText()
		if err != nil {
			return ctx.mkErr(src, err)
		}
		b, err = json.Marshal(string(b))
		if err != nil {
			return ctx.mkErr(src, err)
		}
		return parseJSON(ctx, b)

	case error:
		return ctx.mkErr(src, v.Error())
	case bool:
		return &boolLit{src.base(), v}
	case string:
		return &stringLit{src.base(), v}
	case []byte:
		return &bytesLit{src.base(), v}
	case int:
		return toInt(ctx, src, int64(v))
	case int8:
		return toInt(ctx, src, int64(v))
	case int16:
		return toInt(ctx, src, int64(v))
	case int32:
		return toInt(ctx, src, int64(v))
	case int64:
		return toInt(ctx, src, int64(v))
	case uint:
		return toUint(ctx, src, uint64(v))
	case uint8:
		return toUint(ctx, src, uint64(v))
	case uint16:
		return toUint(ctx, src, uint64(v))
	case uint32:
		return toUint(ctx, src, uint64(v))
	case uint64:
		return toUint(ctx, src, uint64(v))
	case float64:
		r := newNum(src, floatKind)
		r.v.SetString(fmt.Sprintf("%g", v))
		return r

	case reflect.Value:
		if v.CanInterface() {
			return convert(ctx, src, v.Interface())
		}

	default:
		value := reflect.ValueOf(v)
		switch value.Kind() {
		case reflect.Ptr:
			if value.IsNil() {
				// Interpret a nil pointer as an undefined value that is only
				// null by default, but may still be set: *null | _.
				return makeNullable(&top{src.base()}).(evaluated)
			}
			return convert(ctx, src, value.Elem().Interface())

		case reflect.Struct:
			obj := newStruct(src)
			t := value.Type()
			for i := 0; i < value.NumField(); i++ {
				t := t.Field(i)
				if t.PkgPath != "" {
					continue
				}
				val := value.Field(i)
				if isOmitEmpty(&t) && isZero(val) {
					continue
				}
				sub := convert(ctx, src, val.Interface())
				// leave errors like we do during normal evaluation or do we
				// want to return the error?
				name := getName(&t)
				if name == "-" {
					continue
				}
				f := ctx.strLabel(name)
				obj.arcs = append(obj.arcs, arc{feature: f, v: sub})
			}
			sort.Sort(obj)
			return obj

		case reflect.Map:
			obj := newStruct(src)
			t := value.Type()
			if t.Key().Kind() != reflect.String {
				return ctx.mkErr(src, "builtin map key not a string, but unsupported type %s", t.Key().String())
			}
			keys := []string{}
			for _, k := range value.MapKeys() {
				keys = append(keys, k.String())
			}
			sort.Strings(keys)
			for _, k := range keys {
				sub := convert(ctx, src, value.MapIndex(reflect.ValueOf(k)).Interface())
				// leave errors like we do during normal evaluation or do we
				// want to return the error?
				f := ctx.strLabel(k)
				obj.arcs = append(obj.arcs, arc{feature: f, v: sub})
			}
			sort.Sort(obj)
			return obj

		case reflect.Slice, reflect.Array:
			list := &list{baseValue: src.base()}
			arcs := []arc{}
			for i := 0; i < value.Len(); i++ {
				x := convert(ctx, src, value.Index(i).Interface())
				if isBottom(x) {
					return x
				}
				arcs = append(arcs, arc{feature: label(len(arcs)), v: x})
			}
			list.elem = &structLit{baseValue: list.baseValue, arcs: arcs}
			list.initLit()
			// There is no need to set the type of the list, as the list will
			// be of fixed size and all elements will already have a defined
			// value.
			return list
		}
	}
	return ctx.mkErr(src, "builtin returned unsupported type %T", x)
}

func toInt(ctx *context, src source, x int64) evaluated {
	n := newNum(src, intKind)
	n.v.SetInt64(x)
	return n
}

func toUint(ctx *context, src source, x uint64) evaluated {
	n := newNum(src, floatKind)
	n.v.Coeff.SetUint64(x)
	return n
}

var (
	typeCache sync.Map // map[reflect.Type]evaluated
	mutex     sync.Mutex
)

func convertGoType(inst *Instance, t reflect.Type) value {
	ctx := inst.newContext()
	// TODO: this can be much more efficient.
	mutex.Lock()
	defer mutex.Unlock()
	return goTypeToValue(ctx, true, t)
}

var (
	jsonMarshaler = reflect.TypeOf(new(json.Marshaler)).Elem()
	textMarshaler = reflect.TypeOf(new(encoding.TextMarshaler)).Elem()
	topSentinel   = &top{}
)

// goTypeToValue converts a Go Type to a value.
//
// TODO: if this value will always be unified with a concrete type in Go, then
// many of the fields may be omitted.
func goTypeToValue(ctx *context, allowNullDefault bool, t reflect.Type) (e value) {
	if e, ok := typeCache.Load(t); ok {
		return e.(value)
	}

	// Even if this is for types that we know cast to a certain type, it can't
	// hurt to return top, as in these cases the concrete values will be
	// strict instances and there cannot be any tags that further constrain
	// the values.
	if t.Implements(jsonMarshaler) || t.Implements(textMarshaler) {
		return topSentinel
	}

	switch k := t.Kind(); k {
	case reflect.Ptr:
		elem := t.Elem()
		for elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		e = goTypeToValue(ctx, false, elem)
		if allowNullDefault {
			e = wrapOrNull(e)
		}

	case reflect.Interface:
		switch t.Name() {
		case "error":
			// This is really null | _|_. There is no error if the error is null.
			e = &nullLit{} // null
		default:
			e = topSentinel // `_`
		}

	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e = predefinedRanges[t.Kind().String()]

	case reflect.Uint, reflect.Uintptr:
		e = predefinedRanges["uint64"]

	case reflect.Int:
		e = predefinedRanges["int64"]

	case reflect.String:
		e = &basicType{k: stringKind}

	case reflect.Bool:
		e = &basicType{k: boolKind}

	case reflect.Float32, reflect.Float64:
		e = &basicType{k: floatKind}

	case reflect.Struct:
		// Some of these values have MarshalJSON methods, but that may not be
		// the case for older Go versions.
		name := fmt.Sprint(t)
		switch name {
		case "big.Int":
			e = &basicType{k: intKind}
			goto store
		case "big.Rat", "big.Float", "apd.Decimal":
			e = &basicType{k: floatKind}
			goto store
		case "time.Time":
			// We let the concrete value decide.
			e = topSentinel
			goto store
		}

		// First iterate to create struct, then iterate another time to
		// resolve field tags to allow field tags to refer to the struct fields.
		tags := map[label]string{}
		obj := newStruct(baseValue{})
		typeCache.Store(t, obj)

		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			_, ok := f.Tag.Lookup("cue")
			elem := goTypeToValue(ctx, !ok, f.Type)

			// leave errors like we do during normal evaluation or do we
			// want to return the error?
			name := getName(&f)
			if name == "-" {
				continue
			}
			l := ctx.strLabel(name)
			obj.arcs = append(obj.arcs, arc{
				feature: l,
				// The GO JSON decoder always allows a value to be undefined.
				optional: isOptional(&f),
				v:        elem,
			})

			if tag, ok := f.Tag.Lookup("cue"); ok {
				tags[l] = tag
			}
		}
		sort.Sort(obj)

		for label, tag := range tags {
			v := parseTag(ctx, obj, label, tag)
			for i, a := range obj.arcs {
				if a.feature == label {
					// Instead of unifying with the existing type, we substitute
					// with the constraints from the tags. The type constraints
					// will be implied when unified with a concrete value.
					obj.arcs[i].v = mkBin(ctx, token.NoPos, opUnify, a.v, v)
				}
			}
		}

		return obj

	case reflect.Array, reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			e = &basicType{k: bytesKind}
		} else {
			elem := goTypeToValue(ctx, allowNullDefault, t.Elem())

			var ln value = &top{}
			if t.Kind() == reflect.Array {
				ln = toInt(ctx, baseValue{}, int64(t.Len()))
			}
			e = &list{elem: &structLit{}, typ: elem, len: ln}
		}
		if k == reflect.Slice {
			e = wrapOrNull(e)
		}

	case reflect.Map:
		if key := t.Key(); key.Kind() != reflect.String {
			// What does the JSON library do here?
			e = ctx.mkErr(baseValue{}, "type %v not supported as key type", key)
			break
		}

		obj := newStruct(baseValue{})
		sig := &params{}
		sig.add(ctx.label("_", true), &basicType{k: stringKind})
		v := goTypeToValue(ctx, allowNullDefault, t.Elem())
		obj.template = &lambdaExpr{params: sig, value: v}

		e = wrapOrNull(obj)
	}

store:
	// TODO: store error if not nil?
	if e != nil {
		typeCache.Store(t, e)
	}
	return e
}

func wrapOrNull(e value) value {
	if e.kind().isAnyOf(nullKind) {
		return e
	}
	return makeNullable(e)
}

const nullIsDefault = true

func makeNullable(e value) value {
	return &disjunction{
		baseValue: baseValue{e},
		values: []dValue{
			{val: &nullLit{}, marked: nullIsDefault},
			{val: e}},
		hasDefaults: nullIsDefault,
	}
}
