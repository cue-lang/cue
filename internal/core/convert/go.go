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

	"github.com/cockroachdb/apd/v3"
	"golang.org/x/text/encoding/unicode"

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

// FromGoValue converts a Go value to an internal CUE value.
// The returned CUE value is finalized and concrete.
func FromGoValue(ctx *adt.OpContext, x any, nilIsTop bool) adt.Value {
	v := fromGoValue(ctx, nilIsTop, x)
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
		return &adt.Bottom{
			Err: errors.Promote(err, "compile")}
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
		ctx.AddErr(errors.Wrapf(err, ctx.Pos(),
			"invalid tag %q for field %q", tag, field))
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
	switch x.Kind() {
	// Only check for supported types; ignore func and chan.
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return x.IsNil()
	}
	return false
}

func fromGoValue(ctx *adt.OpContext, nilIsTop bool, x interface{}) (result adt.Value) {
	src := ctx.Source()
	switch v := x.(type) {
	case types.Interface:
		t := &types.Value{}
		v.Core(t)
		// TODO: panic if not the same runtime.
		return t.V
	case nil:
		if nilIsTop {
			ident, _ := src.(*ast.Ident)
			return &adt.Top{Src: ident}
		}
		return &adt.Null{Src: src}

	case *ast.File:
		x, err := compile.Files(nil, ctx, pkgID(), v)
		if err != nil {
			return &adt.Bottom{Err: errors.Promote(err, "compile")}
		}
		if _, n := x.SingleConjunct(); n != 1 {
			panic("unexpected length")
		}
		return x

	case ast.Expr:
		return compileExpr(ctx, v)

	case *big.Int:
		v2 := new(apd.BigInt).SetMathBigInt(v)
		return &adt.Num{
			Src: src,
			K:   adt.IntKind,
			X:   *apd.NewWithBigInt(v2, 0),
		}

	case *big.Rat:
		// should we represent this as a binary operation?
		n := &adt.Num{Src: src, K: adt.IntKind}
		_, err := internal.BaseContext.Quo(&n.X,
			apd.NewWithBigInt(new(apd.BigInt).SetMathBigInt(v.Num()), 0),
			apd.NewWithBigInt(new(apd.BigInt).SetMathBigInt(v.Denom()), 0),
		)
		if err != nil {
			return ctx.AddErrf("could not convert *big.Rat: %v", err)
		}
		if !v.IsInt() {
			n.K = adt.FloatKind
		}
		return n

	case *big.Float:
		n := &adt.Num{Src: src, K: adt.FloatKind}
		_, _, err := n.X.SetString(v.String())
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "invalid float"))
		}
		return n

	case *apd.Decimal:
		// TODO: should we allow an "int" bit to be set here? It is a bit
		// tricky, as we would also need to pass down the result of rounding.
		// So more likely an API must return explicitly whether a value is
		// a float or an int after all.
		// The code to autodetect whether something is an integer can be done
		// with this:
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

	case json.Marshaler:
		b, err := v.MarshalJSON()
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "json.Marshaler"))
		}
		expr, err := parser.ParseExpr("json", b)
		if err != nil {
			panic(err) // cannot happen
		}
		return compileExpr(ctx, expr)

	case encoding.TextMarshaler:
		b, err := v.MarshalText()
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "encoding.TextMarshaler"))
		}
		s, _ := unicode.UTF8.NewEncoder().String(string(b))
		return &adt.String{Src: src, Str: s}

	case error:
		var errs errors.Error
		switch x := v.(type) {
		case errors.Error:
			errs = x
		default:
			errs = ctx.Newf("%s", x.Error())
		}
		return &adt.Bottom{Err: errs}
	case bool:
		return ctx.NewBool(v)
	case string:
		s, _ := unicode.UTF8.NewEncoder().String(v)
		return &adt.String{Src: src, Str: s}
	case []byte:
		return &adt.Bytes{Src: src, B: v}
	case int:
		return toInt(ctx, int64(v))
	case int8:
		return toInt(ctx, int64(v))
	case int16:
		return toInt(ctx, int64(v))
	case int32:
		return toInt(ctx, int64(v))
	case int64:
		return toInt(ctx, v)
	case uint:
		return toUint(ctx, uint64(v))
	case uint8:
		return toUint(ctx, uint64(v))
	case uint16:
		return toUint(ctx, uint64(v))
	case uint32:
		return toUint(ctx, uint64(v))
	case uint64:
		return toUint(ctx, v)
	case uintptr:
		return toUint(ctx, uint64(v))
	case float64:
		n := &adt.Num{Src: src, K: adt.FloatKind}
		_, err := n.X.SetFloat64(v)
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "invalid float"))
		}
		return n
	case float32:
		n := &adt.Num{Src: src, K: adt.FloatKind}
		// apd.Decimal has a SetFloat64 method, but no SetFloat32.
		_, _, err := n.X.SetString(strconv.FormatFloat(float64(v), 'E', -1, 32))
		if err != nil {
			return ctx.AddErr(errors.Promote(err, "invalid float"))
		}
		return n

	default:
		value := reflect.ValueOf(v)
		switch value.Kind() {
		case reflect.Bool:
			return ctx.NewBool(value.Bool())

		case reflect.String:
			str := value.String()
			str, _ = unicode.UTF8.NewEncoder().String(str)
			// TODO: here and above: allow to fail on invalid strings.
			// if !utf8.ValidString(str) {
			// 	return ctx.AddErrf("cannot convert result to string: invalid UTF-8")
			// }
			return &adt.String{Src: src, Str: str}

		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64:
			return toInt(ctx, value.Int())

		case reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return toUint(ctx, value.Uint())

		case reflect.Float32, reflect.Float64:
			return fromGoValue(ctx, nilIsTop, value.Float())

		case reflect.Pointer:
			if value.IsNil() {
				if nilIsTop {
					ident, _ := src.(*ast.Ident)
					return &adt.Top{Src: ident}
				}
				return &adt.Null{Src: src}
			}
			return fromGoValue(ctx, nilIsTop, value.Elem().Interface())

		case reflect.Struct:
			sl := &adt.StructLit{Src: ast.NewStruct()}
			sl.Init(ctx)
			v := &adt.Vertex{}

			t := value.Type()
			for i := range value.NumField() {
				sf := t.Field(i)
				if sf.PkgPath != "" {
					continue
				}
				val := value.Field(i)
				if !nilIsTop && isNil(val) {
					continue
				}
				if tag, _ := sf.Tag.Lookup("json"); tag == "-" {
					continue
				}
				if isOmitEmpty(&sf) && val.IsZero() {
					continue
				}
				sub := fromGoValue(ctx, nilIsTop, val.Interface())
				if sub == nil {
					// mimic behavior of encoding/json: skip fields of unsupported types
					continue
				}
				if _, ok := sub.(*adt.Bottom); ok {
					return sub
				}

				// leave errors like we do during normal evaluation or do we
				// want to return the error?
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
				createField(ctx, f, sub, sl)
				v.Arcs = append(v.Arcs, ensureArcVertex(ctx, sub, f))
			}

			env := ctx.Env(0)
			if env == nil {
				env = &adt.Environment{}
			}
			// There is no closedness or cycle info for Go structs, so we
			// pass an empty CloseInfo.
			v.AddStruct(sl, env, adt.CloseInfo{})
			v.SetValue(ctx, &adt.StructMarker{})
			v.ForceDone()

			return v

		case reflect.Map:
			obj := &adt.StructLit{Src: ast.NewStruct()}
			obj.Init(ctx)
			v := &adt.Vertex{}

			t := value.Type()
			switch key := t.Key(); key.Kind() {
			default:
				if !key.Implements(textMarshaler) {
					return ctx.AddErrf("unsupported Go type for map key (%v)", key)
				}
				fallthrough
			case reflect.String,
				reflect.Int, reflect.Int8, reflect.Int16,
				reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16,
				reflect.Uint32, reflect.Uint64, reflect.Uintptr:

				for k, val := range value.Seq2() {
					// if isNil(val) {
					// 	continue
					// }

					sub := fromGoValue(ctx, nilIsTop, val.Interface())
					// mimic behavior of encoding/json: report error of
					// unsupported type.
					if sub == nil {
						return ctx.AddErrf("unsupported Go type (%T)", val.Interface())
					}
					if isBottom(sub) {
						return sub
					}

					s := fmt.Sprint(k)
					f := ctx.StringLabel(s)
					v.Arcs = append(v.Arcs, ensureArcVertex(ctx, sub, f))
				}
				slices.SortFunc(v.Arcs, func(a, b *adt.Vertex) int {
					return strings.Compare(a.Label.IdentString(ctx), b.Label.IdentString(ctx))
				})
				// Create all the adt/ast fields after sorting the arcs
				for _, arc := range v.Arcs {
					createField(ctx, arc.Label, arc, obj)
				}
			}

			env := ctx.Env(0)
			if env == nil {
				env = &adt.Environment{}
			}
			v.AddStruct(obj, env, adt.CloseInfo{})
			v.SetValue(ctx, &adt.StructMarker{})
			v.ForceDone()

			return v

		case reflect.Slice, reflect.Array:
			list := &adt.ListLit{Src: ast.NewList()}
			v := &adt.Vertex{}

			i := 0
			for _, val := range value.Seq2() {
				x := fromGoValue(ctx, nilIsTop, val.Interface())
				if x == nil {
					return ctx.AddErrf("unsupported Go type (%T)",
						val.Interface())
				}
				if isBottom(x) {
					return x
				}
				list.Elems = append(list.Elems, x)
				f := adt.MakeIntLabel(adt.IntLabel, int64(i))
				v.Arcs = append(v.Arcs, ensureArcVertex(ctx, x, f))
				i++
			}

			env := ctx.Env(0)
			if env == nil {
				env = &adt.Environment{}
			}
			v.AddConjunct(adt.MakeRootConjunct(env, list))
			v.SetValue(ctx, &adt.ListMarker{})
			v.ForceDone()

			return v
		}
	}
	return nil
}

