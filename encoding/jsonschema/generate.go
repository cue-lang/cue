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

package jsonschema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
)

// GenerateConfig configures JSON Schema generation from CUE values.
type GenerateConfig struct {
	// Version specifies the version of JSON Schema to generate.
	// Currently only VersionDraft2020_12 is supported.
	Version Version

	// NameFunc is used to determine how references map to
	// JSON Schema definition names. It is passed the
	// root value (usually a package) and the path to that value
	// within it, as returned by [cue.Value.ReferencePath].
	//
	// If this is nil, DefaultNameFunc will be used.
	NameFunc func(root cue.Value, path cue.Path) string
}

// Generate generates a JSON Schema for the given CUE value,
// with the returned AST representing the generated JSON result.
func Generate(v cue.Value, cfg *GenerateConfig) (ast.Expr, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &GenerateConfig{}
	} else {
		// Prevent mutation of the argument.
		cfg = *ref(cfg)
	}
	if cfg.NameFunc == nil {
		cfg.NameFunc = defaultNameFunc
	}
	if cfg.Version == VersionUnknown {
		cfg.Version = VersionDraft2020_12
	}

	g := &generator{
		cfg:  cfg,
		defs: make(map[string]item),
	}
	item := g.makeItem(v)
	item = mergeAllOf(item)
	item = enumFromConst(item)
	m := item.generate(g)

	// Add schema version metadata and definitions.
	m["$schema"] = VersionDraft2020_12.String()
	if len(g.defs) != 0 {
		defs := make(map[string]any)
		for name, def := range g.defs {
			defs[name] = def.generate(g)
		}
		m["$defs"] = defs
	}
	if g.err != nil {
		return nil, g.err
	}
	finalv := v.Context().Encode(m)
	if err := finalv.Err(); err != nil {
		return nil, err
	}
	return finalv.Syntax().(ast.Expr), nil
}

func mergeAllOf(it item) item {
	switch it := it.(type) {
	case *itemAllOf:
		it1 := &itemAllOf{
			elems: make([]item, 0, len(it.elems)),
		}
		for _, e := range it.elems {
			e := mergeAllOf(e)
			if e1, ok := e.(*itemAllOf); ok {
				it1.elems = append(it1.elems, e1.elems...)
			} else {
				it1.elems = append(it1.elems, e)
			}
		}
		// Remove elements that are entirely redundant.
		it1.elems = dedupe(it1.elems, func(x, y item) bool { return reflect.DeepEqual(x, y) })
		return it1
	default:
		return it.walk(mergeAllOf)
	}
}

func enumFromConst(it item) item {
	switch it := it.(type) {
	case *itemAnyOf:
		if !allTrue(it.elems, func(it item) bool {
			_, ok := it.(*itemConst)
			return ok
		}) {
			return it
		}
		// All items are const. We can make an enum from this.
		// TODO this doesn't cover cases where there are some
		// const values and some noncrete values.
		it1 := &itemEnum{
			values: make([]json.RawMessage, 0, len(it.elems)),
		}
		for _, e := range it.elems {
			it1.values = append(it1.values, e.(*itemConst).value)
		}
		return it1
	default:
		return it.walk(enumFromConst)
	}
}

type generator struct {
	cfg *GenerateConfig

	// err holds any errors accumulated during translation.
	err errors.Error

	// defs holds any definitions made during the course of generation,
	// indexed by the entry name within the `$defs` field.
	defs map[string]item
}

func (g *generator) addError(pos cue.Value, err error) {
	// TODO pos
	g.err = errors.Append(g.err, errors.Promote(err, ""))
}

