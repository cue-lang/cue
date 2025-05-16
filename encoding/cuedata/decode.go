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

package cuedata

import (
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// Decode initializes the value pointed to by x with expression e, following
// the same rules specified for Encode.
//
// An error is returned if x is nil or not a pointer.
func Decode(e ast.Expr, x any) error {
	if x == nil {
		return fmt.Errorf("cuedata: x must not be nil")
	}
	rv := reflect.ValueOf(x)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("cuedata: x must be a non-nil pointer value")
	}
	return decodeIntoValue(e, rv.Elem())
}

// Reflect quirk helpers.
var (
	jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	bigIntPtrType       = reflect.TypeFor[*big.Int]()
	bigFloatPtrType     = reflect.TypeFor[*big.Float]()
)

// decodeIntoValue decodes the expression into pv.
func decodeIntoValue(e ast.Expr, v reflect.Value) error {
	// Handle null up-front – it zeroes / nils the destination as appropriate.
	if isNullExpr(e) {
		v.SetZero()
		return nil
	}

	// Special-case *big.Int and *big.Float – these cannot be handled nicely by
	// reflection below because they are pointer types with value semantics.
	switch v.Type() {
	case bigIntPtrType:
		return decodeBigInt(e, v)
	case bigFloatPtrType:
		return decodeBigFloat(e, v)
	}

	// Interface support comes first – json.Unmarshaler then TextUnmarshaler.
	if v.Type().Implements(jsonUnmarshalerType) {
		g, err := toGoValue(e)
		if err != nil {
			return err
		}
		b, err := json.Marshal(g)
		if err != nil {
			return fmt.Errorf("cuedata: internal json round-trip: %v", err)
		}
		return v.Interface().(json.Unmarshaler).UnmarshalJSON(b)
	}
	if v.Type().Implements(textUnmarshalerType) {
		txt, err := literalAsText(e)
		if err != nil {
			return err
		}
		return v.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(txt))
	}

	switch v.Kind() {
	case reflect.Bool:
		b, err := parseBoolFromExpr(e)
		if err != nil {
			return err
		}
		v.SetBool(b)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := parseIntFromExpr(e, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetInt(i)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u, err := parseUintFromExpr(e, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetUint(u)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := parseFloatFromExpr(e, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetFloat(f)
		return nil

	case reflect.String:
		s, err := basicLitString(e)
		if err != nil {
			return err
		}
		v.SetString(toValidUTF8(s))
		return nil

	case reflect.Pointer:
		// Allocate if nil and decode the pointed-to value.
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return decodeIntoValue(e, v.Elem())

	case reflect.Slice:
		// Special-case []byte which maps to a CUE bytes literal.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			s, err := basicLitString(e)
			if err != nil {
				return err
			}
			bs, err := decodeBytesLiteral(s)
			if err != nil {
				return err
			}
			v.SetBytes(bs)
			return nil
		}
		ll, ok := e.(*ast.ListLit)
		if !ok {
			return typeMismatch(e, "list literal")
		}
		v.Set(reflect.MakeSlice(v.Type(), len(ll.Elts), len(ll.Elts)))
		for i, el := range ll.Elts {
			if err := decodeIntoValue(el, v.Index(i).Addr()); err != nil {
				return err
			}
		}
		return nil

	case reflect.Array:
		ll, ok := e.(*ast.ListLit)
		if !ok {
			return typeMismatch(e, "list literal")
		}
		if len(ll.Elts) != v.Len() {
			return fmt.Errorf("cuedata: array length mismatch: have %d literals, need %d", len(ll.Elts), v.Len())
		}
		for i, el := range ll.Elts {
			if err := decodeIntoValue(el, v.Index(i).Addr()); err != nil {
				return err
			}
		}
		return nil

	case reflect.Map:
		sl, ok := e.(*ast.StructLit)
		if !ok {
			return typeMismatch(e, "struct literal")
		}
		if v.IsNil() {
			v.Set(reflect.MakeMapWithSize(v.Type(), len(sl.Elts)))
		}
		keyType := v.Type().Key()
		elemType := v.Type().Elem()
		for _, d := range sl.Elts {
			f, ok := d.(*ast.Field)
			if !ok {
				continue // ignore non-field decls – Encode never emits these
			}
			lbl := labelString(f.Label)
			keyVal := reflect.New(keyType).Elem()
			if err := convertLabelToKey(lbl, &keyVal); err != nil {
				return err
			}
			elemPtr := reflect.New(elemType)
			if err := decodeIntoValue(f.Value, elemPtr); err != nil {
				return err
			}
			v.SetMapIndex(keyVal, elemPtr.Elem())
		}
		return nil

	case reflect.Struct:
		sl, ok := e.(*ast.StructLit)
		if !ok {
			return typeMismatch(e, "struct literal")
		}
		fieldMap := make(map[string]*ast.Field, len(sl.Elts))
		for _, d := range sl.Elts {
			if f, ok := d.(*ast.Field); ok {
				fieldMap[labelString(f.Label)] = f
			}
		}
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" {
				continue // unexported
			}
			name := getName(&sf)
			switch {
			case name == "-":
				continue // explicitly ignored
			case sf.Anonymous && name == "":
				// Embedded struct: decode the entire expression into it.
				if err := decodeIntoValue(e, v.Field(i).Addr()); err != nil {
					return err
				}
			default:
				f, ok := fieldMap[name]
				if !ok {
					continue // field absent – leave zero value
				}
				if err := decodeIntoValue(f.Value, v.Field(i).Addr()); err != nil {
					return err
				}
			}
		}
		return nil

	case reflect.Interface:
		gv, err := toGoValue(e)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(gv))
		return nil
	}

	return fmt.Errorf("cuedata: unsupported Go kind %s", v.Kind())
}

