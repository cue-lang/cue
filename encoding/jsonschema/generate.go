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
	"cmp"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// GenerateConfig configures JSON Schema generation from CUE values.
type GenerateConfig struct {
	// Version specifies the version of JSON Schema to generate.
	// Currently only [VersionDraft2020_12] is supported.
	Version Version

	// NameFunc is used to determine how references map to
	// JSON Schema definition names. It is passed the
	// root value (usually a package) and the path to that value
	// within it, as returned by [cue.Value.ReferencePath].
	//
	// If this is nil, [DefaultNameFunc] will be used.
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
		cfg.NameFunc = DefaultNameFunc
	}
	if cfg.Version == VersionUnknown {
		cfg.Version = VersionDraft2020_12
	}
	if cfg.Version != VersionDraft2020_12 {
		return nil, fmt.Errorf("only version %v is supported for generating JSON Schema for now", VersionDraft2020_12)
	}

	g := &generator{
		cfg:  cfg,
		defs: make(map[string]item),
	}
	item := g.makeItem(v)
	item = mergeAllOf(item)
	item = enumFromConst(item)
	expr := item.generate(g)

	// Check if the result is a boolean literal
	if lit, ok := expr.(*ast.BasicLit); ok && (lit.Kind == token.TRUE || lit.Kind == token.FALSE) {
		if lit.Kind == token.FALSE {
			// There should already be an error; if not, create one
			if g.err == nil {
				g.addError(v, fmt.Errorf("schema cannot be satisfied"))
			}
			return nil, g.err
		}
		// true means empty struct
		expr = &ast.StructLit{}
	}

	// The result should be a struct literal
	st, ok := expr.(*ast.StructLit)
	if !ok {
		return nil, fmt.Errorf("expected struct literal from generate, got %T", expr)
	}

	// Add schema version metadata and definitions.
	fields := []ast.Decl{makeField("$schema", ast.NewString(cfg.Version.String()))}
	if len(g.defs) != 0 {
		defFields := make([]ast.Decl, 0, len(g.defs))
		for _, name := range slices.Sorted(maps.Keys(g.defs)) {
			defFields = append(defFields, makeField(name, g.defs[name].generate(g)))
		}
		fields = append(fields, makeField("$defs", &ast.StructLit{Elts: defFields}))
	}
	fields = append(fields, st.Elts...)

	if g.err != nil {
		return nil, g.err
	}
	return makeSchemaStructLit(fields...), nil
}

// mergeAllOf returns the item with adjacent itemAllOf nodes
// all merged into a single itemAllOf node with all
// the conjuncts in.
func mergeAllOf(it item) item {
	switch it := it.(type) {
	case *itemAllOf:
		it1 := &itemAllOf{
			elems: make([]item, 0, len(it.elems)),
		}
	loop:
		for e := range siblings(it) {
			// Remove elements that are entirely redundant.
			// Note: DeepEqual seems reasonable here because values are generally
			// small and the data structures are well-defined. We could
			// reconsider if these assumptions change.
			// TODO we could unify itemType elements here, for example:
			// allOf(itemType(number), itemType(integer)) -> itemType(integer)
			for _, e1 := range it1.elems {
				if reflect.DeepEqual(e1, e) {
					continue loop
				}
			}
			it1.elems = append(it1.elems, e.apply(mergeAllOf))
		}
		if len(it1.elems) == 1 {
			return it1.elems[0]
		}
		return it1
	default:
		return it.apply(mergeAllOf)
	}
}

type elementsItem interface {
	elements() []item
}

func siblings[T elementsItem](it T) iter.Seq[item] {
	return func(yield func(item) bool) {
		yieldSiblings(it, yield)
	}
}

func yieldSiblings[T elementsItem](it T, yield func(item) bool) bool {
	for _, e := range it.elements() {
		if ae, ok := e.(T); ok {
			if !yieldSiblings(ae, yield) {
				return false
			}
		} else {
			if !yield(e) {
				return false
			}
		}
	}
	return true
}