// makeItem returns an item representing the JSON Schema
// for v in naive form.
func (g *generator) makeItem(v cue.Value) item {
	op, args := v.Expr()
	//	log.Printf("makeItem{ path: %v; op %v; kind %v; args: (%s) {", v.Path(), op, v.IncompleteKind(), strings.Join(mapSlice(args, func(v cue.Value) string {
	//		return fmt.Sprintf("«%#v»", v)
	//	}), ", "))
	//defer log.Printf("}}")
	switch op {
	case cue.NoOp, cue.SelectorOp:
		pkg, path := v.ReferencePath()
		//		if len(path.Selectors()) > 0 {
		//			log.Printf("referencePath (%#v) -> %#v (inst %#v) in %v", v, pkg, pkg.BuildInstance(), path)
		//		}
		if pkg.Exists() {
			// It's a reference: generate a definition for it.
			// TODO Not all references need or should have a definition.
			if name := g.cfg.NameFunc(pkg, path); name != "" {
				ref := &itemRef{
					defName: name,
				}
				if _, ok := g.defs[name]; ok {
					// Already defined.
					return ref
				}
				g.defs[name] = nil // Prevent infinite loops on cycles.
				g.defs[name] = g.makeItem(v.Eval())
				return ref
			}
		}
	case cue.AndOp:
		return &itemAllOf{
			elems: mapSlice(args, g.makeItem),
		}
	case cue.OrOp:
		return &itemAnyOf{
			elems: mapSlice(args, g.makeItem),
		}
	case cue.RegexMatchOp,
		cue.NotRegexMatchOp:
		re, err := args[0].String()
		if err != nil {
			g.addError(args[0], err)
			return &itemFalse{}
		}
		var m item = &itemPattern{
			regexp: re,
		}
		if op == cue.NotRegexMatchOp {
			m = &itemNot{
				elem: m,
			}
		}
		return &itemAllOf{
			elems: []item{
				&itemType{
					kinds: []string{"string"},
				},
				m,
			},
		}
	case cue.EqualOp,
		cue.NotEqualOp:
		if len(args) > 1 {
			// Binary operations can't be expressed in JSON Schema.
			break
		}
		data, err := args[0].MarshalJSON()
		if err != nil {
			// If it's not concrete, we can't represent it in JSON Schema
			// so accept anything.
			return &itemTrue{}
		}
		it := &itemConst{
			value: data,
		}
		if op == cue.NotEqualOp {
			return it
		}
		return &itemNot{
			elem: it,
		}
	case cue.LessThanOp,
		cue.LessThanEqualOp,
		cue.GreaterThanOp,
		cue.GreaterThanEqualOp:
		if len(args) > 1 {
			// Binary operations can't be expressed in JSON Schema.
			break
		}
		switch args[0].Kind() {
		case cue.FloatKind, cue.IntKind:
			n, err := args[0].Float64()
			if err != nil {
				// Probably non-concrete.
				return &itemTrue{}
			}
			return &itemBounds{
				constraint: op,
				n:          n,
			}
		case cue.StringKind:
			// Can't express bounds on strings in JSON Schema
			return &itemTrue{}
		default:
			g.addError(args[0], fmt.Errorf("bad argument to unary comparison"))
			return &itemFalse{}
		}
	case cue.CallOp:
		return g.makeCallItem(v, args)
	}
	if isConcreteScalar(v) {
		data, err := v.MarshalJSON()
		if err != nil {
			// Shouldn't happen.
			g.addError(v, err)
			return &itemFalse{}
		}
		return &itemConst{
			value: data,
		}
	}
	kind := v.IncompleteKind()
	if kind == cue.TopKind {
		return &itemTrue{}
	}
	var it item
	switch kind {
	case cue.StructKind:
		it = g.makeStructItem(v)
	case cue.ListKind:
		// TODO
	}
	ty := &itemType{
		kinds: cueKindToJSONSchemaTypes(kind),
	}
	if it != nil {
		return &itemAllOf{
			elems: []item{ty, it},
		}
	}
	return ty
}

func (g *generator) makeCallItem(v cue.Value, args []cue.Value) item {
	if len(args) < 1 {
		// Invalid call - not enough arguments
		g.addError(v, fmt.Errorf("call operation with no function"))
		return &itemFalse{}
	}

	// Get the function name from the first argument
	funcName := fmt.Sprint(args[0])

	switch funcName {
	case "strings.MinRunes":
		if len(args) != 2 {
			g.addError(v, fmt.Errorf("strings.MinRunes expects 1 argument, got %d", len(args)-1))
			return &itemFalse{}
		}
		n, err := args[1].Int64()
		if err != nil {
			g.addError(args[1], err)
			return &itemFalse{}
		}
		return &itemAllOf{
			elems: []item{
				&itemType{kinds: []string{"string"}},
				&itemLengthBounds{constraint: cue.GreaterThanEqualOp, n: int(n)},
			},
		}

	case "strings.MaxRunes":
		if len(args) != 2 {
			g.addError(v, fmt.Errorf("strings.MaxRunes expects 1 argument, got %d", len(args)-1))
			return &itemFalse{}
		}
		n, err := args[1].Int64()
		if err != nil {
			g.addError(args[1], err)
			return &itemFalse{}
		}
		return &itemAllOf{
			elems: []item{
				&itemType{kinds: []string{"string"}},
				&itemLengthBounds{constraint: cue.LessThanEqualOp, n: int(n)},
			},
		}

	case "math.MultipleOf":
		if len(args) != 2 {
			g.addError(v, fmt.Errorf("math.MultipleOf expects 1 argument, got %d", len(args)-1))
			return &itemFalse{}
		}
		n, err := args[1].Float64()
		if err != nil {
			g.addError(args[1], err)
			return &itemFalse{}
		}
		return &itemAllOf{
			elems: []item{
				&itemType{kinds: []string{"number"}},
				&itemMultipleOf{n: n},
			},
		}

	case "time.Format":
		if len(args) != 2 {
			g.addError(v, fmt.Errorf("time.Format expects 1 argument, got %d", len(args)-1))
			return &itemFalse{}
		}
		layout, err := args[1].String()
		if err != nil {
			g.addError(args[1], err)
			return &itemFalse{}
		}
		// Convert CUE time layout to JSON Schema format
		var format string
		switch layout {
		case "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05.999999999Z07:00":
			format = "date-time"
		case "2006-01-02":
			format = "date"
		case "15:04:05":
			format = "time"
		default:
			// For other layouts, we can't express them in JSON Schema
			// but at least we know it's a string.
			return &itemType{kinds: []string{"string"}}
		}
		return &itemAllOf{
			elems: []item{
				&itemType{kinds: []string{"string"}},
				&itemFormat{format: format},
			},
		}

	default:
		// For unknown functions, accept anything rather than fail.
		// This allows for gradual implementation of more function types
		return &itemTrue{}
	}
}

