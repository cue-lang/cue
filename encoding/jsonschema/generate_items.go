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
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// TODO use a defined order when keywords are marshaled
// so that we always put $schema at the start, for example.

// item represents a JSON Schema constraint or structure that can be
// converted to an AST representation for serialization.
type item interface {
	// generate returns the AST representation of this item.
	generate(g *generator) ast.Expr

	// apply invokes f on each sub-item, replacing each with the
	// item returned, and returns the new item (or the same if nothing has changed).
	// Note that it does not call f on the item itself.
	apply(f func(item) item) item
}

// itemTrue represents a schema that accepts any value (true schema)
type itemTrue struct{}

func (i *itemTrue) generate(g *generator) ast.Expr {
	return ast.NewBool(true)
}

func (i *itemTrue) apply(f func(item) item) item {
	return i
}

// itemFalse represents a schema that accepts no values (false schema)
type itemFalse struct{}

func (i *itemFalse) generate(g *generator) ast.Expr {
	return ast.NewBool(false)
}

func (i *itemFalse) apply(f func(item) item) item {
	return i
}

// itemAllOf represents an allOf combinator
type itemAllOf struct {
	elems []item
}

func (i *itemAllOf) add(it item) {
	i.elems = append(i.elems, it)
}

func (i *itemAllOf) generate(g *generator) ast.Expr {
	// Because a single json schema object is essentially an allOf itself,
	// we can merge objects that don't share keywords
	// but we also have to be careful not to merge keywords
	// that interact with one another (for example `properties` and `patternProperties`).
	var unmerged []ast.Expr
	var finalFields []ast.Decl
	finalFieldNames := make(map[string]bool)

	for _, e := range i.elems {
		expr := e.generate(g)
		if lit, ok := expr.(*ast.BasicLit); ok {
			switch lit.Kind {
			case token.TRUE:
				// true does nothing, so can be ignored.
				continue
			case token.FALSE:
				// false means everything is false.
				return expr
			}
		}

		// Try to extract struct literal fields for merging
		st, ok := expr.(*ast.StructLit)
		if !ok {
			// A schema should only ever encode to a bool or a struct.
			panic(fmt.Errorf("unexpected expression in itemAllOf: %T", expr))
		}

		// Check if we can merge these fields with existing ones
		avoidMerging := false
	loop:
		for _, decl := range st.Elts {
			name := fieldLabel(decl)
			if name == "" {
				panic(fmt.Errorf("unexpected element in struct %#v", decl))
			}
			if finalFieldNames[name] {
				// Field already exists in merge target.
				avoidMerging = true
				break
			}
			for _, ik := range keywordInteractions[name] {
				if finalFieldNames[ik] {
					// Field interacts with one of the other fields in merge target.
					avoidMerging = true
					break loop
				}
			}
		}

		if avoidMerging {
			unmerged = append(unmerged, expr)
			continue
		}
		// Merge the fields
		for _, decl := range st.Elts {
			finalFieldNames[fieldLabel(decl)] = true
			finalFields = append(finalFields, decl)
		}
	}

	if len(unmerged) == 0 {
		return makeSchemaStructLit(finalFields...)
	}

	// Add the merged fields as one element if non-empty
	if len(finalFields) > 0 {
		unmerged = append(unmerged, makeSchemaStructLit(finalFields...))
	}

	return singleKeyword("allOf", ast.NewList(unmerged...))
}

func (i *itemAllOf) apply(f func(item) item) item {
	elems, changed := applyElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemAllOf{elems: elems}
}

// itemOneOf represents a oneOf combinator
type itemOneOf struct {
	elems []item
}

func (i *itemOneOf) generate(g *generator) ast.Expr {
	return singleKeyword("oneOf", generateList(g, i.elems))
}

func (i *itemOneOf) apply(f func(item) item) item {
	elems, changed := applyElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemOneOf{elems: elems}
}

// itemAnyOf represents an anyOf combinator
type itemAnyOf struct {
	elems []item
}

func (i *itemAnyOf) generate(g *generator) ast.Expr {
	return singleKeyword("anyOf", generateList(g, i.elems))
}

func (i *itemAnyOf) apply(f func(item) item) item {
	elems, changed := applyElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemAnyOf{elems: elems}
}