// enumFromConst returns the item with disjunctive
// constants replaced by itemEnum.
// For example:
//
//	anyOf(const("a"), const("b"), const("c"))
//	->
//	enum("a", "b", "c")
func enumFromConst(it item) item {
	switch it := it.(type) {
	case *itemAnyOf:
		if slices.ContainsFunc(it.elems, func(it item) bool {
			_, ok := it.(*itemConst)
			return !ok
		}) {
			// They're not all consts, so return as-is.
			return it
		}
		// All items are const. We can make an enum from this.
		// TODO this doesn't cover cases where there are some
		// const values and some noncrete values.
		it1 := &itemEnum{
			values: make([]ast.Expr, 0, len(it.elems)),
		}
		for _, e := range it.elems {
			it1.values = append(it1.values, e.(*itemConst).value)
		}
		return it1
	default:
		return it.apply(enumFromConst)
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

func (g *generator) addErrorf(pos cue.Value, f string, a ...any) {
	g.addError(pos, fmt.Errorf(f, a...))
}

// makeItem returns an item representing the JSON Schema
// for v in naive form.
func (g *generator) makeItem(v cue.Value) item {
	op, args := v.Expr()
	switch op {
	case cue.NoOp, cue.SelectorOp:
		pkg, path := v.ReferencePath()
		if !pkg.Exists() {
			break
		}
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
		if !args[0].IsConcrete() {
			// If it's not concrete, we can't represent it in JSON Schema
			// so accept anything.
			return &itemTrue{}
		}
		syntax := args[0].Syntax()
		expr, ok := syntax.(ast.Expr)
		if !ok {
			g.addError(args[0], fmt.Errorf("expected expression from Syntax, got %T", syntax))
			return &itemFalse{}
		}
		it := &itemConst{
			value: expr,
		}
		if op == cue.EqualOp {
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
		switch kind := args[0].Kind(); kind {
		case cue.FloatKind, cue.IntKind:
			n, err := args[0].Float64()
			if err != nil {
				// Probably non-concrete.
				return &itemTrue{}
			}
			return &itemAllOf{
				elems: []item{
					&itemBounds{
						constraint: op,
						n:          n,
					},
					&itemType{
						kinds: []string{"number"},
					},
				},
			}
		case cue.StringKind:
			// Can't express bounds on strings in JSON Schema
			return &itemType{
				kinds: cueKindToJSONSchemaTypes(kind),
			}
		default:
			g.addError(args[0], fmt.Errorf("bad argument to unary comparison"))
			return &itemFalse{}
		}
	case cue.CallOp:
		return g.makeCallItem(v, args)
	}
	if isConcreteScalar(v) && !v.IsNull() {
		if err := v.Err(); err != nil {
			g.addError(v, fmt.Errorf("error found in schema: %v", err))
			return &itemFalse{}
		}
		syntax := v.Syntax()
		expr, ok := syntax.(ast.Expr)
		if !ok {
			g.addError(v, fmt.Errorf("expected expression from Syntax, got %T", syntax))
			return &itemFalse{}
		}
		return &itemConst{
			value: expr,
		}
	}
	kind := v.IncompleteKind()
	if kind == cue.TopKind {
		return &itemTrue{}
	}
	var it item // additional constraints for some known types.
	switch kind {
	case cue.StructKind:
		it = g.makeStructItem(v)
	case cue.ListKind:
		it = g.makeListItem(v)
	}
	var elems []item
	if kinds := cueKindToJSONSchemaTypes(kind); len(kinds) > 0 {
		elems = append(elems, &itemType{
			kinds: kinds,
		})
	}
	if it != nil {
		elems = append(elems, it)
	}
	switch len(elems) {
	case 0:
		return &itemTrue{}
	case 1:
		return elems[0]
	}
	return &itemAllOf{
		elems: elems,
	}
}

func (g *generator) makeCallItem(v cue.Value, args []cue.Value) item {
	if len(args) < 1 {
		// Invalid call - not enough arguments
		g.addError(v, fmt.Errorf("call operation with no function"))
		return &itemFalse{}
	}

	// Get the function name from the first argument.
	// TODO this might need rethinking if/when functions become more of
	// a first class thing within CUE.
	funcName := fmt.Sprint(args[0])

	switch funcName {
	case "error()", "error":
		// Explicit error: don't add an error to g but map it to a `false` schema.
		// See https://github.com/cue-lang/cue/issues/4133 for why
		// we include "error()" as well as "error"
		return &itemFalse{}
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
			// TODO should we just fall back to type=string if we
			// can't determine the concrete format?
			g.addError(args[1], err)
			return &itemFalse{}
		}
		// Convert CUE time layout to JSON Schema format
		var format string
		switch layout {
		case time.RFC3339, time.RFC3339Nano:
			format = "date-time"
		case time.DateOnly:
			format = "date"
		case time.TimeOnly:
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
	case "list.MinItems", "list.MaxItems":
		if len(args) != 2 {
			g.addError(v, fmt.Errorf("%s expects 1 argument, got %d", funcName, len(args)-1))
			return &itemFalse{}
		}
		n, err := args[1].Int64()
		if err != nil {
			g.addError(args[1], err)
			return &itemFalse{}
		}
		var constraint cue.Op
		if funcName == "list.MinItems" {
			constraint = cue.GreaterThanEqualOp
		} else {
			constraint = cue.LessThanEqualOp
		}
		return &itemAllOf{
			elems: []item{
				&itemType{kinds: []string{"array"}},
				&itemItemsBounds{constraint: constraint, n: int(n)},
			},
		}

	case "list.MatchN":
		// list.MatchN is generated by Extract for the contains keyword.
		// - list.MatchN(>=N, schema) represents contains with minContains: N
		// - list.MatchN(>=N & <=M, schema) represents contains with minContains: N and maxContains: M
		if len(args) != 3 {
			// Unrecognized form, accept anything
			return &itemTrue{}
		}

		// Parse the constraint from the first argument
		constraintVal := args[1]
		var minVal, maxVal *int

		op, opArgs := constraintVal.Expr()
		switch op {
		case cue.NoOp:
			// It's a simple expression, could be a literal or something more complex
			// Try to parse as an int literal for the minimum
			n, err := constraintVal.Int64()
			if err == nil {
				min := int(n)
				minVal = &min
			} else {
				// Not a simple integer, accept anything
				return &itemTrue{}
			}
		case cue.GreaterThanEqualOp:
			// >=N constraint for minimum
			if len(opArgs) != 1 {
				return &itemTrue{}
			}
			n, err := opArgs[0].Int64()
			if err != nil {
				return &itemTrue{}
			}
			min := int(n)
			minVal = &min
		case cue.AndOp:
			// Could be >=N & <=M
			if len(opArgs) != 2 {
				return &itemTrue{}
			}
			// First operand should be >=N
			op1, op1Args := opArgs[0].Expr()
			if op1 != cue.GreaterThanEqualOp || len(op1Args) != 1 {
				return &itemTrue{}
			}
			n, err := op1Args[0].Int64()
			if err != nil {
				return &itemTrue{}
			}
			min := int(n)
			minVal = &min

			// Second operand should be <=M
			op2, op2Args := opArgs[1].Expr()
			if op2 != cue.LessThanEqualOp || len(op2Args) != 1 {
				return &itemTrue{}
			}
			m, err := op2Args[0].Int64()
			if err != nil {
				return &itemTrue{}
			}
			max := int(m)
			maxVal = &max
		default:
			// Unknown constraint pattern, accept anything
			return &itemTrue{}
		}

		// Get the schema element from the second argument
		// Check if it's bottom first (which represents "contains: false")
		// to avoid adding errors to the generator.
		var elem item
		elemVal := args[2]
		if err := elemVal.Err(); err != nil {
			// Bottom value - represents "contains: false"
			elem = &itemFalse{}
		} else {
			elem = g.makeItem(elemVal)
		}

		return &itemAllOf{
			elems: []item{
				&itemType{kinds: []string{"array"}},
				&itemContains{elem: elem, min: minVal, max: maxVal},
			},
		}

	case "matchN":
		// matchN is generated by Extract for oneOf, anyOf, allOf, and not.
		// - matchN(1, [a, b, c, ...]) represents oneOf
		// - matchN(0, [x]) represents not
		// - matchN(>=1, [a, b, c, ...]) represents anyOf
		// - matchN(N, [a, b, c, ...]) where N == len(list) represents allOf
		if len(args) != 3 {
			// Unrecognized form, accept anything
			return &itemTrue{}
		}

		constraintVal, listVal := args[1], args[2]

		var items []item
		for i := 0; ; i++ {
			// Unfortunately https://github.com/cue-lang/cue/issues/4132 means
			// that we cannot iterate over elements of the list with listVal.List
			// when there are error elements (which there could be, as [Extract]
			// can generate explicit errors, but we _can_ use [Value.LookupPath]
			// to look up explicit indexes.
			v := listVal.LookupPath(cue.MakePath(cue.Index(i)))
			if !v.Exists() {
				break
			}
			items = append(items, g.makeItem(v))
		}
		// Extract the list of items from the second argument.

		// Determine which combinator to use based on the constraint
		// It can be a literal int (0, 1, N) or a unary expression (>=1).
		op, opArgs := constraintVal.Expr()
		switch op {
		case cue.NoOp:
			// It's a simple integer literal
			n, err := constraintVal.Int64()
			if err != nil {
				// Not an integer, accept anything
				return &itemTrue{}
			}
			switch n {
			case 0:
				// matchN(0, [x]) represents not
				if len(items) != 1 {
					// Unexpected form, accept anything
					return &itemTrue{}
				}
				return &itemNot{elem: items[0]}
			case 1:
				if len(items) == 0 {
					return &itemFalse{}
				}
				// matchN(1, [a, b, c, ...]) represents oneOf
				return &itemOneOf{elems: items}
			default:
				// matchN(N, [...]) where N == len(list) represents allOf
				if int(n) == len(items) {
					return &itemAllOf{elems: items}
				}
				// Unknown matchN pattern, accept anything
				return &itemTrue{}
			}

		case cue.GreaterThanEqualOp:
			// matchN(>=1, [a, b, c, ...]) represents anyOf
			if len(opArgs) != 1 {
				return &itemTrue{}
			}
			n, err := opArgs[0].Int64()
			if err != nil || n != 1 {
				// Unknown matchN pattern, accept anything
				return &itemTrue{}
			}
			if len(items) == 0 {
				return &itemFalse{}
			}
			return &itemAnyOf{elems: items}

		default:
			// Unknown operator, accept anything
			return &itemTrue{}
		}

	case "matchIf":
		// matchIf is generated by Extract for if/then/else constraints.
		// - matchIf(ifExpr, thenExpr, elseExpr)
		if len(args) != 4 {
			// Unrecognized form, accept anything
			return &itemTrue{}
		}

		return &itemIfThenElse{
			ifElem:   g.makeItem(args[1]),
			thenElem: trueAsNil(g.makeItem(args[2])),
			elseElem: trueAsNil(g.makeItem(args[3])),
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
	slices.SortFunc(props.elems, func(e1, e2 property) int {
		return cmp.Compare(e1.name, e2.name)
	})
	if len(props.elems) == 0 && len(props.required) == 0 {
		return &itemTrue{}
	}
	return &props
}

func (g *generator) makeListItem(v cue.Value) item {
	ellipsis := v.LookupPath(cue.MakePath(cue.AnyIndex))
	lenv := v.Len()
	var n int64
	if ellipsis.Exists() {
		// It's an open list. The length will be in the form int&>=5
		op, args := lenv.Expr()
		if op != cue.AndOp || len(args) != 2 {
			g.addErrorf(v, "list length has unexpected form; got %v want int&>=N", lenv)
			return &itemFalse{}
		}
		op, args = args[1].Expr()
		if op != cue.GreaterThanEqualOp || len(args) != 1 {
			g.addErrorf(v, "list length has unexpected form (2); got %v want >=N", lenv)
			return &itemFalse{}
		}
		var err error
		n, err = args[0].Int64()
		if err != nil {
			g.addErrorf(v, "cannot extract list length from %v: %v", v, err)
			return &itemFalse{}
		}
	} else {
		var err error
		n, err = lenv.Int64()
		if err != nil {
			g.addErrorf(v, "cannot extract concrete list length from %v: %v", v, err)
		}
	}
	prefix := make([]item, n)
	for i := range n {
		elem := v.LookupPath(cue.MakePath(cue.Index(i)))
		if !elem.Exists() {
			g.addErrorf(v, "cannot get value at index %d in %v", i, v)
			return &itemFalse{}
		}
		prefix[i] = g.makeItem(elem)
	}
	a := &itemAllOf{
		elems: []item{&itemType{kinds: []string{"array"}}},
	}
	items := &itemItems{}
	if len(prefix) > 0 {
		a.elems = append(a.elems, &itemLengthBounds{
			constraint: cue.GreaterThanEqualOp,
			n:          len(prefix),
		})
		items.prefix = prefix
	}
	if ellipsis.Exists() {
		items.rest = trueAsNil(g.makeItem(ellipsis))
	} else {
		a.elems = append(a.elems, &itemLengthBounds{
			constraint: cue.LessThanEqualOp,
			n:          len(prefix),
		})
	}
	if items.rest != nil || len(items.prefix) > 0 {
		a.elems = append(a.elems, items)
	}
	return a
}

// cueKindToJSONSchemaTypes converts a CUE kind to JSON Schema type strings
// as associated with the "type" keyword.
func cueKindToJSONSchemaTypes(kind cue.Kind) []string {
	types := make([]string, 0, kind.Count())
	if (kind & cue.FloatKind) != 0 {
		// JSON Schema doesn't distinguish between float and number,
		// so any float allows all numbers (CUE models "number" as float|int).
		kind &^= cue.NumberKind
		types = append(types, "number")
	}

	for k := range kind.Kinds() {
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

// trueAsNil returns the nil item if the item
// is *itemTrue (top).
func trueAsNil(it item) item {
	if _, ok := it.(*itemTrue); ok {
		return nil
	}
	return it
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

// DefaultNameFunc holds the default function used by [Generate]
// to generate a JSON Schema definition name from a reference path
// within the value inst, where inst is usually a CUE package value.
func DefaultNameFunc(inst cue.Value, ref cue.Path) string {
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

// mapSlice returns a slice of f(x) for each x in xs.
func mapSlice[T1, T2 any](xs []T1, f func(T1) T2) []T2 {
	xs1 := make([]T2, len(xs))
	for i, x := range xs {
		xs1[i] = f(x)
	}
	return xs1
}
