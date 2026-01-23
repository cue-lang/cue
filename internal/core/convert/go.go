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

// Package convert allows converting to and from Go values and Types.
package convert

import (
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/types"
)

// This file contains functionality for converting Go to CUE.
//
// The code in this file is a prototype implementation and is far from
// optimized.

// TODO(mvdan): get rid of the uses of %T below; have the recursive methods return *Bottom
// TODO(mvdan): swap order of parameters in the recursive methods to match the top-level API order

// FromGoValue converts a Go value to an internal CUE value.
// The returned CUE value is finalized and concrete.
func FromGoValue(ctx *adt.OpContext, x any, nilIsTop bool) adt.Value {
	val := reflect.ValueOf(x)
	v := fromGoValue(ctx, nilIsTop, val)
	if v == nil {
		return ctx.AddErrf("unsupported Go type (%T)", x)
	}
	// TODO: return Value
	return v
}

// FromGoType converts a Go type to an internal CUE expression.
func FromGoType(ctx *adt.OpContext, x any) (adt.Expr, errors.Error) {
	// TODO: if this value will always be unified with a concrete type in Go,
	// then many of the fields may be omitted.
	// TODO: this can be much more efficient.
	// TODO: synchronize
	typ := reflect.TypeOf(x)
	if _, t, ok := ctx.LoadType(typ); ok {
		return t, nil
	}
	_, expr := fromGoType(ctx, true, typ)
	if expr == nil {
		expr = ctx.AddErrf("unsupported Go type (%v)", typ)
	}
	if err := ctx.Err(); err != nil {
		// TODO: return an error as the expr itself, like [FromGoValue]?
		return expr, err.Err
	}
	return expr, nil
}

func compileExpr(ctx *adt.OpContext, expr ast.Expr) adt.Value {
	c, err := compile.Expr(nil, ctx, pkgID(), expr)
	if err != nil {
		return &adt.Bottom{Err: errors.Promote(err, "compile")}
	}
	return adt.Resolve(ctx, c)
}

// parseTag parses a CUE expression from a cue tag.
func parseTag(ctx *adt.OpContext, field, tag string) ast.Expr {
	tag, _ = splitTag(tag)
	if tag == "" {
		return topSentinel
	}
	expr, err := parser.ParseExpr("<field:>", tag)
	if err != nil {
		err := errors.Promote(err, "parser")
		ctx.AddErr(errors.Wrapf(err, ctx.Pos(), "invalid tag %q for field %q", tag, field))
		return &ast.BadExpr{}
	}
	return expr
}

// splitTag splits a cue tag into cue and options.
func splitTag(tag string) (cue string, options string) {
	q := strings.LastIndexByte(tag, '"')
	if c := strings.IndexByte(tag[q+1:], ','); c >= 0 {
		return tag[:q+1+c], tag[q+1+c+1:]
	}
	return tag, ""
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

// isOptional indicates whether a field should be marked as optional.
func isOptional(f *reflect.StructField) bool {
	isOptional := false
	switch f.Type.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Interface, reflect.Slice:
		// Note: it may be confusing to distinguish between an empty slice and
		// a nil slice. However, it is also surprising to not be able to specify
		// a default value for a slice. So for now we will allow it.
		isOptional = true
	}
	if tag, ok := f.Tag.Lookup("cue"); ok {
		// TODO: only if first field is not empty.
		_, opt := splitTag(tag)
		isOptional = false
		for f := range strings.SplitSeq(opt, ",") {
			switch f {
			case "opt":
				isOptional = true
			case "req":
				return false
			}
		}
	} else if tag, ok = f.Tag.Lookup("json"); ok {
		isOptional = false
		if slices.Contains(strings.Split(tag, ",")[1:], "omitempty") {
			return true
		}
	}
	return isOptional
}