// itemNot represents a not combinator
type itemNot struct {
	elem item
}

func (i *itemNot) generate(g *generator) ast.Expr {
	return singleKeyword("not", i.elem.generate(g))
}

func (i *itemNot) apply(f func(item) item) item {
	elem := f(i.elem)
	if elem == i.elem {
		return i
	}
	return &itemNot{elem: elem}
}

// itemConst represents a constant value constraint.
// The value represents the actual constant in question as an AST expression.
type itemConst struct {
	value ast.Expr
}

func (i *itemConst) generate(g *generator) ast.Expr {
	return singleKeyword("const", i.value)
}

func (i *itemConst) apply(f func(item) item) item {
	return i
}

// itemEnum represents an "enum" constraint.
// Each value represents one possible value of the enum.
type itemEnum struct {
	values []ast.Expr
}

func (i *itemEnum) generate(g *generator) ast.Expr {
	return singleKeyword("enum", ast.NewList(i.values...))
}

func (i *itemEnum) apply(f func(item) item) item {
	return i
}

type itemRef struct {
	defName string
}

func (i *itemRef) generate(g *generator) ast.Expr {
	return singleKeyword("$ref", ast.NewString("#/$defs/"+i.defName))
}

func (i *itemRef) apply(f func(item) item) item {
	return i
}

// itemType represents a type constraint
type itemType struct {
	kinds []string
}

func (i *itemType) generate(g *generator) ast.Expr {
	if len(i.kinds) == 1 {
		return singleKeyword("type", ast.NewString(i.kinds[0]))
	}
	exprs := make([]ast.Expr, len(i.kinds))
	for i, k := range i.kinds {
		exprs[i] = ast.NewString(k)
	}
	return singleKeyword("type", ast.NewList(exprs...))
}

func (i *itemType) apply(f func(item) item) item {
	return i
}

// itemFormat represents a format constraint
type itemFormat struct {
	format string
}

func (i *itemFormat) generate(g *generator) ast.Expr {
	return singleKeyword("format", ast.NewString(i.format))
}

func (i *itemFormat) apply(f func(item) item) item {
	return i
}

// itemPattern represents a pattern constraint
type itemPattern struct {
	regexp string
}

func (i *itemPattern) generate(g *generator) ast.Expr {
	return singleKeyword("pattern", ast.NewString(i.regexp))
}

func (i *itemPattern) apply(f func(item) item) item {
	return i
}

// itemBounds represents numeric bounds constraints
type itemBounds struct {
	constraint cue.Op // LessThanEqualOp, LessThanOp, GreaterThanEqualOp, GreaterThanOp
	// TODO this encodes awkwardly in CUE (for example 10 becomes 1e0). It
	// would be good to fix that.
	n float64
}

func (i *itemBounds) generate(g *generator) ast.Expr {
	var keyword string
	switch i.constraint {
	case cue.LessThanOp:
		keyword = "exclusiveMaximum"
	case cue.LessThanEqualOp:
		keyword = "maximum"
	case cue.GreaterThanOp:
		keyword = "exclusiveMinimum"
	case cue.GreaterThanEqualOp:
		keyword = "minimum"
	default:
		panic(fmt.Errorf("unexpected bound operand %v", i.constraint))
	}
	return singleKeyword(keyword, ast.NewLit(token.FLOAT, fmt.Sprint(i.n)))
}

func (i *itemBounds) apply(f func(item) item) item {
	return i
}

// itemMultipleOf represents a multipleOf constraint
type itemMultipleOf struct {
	n float64
}

func (i *itemMultipleOf) generate(g *generator) ast.Expr {
	return singleKeyword("multipleOf", ast.NewLit(token.FLOAT, fmt.Sprint(i.n)))
}

func (i *itemMultipleOf) apply(f func(item) item) item {
	return i
}

// itemLengthBounds represents string length constraints
type itemLengthBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (i *itemLengthBounds) generate(g *generator) ast.Expr {
	var keyword string
	switch i.constraint {
	case cue.LessThanEqualOp:
		keyword = "maxLength"
	case cue.GreaterThanEqualOp:
		keyword = "minLength"
	default:
		panic("unexpected constraint in length bounds")
	}

	return singleKeyword(keyword, ast.NewLit(token.INT, fmt.Sprint(i.n)))
}