func isNullExpr(e ast.Expr) bool {
	if bl, ok := e.(*ast.BasicLit); ok {
		return bl.Kind == token.NULL
	}
	return false
}

func typeMismatch(e ast.Expr, want string) error {
	return fmt.Errorf("cuedata: cannot decode %T – need %s", e, want)
}

func parseBoolFromExpr(e ast.Expr) (bool, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok {
		return false, typeMismatch(e, "boolean literal")
	}
	switch strings.ToLower(bl.Value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("cuedata: invalid boolean literal %q", bl.Value)
}

func parseIntFromExpr(e ast.Expr, bits int) (int64, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.INT {
		return 0, typeMismatch(e, "integer literal")
	}
	return strconv.ParseInt(bl.Value, 0, bits)
}

func parseUintFromExpr(e ast.Expr, bits int) (uint64, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.INT {
		return 0, typeMismatch(e, "integer literal")
	}
	return strconv.ParseUint(bl.Value, 0, bits)
}

func parseFloatFromExpr(e ast.Expr, bits int) (float64, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || (bl.Kind != token.FLOAT && bl.Kind != token.INT) {
		return 0, typeMismatch(e, "number literal")
	}
	return strconv.ParseFloat(bl.Value, bits)
}

func basicLitString(e ast.Expr) (string, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", typeMismatch(e, "string literal")
	}
	return literal.Unquote(bl.Value)
}

func decodeBytesLiteral(s string) ([]byte, error) {
	// literal.Unquote has already removed quotes and decoded escapes – the
	// resulting string holds the raw bytes.
	return []byte(s), nil
}