// isOmitEmpty means that the zero value is interpreted as undefined.
func isOmitEmpty(f *reflect.StructField) bool {
	isOmitEmpty := false
	switch f.Type.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Interface, reflect.Slice:
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
		if slices.Contains(strings.Split(tag, ",")[1:], "omitempty") {
			return true
		}
	}
	return isOmitEmpty
}

func isNil(x reflect.Value) bool {
	switch x.Kind() { // Only check for supported types; ignore func and chan.
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return x.IsNil()
	}
	return false
}

func fromGoValue(ctx *adt.OpContext, nilIsTop bool, val reflect.Value) (result adt.Value) {
	src := ctx.Source()
	if !val.IsValid() { // untyped nil, or dereferencing a nil pointer/interface
		if nilIsTop {
			ident, _ := src.(*ast.Ident)
			return &adt.Top{Src: ident}
		}
		return &adt.Null{Src: src}
	}
	env := ctx.Env(0)
	typ := val.Type()
	switch typ {
	case astFile:
		v, _ := val.Interface().(*ast.File) // TODO(go1.25): use reflect.TypeAssert
		x, err := compile.Files(nil, ctx, pkgID(), v)
		if err != nil {
			return &adt.Bottom{Err: errors.Promote(err, "compile")}
		}
		if _, n := x.SingleConjunct(); n != 1 {
			panic("unexpected length")
		}
		return x

	case bigInt:
		v, _ := val.Interface().(*big.Int) // TODO(go1.25): use reflect.TypeAssert
		return &adt.Num{
			Src: src,
			K:   adt.IntKind,
			X:   fromGoBigInt(v),
		}

	case bigRat:
		v, _ := val.Interface().(*big.Rat) // TODO(go1.25): use reflect.TypeAssert
		// should we represent this as a binary operation?
		n := &adt.Num{Src: src, K: adt.IntKind}
		num := fromGoBigInt(v.Num())
		denom := fromGoBigInt(v.Denom())
		if _, err := internal.BaseContext.Quo(&n.X, &num, &denom); err != nil {
			return ctx.AddErrf("could not convert *big.Rat: %v", err)
		}
		if !v.IsInt() {
			n.K = adt.FloatKind
		}
		return n

	case bigFloat:
		v, _ := val.Interface().(*big.Float) // TODO(go1.25): use reflect.TypeAssert
		n := &adt.Num{Src: src, K: adt.FloatKind}
		// NOTE: apd.Decimal has an API to set from a big.Int, but not from a big.Float.
		if _, _, err := n.X.SetString(v.String()); err != nil {
			return ctx.AddErr(errors.Promote(err, "invalid float"))
		}
		return n

	case apdDecimal:
		v, _ := val.Interface().(*apd.Decimal) // TODO(go1.25): use reflect.TypeAssert
		// TODO: should we allow an "int" bit to be set here?
		// It is a bit tricky, as we would also need to pass down the result of rounding.
		// So more likely an API must return explicitly whether a value is a float or an int after all.
		// The code to autodetect whether something is an integer can be done with this:
		kind := adt.FloatKind
		var d apd.Decimal
		res, _ := internal.BaseContext.RoundToIntegralExact(&d, v)
		if !res.Inexact() {
			kind = adt.IntKind
			v = &d
		}
		n := &adt.Num{Src: src, K: kind}
		n.X = *v
		return n
	}

	if _, ok := implements(typ, typesInterface); ok {
		v, _ := val.Interface().(types.Interface) // TODO(go1.25): use reflect.TypeAssert
		t := v.Core()
		// TODO: panic if not the same runtime.
		return t.V
	}
	if _, ok := implements(typ, astExpr); ok {
		v, _ := val.Interface().(ast.Expr) // TODO(go1.25): use reflect.TypeAssert
		return compileExpr(ctx, v)
	}
	if _, ok := implements(typ, jsonMarshaler); ok {
		v, _ := val.Interface().(json.Marshaler) // TODO(go1.25): use reflect.TypeAssert
		b, err := v.MarshalJSON()
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "json.Marshaler"))
		}
		expr, err := parser.ParseExpr("json", b)
		if err != nil {
			panic(err) // cannot happen
		}
		return compileExpr(ctx, expr)
	}
	if _, ok := implements(typ, textMarshaler); ok {
		v, _ := val.Interface().(encoding.TextMarshaler) // TODO(go1.25): use reflect.TypeAssert
		b, err := v.MarshalText()
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "encoding.TextMarshaler"))
		}
		str := strings.ToValidUTF8(string(b), string(utf8.RuneError))
		return &adt.String{Src: src, Str: str}
	}
	if _, ok := implements(typ, goError); ok {
		v, _ := val.Interface().(error) // TODO(go1.25): use reflect.TypeAssert
		errs, ok := v.(errors.Error)
		if !ok {
			errs = ctx.Newf("%s", v.Error())
		}
		return &adt.Bottom{Err: errs}
	}

	switch typ.Kind() {
	case reflect.Bool:
		return ctx.NewBool(val.Bool())

	case reflect.String:
		str := strings.ToValidUTF8(val.String(), string(utf8.RuneError))
		// TODO: here and above: allow to fail on invalid strings.
		// if !utf8.ValidString(str) {
		// 	return ctx.AddErrf("cannot convert result to string: invalid UTF-8")
		// }
		return &adt.String{Src: src, Str: str}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := &adt.Num{Src: src, K: adt.IntKind}
		n.X = *apd.New(val.Int(), 0)
		return n

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n := &adt.Num{Src: src, K: adt.IntKind}
		n.X.Coeff.SetUint64(val.Uint())
		return n

	case reflect.Float64:
		n := &adt.Num{Src: src, K: adt.FloatKind}
		if _, err := n.X.SetFloat64(val.Float()); err != nil {
			return ctx.AddErr(errors.Promote(err, "invalid float"))
		}
		return n

	case reflect.Float32:
		n := &adt.Num{Src: src, K: adt.FloatKind}
		// NOTE: apd.Decimal has a SetFloat64 method, but no SetFloat32.
		if _, _, err := n.X.SetString(strconv.FormatFloat(val.Float(), 'E', -1, 32)); err != nil {
			return ctx.AddErr(errors.Promote(err, "invalid float"))
		}
		return n

	case reflect.Pointer, reflect.Interface:
		return fromGoValue(ctx, nilIsTop, val.Elem())

	case reflect.Struct:
		// Grow the slices to match the number of fields in the Go struct,
		// avoiding repeated slice growth in append calls below.
		numFields := typ.NumField()
		sl := &adt.StructLit{
			Src:   src,
			Decls: make([]adt.Decl, 0, numFields),
		}
		sl.Init(ctx)
		v := &adt.Vertex{
			Arcs: make([]*adt.Vertex, 0, numFields),
		}

		for i := range typ.NumField() {
			sf := typ.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			val := val.Field(i)
			if !nilIsTop && isNil(val) {
				continue
			}
			if tag, _ := sf.Tag.Lookup("json"); tag == "-" {
				continue
			}
			if isOmitEmpty(&sf) && val.IsZero() {
				continue
			}
			sub := fromGoValue(ctx, nilIsTop, val)
			if sub == nil {
				// mimic behavior of encoding/json: skip fields of unsupported types
				continue
			}
			if _, ok := sub.(*adt.Bottom); ok {
				return sub
			}

			// leave errors like we do during normal evaluation or do we want to return the error?
			name := getName(&sf)
			if name == "-" {
				continue
			}
			if sf.Anonymous && name == "" {
				arc, ok := sub.(*adt.Vertex)
				if ok {
					v.Arcs = append(v.Arcs, arc.Arcs...)
				}
				continue
			}

			f := ctx.StringLabel(name)
			sl.Decls = append(sl.Decls, &adt.Field{Label: f, Value: sub})
			v.Arcs = append(v.Arcs, ensureArcVertex(ctx, env, sub, f))
		}

		// There is no closedness or cycle info for Go structs, so we pass an empty CloseInfo.
		v.AddStruct(sl)
		v.SetValue(ctx, &adt.StructMarker{})
		v.ForceDone()
		return v

	case reflect.Map:
		obj := &adt.StructLit{Src: src}
		obj.Init(ctx)
		v := &adt.Vertex{}

		switch key := typ.Key(); key.Kind() {
		default:
			if !key.Implements(textMarshaler) {
				return ctx.AddErrf("unsupported Go type for map key (%v)", key)
			}
			fallthrough
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

			// Note that we don't use [reflect.Value.Seq2]; see the note below for [reflect.Array].
			iter := val.MapRange()
			for iter.Next() {
				k, val := iter.Key(), iter.Value()
				sub := fromGoValue(ctx, nilIsTop, val)
				// mimic behavior of encoding/json: report error of unsupported type.
				if sub == nil {
					return ctx.AddErrf("unsupported Go type (%T)", val.Interface())
				}
				if isBottom(sub) {
					return sub
				}

				s := fmt.Sprint(k)
				f := ctx.StringLabel(s)
				v.Arcs = append(v.Arcs, ensureArcVertex(ctx, env, sub, f))
			}
			slices.SortFunc(v.Arcs, func(a, b *adt.Vertex) int {
				return strings.Compare(a.Label.IdentString(ctx), b.Label.IdentString(ctx))
			})
			// Create all the adt/ast fields after sorting the arcs
			for _, arc := range v.Arcs {
				obj.Decls = append(obj.Decls, &adt.Field{Label: arc.Label, Value: arc})
			}
		}

		v.AddStruct(obj)
		v.SetValue(ctx, &adt.StructMarker{})
		v.ForceDone()
		return v

	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 { // []byte
			return &adt.Bytes{Src: src, B: val.Bytes()}
		}
		fallthrough
	case reflect.Array:
		// Grow the slices to match the number of fields in the Go struct,
		// avoiding repeated slice growth in append calls below.
		numElems := val.Len()
		src, _ := src.(*ast.ListLit)
		list := &adt.ListLit{
			Src:   src,
			Elems: make([]adt.Elem, 0, numElems),
		}
		v := &adt.Vertex{
			Arcs: make([]*adt.Vertex, 0, numElems),
		}

		// Note that we don't use [reflect.Value.Seq2],
		// as it allocates more per iteration, and we don't need the index value.
		// We can't use [reflect.Value.Seq] either, as that's just the indices.
		// See the upstream bug report: https://go.dev/issue/76357
		for i := range numElems {
			val := val.Index(i)
			x := fromGoValue(ctx, nilIsTop, val)
			if x == nil {
				return ctx.AddErrf("unsupported Go type (%T)", val.Interface())
			}
			if isBottom(x) {
				return x
			}
			list.Elems = append(list.Elems, x)
			f := adt.MakeIntLabel(adt.IntLabel, int64(i))
			v.Arcs = append(v.Arcs, ensureArcVertex(ctx, env, x, f))
		}

		v.AddConjunct(adt.MakeRootConjunct(env, list))
		v.SetValue(ctx, &adt.ListMarker{})
		v.ForceDone()
		return v
	}
	return nil
}