func (i *itemLengthBounds) apply(f func(item) item) item {
	return i
}

// itemItemsBounds represents array length constraints
type itemItemsBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (i *itemItemsBounds) generate(g *generator) ast.Expr {
	var keyword string
	switch i.constraint {
	case cue.LessThanEqualOp:
		keyword = "maxItems"
	case cue.GreaterThanEqualOp:
		keyword = "minItems"
	default:
		panic("unexpected constraint in items bounds")
	}
	return singleKeyword(keyword, ast.NewLit(token.INT, fmt.Sprint(i.n)))
}

func (i *itemItemsBounds) apply(f func(item) item) item {
	return i
}

// itemPropertyBounds represents object property count constraints
type itemPropertyBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (i *itemPropertyBounds) generate(g *generator) ast.Expr {
	var keyword string
	switch i.constraint {
	case cue.LessThanEqualOp:
		keyword = "maxProperties"
	case cue.GreaterThanEqualOp:
		keyword = "minProperties"
	default:
		panic("unexpected constraint in items bounds")
	}
	return singleKeyword(keyword, ast.NewLit(token.INT, fmt.Sprint(i.n)))
}

func (i *itemPropertyBounds) apply(f func(item) item) item {
	return i
}

// itemPrefixItems represents prefixItems constraint for arrays
type itemItems struct {
	prefix []item
	rest   item
}