func convertLabelToKey(label string, key *reflect.Value) error {
	// Unquote if quoted.
	if strings.HasPrefix(label, "\"") || strings.HasPrefix(label, "`") || strings.HasPrefix(label, "'") {
		var err error
		label, err = literal.Unquote(label)
		if err != nil {
			return err
		}
	}

	// Check if the key implements TextUnmarshaler.
	if key.Addr().Type().Implements(textUnmarshalerType) {
		return key.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(label))
	}

	switch key.Kind() {
	case reflect.String:
		key.SetString(label)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(label, 0, key.Type().Bits())
		if err != nil {
			return err
		}
		key.SetInt(i)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u, err := strconv.ParseUint(label, 0, key.Type().Bits())
		if err != nil {
			return err
		}
		key.SetUint(u)
		return nil
	default:
		return fmt.Errorf("cuedata: unsupported map key kind %s", key.Kind())
	}
}

func literalAsText(e ast.Expr) (string, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok {
		return "", typeMismatch(e, "literal")
	}
	switch bl.Kind {
	case token.STRING:
		return literal.Unquote(bl.Value)
	case token.INT, token.FLOAT:
		return bl.Value, nil
	default:
		return "", fmt.Errorf("cuedata: literal kind %s cannot be converted to text", bl.Kind)
	}
}

// toGoValue converts the AST expression to a generic Go representation similar
// to what encoding/json produces: bool, string, float64, map[string]any,
// []any, or nil.
func toGoValue(e ast.Expr) (any, error) {
	switch x := e.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.NULL:
			return nil, nil
		case token.INT:
			if i, err := strconv.ParseInt(x.Value, 0, 64); err == nil {
				return i, nil
			} else {
				return nil, err
			}
		case token.FLOAT:
			f, err := strconv.ParseFloat(x.Value, 64)
			if err != nil {
				return nil, err
			}
			return f, nil
		case token.STRING:
			s, err := literal.Unquote(x.Value)
			if err != nil {
				return nil, err
			}
			return s, nil
		default:
			// Could be bool which Encode always produces with NewBool.
			if strings.EqualFold(x.Value, "true") {
				return true, nil
			}
			if strings.EqualFold(x.Value, "false") {
				return false, nil
			}
		}
	case *ast.ListLit:
		arr := make([]any, len(x.Elts))
		for i, el := range x.Elts {
			v, err := toGoValue(el)
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return arr, nil
	case *ast.StructLit:
		m := make(map[string]any, len(x.Elts))
		for _, d := range x.Elts {
			if f, ok := d.(*ast.Field); ok {
				k := labelString(f.Label)
				v, err := toGoValue(f.Value)
				if err != nil {
					return nil, err
				}
				// Unquote keys as required.
				if strings.HasPrefix(k, "\"") || strings.HasPrefix(k, "`") || strings.HasPrefix(k, "'") {
					if uq, err := literal.Unquote(k); err == nil {
						k = uq
					}
				}
				m[k] = v
			}
		}
		return m, nil
	}
	return nil, fmt.Errorf("cuedata: cannot convert expression of type %T to Go value", e)
}

func decodeBigInt(e ast.Expr, v reflect.Value) error {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.INT {
		return typeMismatch(e, "integer literal")
	}
	if v.IsNil() {
		v.Set(reflect.ValueOf(&big.Int{}))
	}
	bi := v.Interface().(*big.Int)
	if _, ok := bi.SetString(bl.Value, 0); !ok {
		return fmt.Errorf("cuedata: invalid big.Int literal %q", bl.Value)
	}
	return nil
}

func decodeBigFloat(e ast.Expr, pv reflect.Value) error {
	bl, ok := e.(*ast.BasicLit)
	if !ok || (bl.Kind != token.FLOAT && bl.Kind != token.INT) {
		return typeMismatch(e, "number literal")
	}
	if pv.IsNil() {
		pv.Set(reflect.ValueOf(&big.Float{}))
	}
	bf := pv.Interface().(*big.Float)
	if _, ok := bf.SetString(bl.Value); !ok {
		return fmt.Errorf("cuedata: invalid big.Float literal %q", bl.Value)
	}
	return nil
}

// toValidUTF8 is copied from encode.go so that decode can reuse it without
// circular dependency.
func toValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string([]rune(s))
}