func fromGoBigInt(x *big.Int) apd.Decimal {
	// Integers fitting in 64 bits is rather common.
	// In that case, avoid the conversion to [apd.BigInt], which also allocates.
	if x.IsInt64() {
		var dec apd.Decimal
		dec.SetInt64(x.Int64())
		return dec
	}
	return *apd.NewWithBigInt(new(apd.BigInt).SetMathBigInt(x), 0)
}

func ensureArcVertex(ctx *adt.OpContext, env *adt.Environment, x adt.Value, l adt.Feature) *adt.Vertex {
	if arc, ok := x.(*adt.Vertex); ok {
		if arc.Label == l {
			// We already have a vertex with the correct label; do not make a copy.
			return arc
		}
		// We already have a vertex; copy it and adjust its label.
		a := *arc
		a.Label = l
		return &a
	}
	arc := &adt.Vertex{Label: l}
	arc.AddConjunct(adt.MakeRootConjunct(env, x))
	arc.SetValue(ctx, x)
	arc.ForceDone()
	return arc
}

var (
	goError        = reflect.TypeFor[error]()
	typesInterface = reflect.TypeFor[types.Interface]()
	jsonMarshaler  = reflect.TypeFor[json.Marshaler]()
	textMarshaler  = reflect.TypeFor[encoding.TextMarshaler]()
	astExpr        = reflect.TypeFor[ast.Expr]()
	astFile        = reflect.TypeFor[*ast.File]()
	bigInt         = reflect.TypeFor[*big.Int]()
	bigRat         = reflect.TypeFor[*big.Rat]()
	bigFloat       = reflect.TypeFor[*big.Float]()
	apdDecimal     = reflect.TypeFor[*apd.Decimal]()
	topSentinel    = ast.NewIdent("_")
)