func (i *itemItems) generate(g *generator) ast.Expr {
	fields := make([]ast.Decl, 0, 2)
	if len(i.prefix) > 0 {
		items := make([]ast.Expr, len(i.prefix))
		for i, e := range i.prefix {
			items[i] = e.generate(g)
		}
		fields = append(fields, makeField("prefixItems", &ast.ListLit{
			Elts: items,
		}))
	}
	if i.rest != nil {
		fields = append(fields, makeField("items", i.rest.generate(g)))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemItems) apply(f func(item) item) item {
	rest := i.rest
	if rest != nil {
		rest = f(rest)
	}
	prefix, changed := applyElems(i.prefix, f)
	if !changed && rest == i.rest {
		return i
	}
	return &itemItems{prefix: prefix, rest: rest}
}

// itemContains represents a contains constraint for arrays
type itemContains struct {
	elem item
	min  *int
	max  *int
}

func (i *itemContains) generate(g *generator) ast.Expr {
	fields := []ast.Decl{makeField("contains", i.elem.generate(g))}
	if i.min != nil {
		fields = append(fields, makeField("minContains", ast.NewLit(token.INT, fmt.Sprint(*i.min))))
	}
	if i.max != nil {
		fields = append(fields, makeField("maxContains", ast.NewLit(token.INT, fmt.Sprint(*i.max))))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemContains) apply(f func(item) item) item {
	elem := f(i.elem)
	if elem == i.elem {
		return i
	}
	return &itemContains{elem: elem, min: i.min, max: i.max}
}

// property represents an object property
type property struct {
	name string
	item item
}

// itemProperties represents object properties and associated keywords.
type itemProperties struct {
	elems    []property
	required []string
	// TODO patternProperties, additionalProperties
}

func (i *itemProperties) generate(g *generator) ast.Expr {
	propFields := make([]ast.Decl, len(i.elems))
	for j, prop := range i.elems {
		propFields[j] = makeField(prop.name, prop.item.generate(g))
	}
	fields := []ast.Decl{makeField("properties", &ast.StructLit{Elts: propFields})}
	if len(i.required) > 0 {
		reqExprs := make([]ast.Expr, len(i.required))
		for j, r := range i.required {
			reqExprs[j] = ast.NewString(r)
		}
		fields = append(fields, makeField("required", ast.NewList(reqExprs...)))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemProperties) apply(f func(item) item) item {
	changed := false
	elems := i.elems
	for j, prop := range elems {
		if it := f(prop.item); it != prop.item {
			if !changed {
				elems = slices.Clone(elems)
				changed = true
			}
			elems[j] = property{
				name: prop.name,
				item: it,
			}
		}
	}
	if !changed {
		return i
	}
	return &itemProperties{
		elems:    elems,
		required: i.required,
	}
}

func applyElems(elems []item, f func(item) item) ([]item, bool) {
	changed := false
	for i, e := range elems {
		e1 := f(e)
		if e1 == e {
			continue
		}
		if !changed {
			elems = slices.Clone(elems)
			changed = true
		}
		elems[i] = e1
	}
	return elems, changed
}

// itemIfThenElse represents if/then/else constraints
type itemIfThenElse struct {
	ifElem   item
	thenElem item
	elseElem item
}

func (i *itemIfThenElse) generate(g *generator) ast.Expr {
	fields := []ast.Decl{makeField("if", i.ifElem.generate(g))}
	if i.thenElem != nil {
		fields = append(fields, makeField("then", i.thenElem.generate(g)))
	}
	if i.elseElem != nil {
		fields = append(fields, makeField("else", i.elseElem.generate(g)))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemIfThenElse) apply(f func(item) item) item {
	ifElem := f(i.ifElem)
	var thenElem, elseElem item
	if i.thenElem != nil {
		thenElem = f(i.thenElem)
	}
	if i.elseElem != nil {
		elseElem = f(i.elseElem)
	}

	if ifElem == i.ifElem && thenElem == i.thenElem && elseElem == i.elseElem {
		return i
	}
	return &itemIfThenElse{ifElem: ifElem, thenElem: thenElem, elseElem: elseElem}
}

func generateList(g *generator, items []item) ast.Expr {
	exprs := make([]ast.Expr, len(items))
	for i, it := range items {
		exprs[i] = it.generate(g)
	}
	return ast.NewList(exprs...)
}

func singleKeyword(name string, val ast.Expr) ast.Expr {
	return makeSchemaStructLit(makeField(name, val))
}

// keywordGroups holds sets of JSON Schema keywords that
// interact directly with one another and therefore should not
// be merged with other keywords in the same group.
var keywordGroups = [][]string{
	{"properties", "patternProperties", "additionalProperties"},
	{"contains", "maxContains", "minContains"},
	{"items", "additionalItems", "prefixItems"},
	{"if", "then", "else"},
}

// keywordInteractions maps from a keyword to the set of
// keywords it interacts with (including itself).
var keywordInteractions = func() map[string][]string {
	m := make(map[string][]string)
	for _, ks := range keywordGroups {
		for _, k := range ks {
			m[k] = ks
		}
	}
	return m
}()

// fieldLabel extracts the field label name from a declaration.
func fieldLabel(d ast.Decl) string {
	if f, ok := d.(*ast.Field); ok {
		if name, _, _ := ast.LabelName(f.Label); name != "" {
			return name
		}
	}
	return ""
}

// makeField creates a field with a string label and given value.
func makeField(name string, value ast.Expr) *ast.Field {
	return &ast.Field{
		Label: ast.NewStringLabel(name),
		Value: value,
	}
}

// makeSchemaStructLit creates a struct literal representing a JSON Schema
// schema, with fields in schema-centric order.
func makeSchemaStructLit(fields ...ast.Decl) *ast.StructLit {
	slices.SortFunc(fields, func(a, b ast.Decl) int {
		return cmpSchemaLabels(fieldLabel(a), fieldLabel(b))
	})
	return &ast.StructLit{
		Elts: fields,
	}
}

func cmpSchemaLabels(l1, l2 string) int {
	return cmp.Or(cmp.Compare(labelPriority(l1), labelPriority(l2)), cmp.Compare(l1, l2))
}

// labelPriorityValues holds priority groups for sorting label names.
var labelPriorityValues = func() map[string]int {
	// Always put these keywords at the start.
	m := map[string]int{
		"$schema": 0,
		"$defs":   1,
		"type":    2,
	}
	// It's nice to group related keywords together.
	n := len(m)
	for i, g := range keywordGroups {
		for _, name := range g {
			m[name] = n + i + 1
		}
	}
	// Anything else gets put at the end in lexical order.
	return m
}()

func labelPriority(s string) int {
	if pri, ok := labelPriorityValues[s]; ok {
		return pri
	}
	return 1000
}