func (g *generator) makeStructItem(v cue.Value) item {
	var props itemProperties

	// TODO include pattern constraints in the results when that's implemented
	iter, err := v.Fields(cue.Optional(true))
	if err != nil {
		g.addError(v, err)
		return &itemFalse{}
	}
	for iter.Next() {
		sel := iter.Selector()
		switch sel.ConstraintType() {
		case cue.OptionalConstraint:
		case cue.RequiredConstraint:
			props.required = append(props.required, sel.Unquoted())
		default:
			// It's a regular field. If it's concrete, then we can
			// consider the field to be optional because it's OK
			// to omit it. Otherwise it'll be required.
			if err := iter.Value().Validate(cue.Concrete(true)); err != nil {
				props.required = append(props.required, sel.Unquoted())
			}
		}
		props.elems = append(props.elems, property{
			name: sel.Unquoted(),
			item: g.makeItem(iter.Value()),
		})
	}
	if len(props.elems) == 0 && len(props.required) == 0 {
		return &itemTrue{}
	}
	return &props
}

// cueKindToJSONSchemaTypes converts a CUE kind to JSON Schema type strings
func cueKindToJSONSchemaTypes(kind cue.Kind) []string {
	types := make([]string, 0, kind.Count())
	if (kind & cue.NumberKind) == cue.FloatKind {
		kind &^= cue.NumberKind
		types = append(types, "number")
	}

	for k := range kind.AllKinds() {
		var t string
		switch k {
		case cue.NullKind:
			t = "null"
		case cue.BoolKind:
			t = "boolean"
		case cue.StringKind:
			t = "string"
		case cue.IntKind:
			t = "integer"
		case cue.StructKind:
			t = "object"
		case cue.ListKind:
			t = "array"
		default:
			continue
		}
		types = append(types, t)
	}
	return types
}

// isConcreteScalar reports whether v should be considered concrete
// enough to be encoded as a const or enum value.
//
// Structs and lists are excluded for now to avoid O(n^2)
// overhead when checking.
//
// TODO handle struct and list kinds.
func isConcreteScalar(v cue.Value) bool {
	if !v.IsConcrete() {
		return false
	}
	return (v.Kind() & (cue.StructKind | cue.ListKind)) == 0
}

func mapSlice[T1, T2 any](xs []T1, f func(T1) T2) []T2 {
	xs1 := make([]T2, len(xs))
	for i, x := range xs {
		xs1[i] = f(x)
	}
	return xs1
}

func defaultNameFunc(inst cue.Value, ref cue.Path) string {
	var buf strings.Builder
	for i, sel := range ref.Selectors() {
		if i > 0 {
			buf.WriteByte('.')
		}
		// TODO what should this do when it's not a valid identifier?
		buf.WriteString(sel.String())
	}
	return buf.String()
}

func dedupe[T any](xs []T, eq func(T, T) bool) []T {
	if len(xs) < 2 {
		return xs
	}

	// `w` is the write index for the deduplicated slice
	w := 1
outer:
	for i := 1; i < len(xs); i++ {
		x := xs[i]
		for j := 0; j < w; j++ {
			if eq(x, xs[j]) {
				continue outer
			}
		}
		if w != i {
			xs[w] = x // move unique item into position
		}
		w++
	}

	return xs[:w]
}

func allTrue[T any](xs []T, f func(T) bool) bool {
	for _, x := range xs {
		if !f(x) {
			return false
		}
	}
	return true
}