// implements is like t.Implements(ifaceType) but checks whether
// either t or reflect.PointerTo(t) implements the interface.
// It also returns false for the case where t is an interface type.
func implements(t, ifaceType reflect.Type) (needAddr, ok bool) {
	switch {
	case t.Kind() == reflect.Interface:
		return false, false
	case t.Implements(ifaceType):
		return false, true
	case reflect.PointerTo(t).Implements(ifaceType):
		return true, true
	default:
		return false, false
	}
}

func fromGoType(ctx *adt.OpContext, allowNullDefault bool, t reflect.Type) (e ast.Expr, expr adt.Expr) {
	if src, t, ok := ctx.LoadType(t); ok {
		return src, t
	}

	switch reflect.Zero(t).Interface().(type) {
	case *big.Int, big.Int:
		e = ast.NewIdent("int")
		goto store

	case *big.Float, big.Float, *big.Rat, big.Rat:
		e = ast.NewIdent("number")
		goto store

	case *apd.Decimal, apd.Decimal:
		e = ast.NewIdent("number")
		goto store
	}

	// Even if this is for types that we know cast to a certain type, it can't
	// hurt to return top, as in these cases the concrete values will be
	// strict instances and there cannot be any tags that further constrain
	// the values.
	if t.Implements(jsonMarshaler) || t.Implements(textMarshaler) {
		e = topSentinel
		goto store
	}

	switch k := t.Kind(); k {
	case reflect.Pointer:
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		e, _ = fromGoType(ctx, false, elem)
		if allowNullDefault {
			e = wrapOrNull(e)
		}

	case reflect.Interface:
		switch t.Name() {
		case "error":
			// This is really null | _|_. There is no error if the error is null.
			e = ast.NewNull()
		default:
			e = topSentinel // `_`
		}

	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e = compile.LookupRange(t.Kind().String()).Source().(ast.Expr)

	case reflect.Uint, reflect.Uintptr:
		e = compile.LookupRange("uint64").Source().(ast.Expr)

	case reflect.Int:
		e = compile.LookupRange("int64").Source().(ast.Expr)

	case reflect.String:
		e = ast.NewIdent("__string")

	case reflect.Bool:
		e = ast.NewIdent("__bool")

	case reflect.Float32, reflect.Float64:
		e = ast.NewIdent("__number")

	case reflect.Struct:
		obj := &ast.StructLit{}

		// TODO: dirty trick: set this to a temporary Vertex and then update the
		// arcs and conjuncts of this vertex below. This will allow circular
		// references. Maybe have a special kind of "hardlink" reference.
		ctx.StoreType(t, obj, nil)

		for i := range t.NumField() {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			_, ok := f.Tag.Lookup("cue")
			elem, _ := fromGoType(ctx, !ok, f.Type)
			if isBad(elem) {
				continue // Ignore fields for unsupported types
			}

			// leave errors like we do during normal evaluation or do we
			// want to return the error?
			name := getName(&f)
			if name == "-" {
				continue
			}

			if tag, ok := f.Tag.Lookup("cue"); ok {
				v := parseTag(ctx, name, tag)
				if isBad(v) {
					return v, nil
				}
				elem = ast.NewBinExpr(token.AND, elem, v)
			}
			// TODO: if an identifier starts with __ (or otherwise is not a
			// valid CUE name), make it a string and create a map to a new
			// name for references.

			// The Go JSON decoder always allows a value to be undefined.
			d := &ast.Field{Label: ast.NewIdent(name), Value: elem}
			if isOptional(&f) {
				d.Constraint = token.OPTION
			}
			obj.Elts = append(obj.Elts, d)
		}

		// TODO: should we validate references here? Can be done using
		// astutil.ToFile and astutil.Resolve.

		e = obj

	case reflect.Array, reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			e = ast.NewIdent("__bytes")
		} else {
			elem, _ := fromGoType(ctx, allowNullDefault, t.Elem())
			if elem == nil {
				return &ast.BadExpr{}, ctx.AddErrf("unsupported Go type (%v)", t.Elem())
			}

			if t.Kind() == reflect.Array {
				e = ast.NewCall(
					ast.NewSel(&ast.Ident{
						Name: "list",
						Node: ast.NewImport(nil, "list"),
					}, "Repeat"),
					ast.NewList(elem),
					ast.NewLit(token.INT, strconv.Itoa(t.Len())))
			} else {
				e = ast.NewList(&ast.Ellipsis{Type: elem})
			}
		}
		if k == reflect.Slice {
			e = wrapOrNull(e)
		}

	case reflect.Map:
		switch key := t.Key(); key.Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8,
			reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		default:
			return &ast.BadExpr{}, ctx.AddErrf("unsupported Go type for map key (%v)", key)
		}

		v, x := fromGoType(ctx, allowNullDefault, t.Elem())
		if v == nil {
			return &ast.BadExpr{}, ctx.AddErrf("unsupported Go type (%v)", t.Elem())
		}
		if isBad(v) {
			return v, x
		}

		e = ast.NewStruct(&ast.Field{
			Label: ast.NewList(ast.NewIdent("__string")),
			Value: v,
		})

		e = wrapOrNull(e)
	}