func ensureArcVertex(ctx *adt.OpContext, x adt.Value, l adt.Feature) *adt.Vertex {
	if arc, ok := x.(*adt.Vertex); ok {
		if arc.Label == l {
			// TODO(mvdan): avoid these calls entirely.
			// Do this for later to avoid merge conflicts with other changes.
			return arc
		}
		a := *arc
		a.Label = l
		return &a
	}
	env := ctx.Env(0)
	if env == nil {
		env = &adt.Environment{}
	}
	arc := &adt.Vertex{Label: l}
	arc.AddConjunct(adt.MakeRootConjunct(env, x))
	arc.SetValue(ctx, x)
	arc.ForceDone()
	return arc
}

func createField(ctx *adt.OpContext, l adt.Feature, sub adt.Value, sl *adt.StructLit) {
	src := sl.Src.(*ast.StructLit)
	astField := &ast.Field{
		Label:      ast.NewIdent(l.IdentString(ctx)),
		Constraint: token.ILLEGAL,
	}
	if expr, ok := sub.Source().(ast.Expr); ok {
		astField.Value = expr
	}
	src.Elts = append(src.Elts, astField)
	field := &adt.Field{Label: l, Value: sub, Src: astField}
	sl.Decls = append(sl.Decls, field)
}

func toInt(ctx *adt.OpContext, x int64) adt.Value {
	n := &adt.Num{Src: ctx.Source(), K: adt.IntKind}
	n.X = *apd.New(x, 0)
	return n
}

func toUint(ctx *adt.OpContext, x uint64) adt.Value {
	n := &adt.Num{Src: ctx.Source(), K: adt.IntKind}
	n.X.Coeff.SetUint64(x)
	return n
}

var (
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
	topSentinel   = ast.NewIdent("_")
)

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

			// The GO JSON decoder always allows a value to be undefined.
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
				b := ctx.AddErrf("unsupported Go type (%v)", t.Elem())
				return &ast.BadExpr{}, b
			}

			if t.Kind() == reflect.Array {
				e = ast.NewCall(
					ast.NewSel(&ast.Ident{
						Name: "list",
						Node: ast.NewImport(nil, "list")},
						"Repeat"),
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
			b := ctx.AddErrf("unsupported Go type for map key (%v)", key)
			return &ast.BadExpr{}, b
		}

		v, x := fromGoType(ctx, allowNullDefault, t.Elem())
		if v == nil {
			b := ctx.AddErrf("unsupported Go type (%v)", t.Elem())
			return &ast.BadExpr{}, b
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
