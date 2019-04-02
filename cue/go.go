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
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"

	"cuelang.org/go/cue/ast"
	"github.com/cockroachdb/apd"
)

// This file contains functionality for converting Go to CUE.

func convert(ctx *context, src source, x interface{}) evaluated {
	switch v := x.(type) {
	case evaluated:
		return v
	case nil:
		return &nullLit{src.base()}
	case ast.Expr:
		x := newVisitorCtx(ctx, nil, nil, nil)
		return ctx.manifest(x.walk(v))
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
	case *big.Int:
		n := newNum(src, intKind)
		n.v.Coeff.Set(v)
		if v.Sign() < 0 {
			n.v.Coeff.Neg(&n.v.Coeff)
			n.v.Negative = true
		}
		return n
	case *big.Rat:
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
	case reflect.Value:
		if v.CanInterface() {
			return convert(ctx, src, v.Interface())
		}

	default:
		value := reflect.ValueOf(v)
		switch value.Kind() {
		case reflect.Ptr:
			if value.IsNil() {
				return &nullLit{src.base()}
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
				sub := convert(ctx, src, value.Field(i).Interface())
				// leave errors like we do during normal evaluation or do we
				// want to return the error?
				name := t.Name
				for _, s := range []string{"cue", "json", "protobuf"} {
					if tag, ok := t.Tag.Lookup(s); ok {
						if p := strings.Index(tag, ","); p >= 0 {
							tag = tag[:p]
						}
						if tag != "" {
							name = tag
							break
						}
					}
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
			for i := 0; i < value.Len(); i++ {
				x := convert(ctx, src, value.Index(i).Interface())
				if isBottom(x) {
					return x
				}
				list.a = append(list.a, x)
			}
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