store:
	// TODO: store error if not nil?
	if e != nil {
		f := &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: e}}}
		astutil.Resolve(f, func(_ token.Pos, msg string, args ...interface{}) {
			ctx.AddErrf(msg, args...)
		})
		var x adt.Expr
		x2, err := compile.Expr(nil, ctx, pkgID(), e)
		if err != nil {
			b := &adt.Bottom{Err: err}
			ctx.AddBottom(b)
			x = b
		} else {
			x = x2.Expr()
		}
		ctx.StoreType(t, e, x)
		return e, x
	}
	return e, nil
}

func isBottom(x adt.Node) bool {
	if x == nil {
		return true
	}
	b, _ := x.(*adt.Bottom)
	return b != nil
}

func isBad(x ast.Expr) bool {
	if x == nil {
		return true
	}
	if bad, _ := x.(*ast.BadExpr); bad != nil {
		return true
	}
	return false
}

func wrapOrNull(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.NULL {
			return x
		}
	case *ast.BadExpr:
		return e
	}
	return makeNullable(e, true)
}

func makeNullable(e ast.Expr, nullIsDefault bool) ast.Expr {
	var null ast.Expr = ast.NewNull()
	if nullIsDefault {
		null = &ast.UnaryExpr{Op: token.MUL, X: null}
	}
	return ast.NewBinExpr(token.OR, null, e)
}

// pkgID returns a package path that can never resolve to an existing package.
func pkgID() string {
	return "_"
}
