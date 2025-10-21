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
	"hash/maphash"
	"maps"
	"reflect"
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/anyunique"
)

// TODO use a defined order when keywords are marshaled
// so that we always put $schema at the start, for example.

// item represents a JSON Schema constraint or structure that can be
// converted to an AST representation for serialization.
type item interface {
	// generate returns the AST representation of this item.
	generate(g *generator) ast.Expr

	// apply invokes f on each sub-item, replacing each with the item
	// returned, and returns the new item (or the same if nothing has
	// changed). Note that it does not call f on the item itself. It can
	// use u to create new unique items.
	apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item

	// hash writes the hash of the item to h; it should use u.writeHash
	// to write the hash value for any items it contains.
	hash(h *maphash.Hash, u *uniqueItems)
}

// itemTrue represents a schema that accepts any value (true schema)
type itemTrue struct{}

func (i *itemTrue) generate(g *generator) ast.Expr {
	return ast.NewBool(true)
}

func (it *itemTrue) hash(h *maphash.Hash, u *uniqueItems) {
}

func (i *itemTrue) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemFalse represents a schema that accepts no values (false schema)
type itemFalse struct{}

func (i *itemFalse) generate(g *generator) ast.Expr {
	return ast.NewBool(false)
}

func (it *itemFalse) hash(h *maphash.Hash, u *uniqueItems) {
}

func (i *itemFalse) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemAllOf represents an allOf combinator
type itemAllOf struct {
	elems []internItem
}

func (it *itemAllOf) hash(h *maphash.Hash, u *uniqueItems) {
	for _, it := range it.elems {
		u.writeHash(h, it)
	}
}

func (i *itemAllOf) add(it internItem) {
	i.elems = append(i.elems, it)
}

var _ elementsItem = (*itemAllOf)(nil)

// elements implements [elementsItem].
func (i *itemAllOf) elements() []internItem {
	return i.elems
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
		expr := e.Value().generate(g)
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

func (i *itemAllOf) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	elems, changed := applyElems(i.elems, f, u)
	if !changed {
		return i
	}
	return &itemAllOf{elems: elems}
}

// itemOneOf represents a oneOf combinator
type itemOneOf struct {
	elems []internItem
}

func (it *itemOneOf) hash(h *maphash.Hash, u *uniqueItems) {
	for _, it := range it.elems {
		u.writeHash(h, it)
	}
}

func (i *itemOneOf) generate(g *generator) ast.Expr {
	return singleKeyword("oneOf", generateList(g, i.elems))
}

func (i *itemOneOf) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	elems, changed := applyElems(i.elems, f, u)
	if !changed {
		return i
	}
	return &itemOneOf{elems: elems}
}

var _ elementsItem = (*itemOneOf)(nil)

// elements implements [elementsItem].
func (i *itemOneOf) elements() []internItem {
	return i.elems
}

// itemAnyOf represents an anyOf combinator
type itemAnyOf struct {
	elems []internItem
}

func (it *itemAnyOf) hash(h *maphash.Hash, u *uniqueItems) {
	for _, it := range it.elems {
		u.writeHash(h, it)
	}
}

func (i *itemAnyOf) generate(g *generator) ast.Expr {
	return singleKeyword("anyOf", generateList(g, i.elems))
}

func (i *itemAnyOf) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	elems, changed := applyElems(i.elems, f, u)
	if !changed {
		return i
	}
	return &itemAnyOf{elems: elems}
}

var _ elementsItem = (*itemAnyOf)(nil)

// elements implements [elementsItem].
func (i *itemAnyOf) elements() []internItem {
	return i.elems
}

// itemNot represents a not combinator
type itemNot struct {
	elem internItem
}

func (it *itemNot) hash(h *maphash.Hash, u *uniqueItems) {
	u.writeHash(h, it.elem)
}

func (i *itemNot) generate(g *generator) ast.Expr {
	return singleKeyword("not", i.elem.Value().generate(g))
}

func (i *itemNot) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	elem := f(i.elem, u)
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

func (it *itemConst) hash(h *maphash.Hash, u *uniqueItems) {
	writeExprHash(h, it.value)
}

func (i *itemConst) generate(g *generator) ast.Expr {
	return singleKeyword("const", i.value)
}

func (i *itemConst) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemEnum represents an "enum" constraint.
// Each value represents one possible value of the enum.
type itemEnum struct {
	values []ast.Expr
}

func (it *itemEnum) hash(h *maphash.Hash, u *uniqueItems) {
	for _, v := range it.values {
		writeExprHash(h, v)
	}
}

func (i *itemEnum) generate(g *generator) ast.Expr {
	return singleKeyword("enum", ast.NewList(i.values...))
}

func (i *itemEnum) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

type itemRef struct {
	defName string
}

func (it *itemRef) hash(h *maphash.Hash, u *uniqueItems) {
	h.WriteString(it.defName)
}

func (i *itemRef) generate(g *generator) ast.Expr {
	return singleKeyword("$ref", ast.NewString("#/$defs/"+i.defName))
}

func (i *itemRef) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemType represents a type constraint
type itemType struct {
	kinds []string
}

func (it *itemType) hash(h *maphash.Hash, u *uniqueItems) {
	for _, k := range it.kinds {
		h.WriteString(k)
	}
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

func (i *itemType) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemFormat represents a format constraint
type itemFormat struct {
	format string
}

func (it *itemFormat) hash(h *maphash.Hash, u *uniqueItems) {
	h.WriteString(it.format)
}

func (i *itemFormat) generate(g *generator) ast.Expr {
	return singleKeyword("format", ast.NewString(i.format))
}

func (i *itemFormat) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemPattern represents a pattern constraint
type itemPattern struct {
	regexp string
}

func (it *itemPattern) hash(h *maphash.Hash, u *uniqueItems) {
	h.WriteString(it.regexp)
}

func (i *itemPattern) generate(g *generator) ast.Expr {
	return singleKeyword("pattern", ast.NewString(i.regexp))
}

func (i *itemPattern) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemBounds represents numeric bounds constraints
type itemBounds struct {
	constraint cue.Op // LessThanEqualOp, LessThanOp, GreaterThanEqualOp, GreaterThanOp
	// TODO this encodes awkwardly in CUE (for example 10 becomes 1e0). It
	// would be good to fix that.
	n float64
}

func (it *itemBounds) hash(h *maphash.Hash, u *uniqueItems) {
	maphash.WriteComparable(h, it.constraint)
	maphash.WriteComparable(h, it.n)
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

func (i *itemBounds) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemMultipleOf represents a multipleOf constraint
type itemMultipleOf struct {
	n float64
}

func (it *itemMultipleOf) hash(h *maphash.Hash, u *uniqueItems) {
	maphash.WriteComparable(h, it.n)
}

func (i *itemMultipleOf) generate(g *generator) ast.Expr {
	return singleKeyword("multipleOf", ast.NewLit(token.FLOAT, fmt.Sprint(i.n)))
}

func (i *itemMultipleOf) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemLengthBounds represents string length constraints
type itemLengthBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (it *itemLengthBounds) hash(h *maphash.Hash, u *uniqueItems) {
	maphash.WriteComparable(h, it.constraint)
	maphash.WriteComparable(h, it.n)
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

func (i *itemLengthBounds) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemItemsBounds represents array length constraints
type itemItemsBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (it *itemItemsBounds) hash(h *maphash.Hash, u *uniqueItems) {
	maphash.WriteComparable(h, it.constraint)
	maphash.WriteComparable(h, it.n)
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

func (i *itemItemsBounds) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemPropertyBounds represents object property count constraints
type itemPropertyBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (it *itemPropertyBounds) hash(h *maphash.Hash, u *uniqueItems) {
	maphash.WriteComparable(h, it.constraint)
	maphash.WriteComparable(h, it.n)
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

func (i *itemPropertyBounds) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	return i
}

// itemItems represents the items and prefixItems constraint for arrays.
type itemItems struct {
	// known prefix.
	prefix []internItem
	// all elements beyond the prefix.
	rest internItem
}

func (it *itemItems) hash(h *maphash.Hash, u *uniqueItems) {
	for _, p := range it.prefix {
		u.writeHash(h, p)
	}
	u.writeHash(h, it.rest)
}

func (i *itemItems) generate(g *generator) ast.Expr {
	fields := make([]ast.Decl, 0, 2)
	if len(i.prefix) > 0 {
		items := make([]ast.Expr, len(i.prefix))
		for i, e := range i.prefix {
			items[i] = e.Value().generate(g)
		}
		fields = append(fields, makeField("prefixItems", &ast.ListLit{
			Elts: items,
		}))
	}
	if i.rest.Value() != nil {
		fields = append(fields, makeField("items", i.rest.Value().generate(g)))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemItems) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	rest := i.rest
	if rest.Value() != nil {
		rest = f(rest, u)
	}
	prefix, changed := applyElems(i.prefix, f, u)
	if !changed && rest == i.rest {
		return i
	}
	return &itemItems{prefix: prefix, rest: rest}
}

// itemContains represents a contains constraint for arrays
type itemContains struct {
	elem internItem
	min  *int64
	max  *int64
}

func (it *itemContains) hash(h *maphash.Hash, u *uniqueItems) {
	u.writeHash(h, it.elem)
	maphash.WriteComparable(h, it.min == nil)
	if it.min != nil {
		maphash.WriteComparable(h, *it.min)
	}
	maphash.WriteComparable(h, it.max == nil)
	if it.max != nil {
		maphash.WriteComparable(h, *it.max)
	}
}

func (i *itemContains) generate(g *generator) ast.Expr {
	fields := []ast.Decl{makeField("contains", i.elem.Value().generate(g))}
	if i.min != nil {
		fields = append(fields, makeField("minContains", ast.NewLit(token.INT, fmt.Sprint(*i.min))))
	}
	if i.max != nil {
		fields = append(fields, makeField("maxContains", ast.NewLit(token.INT, fmt.Sprint(*i.max))))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemContains) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	elem := f(i.elem, u)
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
	properties           map[string]internItem
	required             []string
	additionalProperties internItem
	patternProperties    map[string]internItem
}

func (it *itemProperties) hash(h *maphash.Hash, u *uniqueItems) {
	writeMapHash(h, it.properties, u)
	for _, name := range slices.Sorted(slices.Values(it.required)) {
		h.WriteString(name)
	}
	u.writeHash(h, it.additionalProperties)
	writeMapHash(h, it.patternProperties, u)
}

func (i *itemProperties) generate(g *generator) ast.Expr {
	fields := []ast.Decl{}
	if len(i.properties) > 0 {
		propFields := make([]ast.Decl, 0, len(i.properties))
		for name, it := range i.properties {
			propFields = append(propFields, makeField(name, it.Value().generate(g)))
		}
		slices.SortFunc(propFields, func(a, b ast.Decl) int {
			return cmp.Compare(fieldLabel(a), fieldLabel(b))
		})
		fields = append(fields, makeField("properties", &ast.StructLit{Elts: propFields}))
	}
	if len(i.required) > 0 {
		reqExprs := make([]ast.Expr, len(i.required))
		for j, r := range i.required {
			reqExprs[j] = ast.NewString(r)
		}
		fields = append(fields, makeField("required", ast.NewList(reqExprs...)))
	}
	if i.additionalProperties.Value() != nil {
		fields = append(fields, makeField("additionalProperties", i.additionalProperties.Value().generate(g)))
	}
	if len(i.patternProperties) > 0 {
		pp := &ast.StructLit{}
		for _, p := range slices.Sorted(maps.Keys(i.patternProperties)) {
			pp.Elts = append(pp.Elts, makeField(p, i.patternProperties[p].Value().generate(g)))
		}
		fields = append(fields, makeField("patternProperties", pp))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemProperties) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	properties, changed0 := applyMap(i.properties, f, u)
	patternProperties, changed1 := applyMap(i.patternProperties, f, u)
	changed := changed0 || changed1
	additionalProperties := i.additionalProperties
	if additionalProperties.Value() != nil {
		if ap := f(additionalProperties, u); ap != additionalProperties {
			additionalProperties = ap
			changed = true
		}
	}
	if !changed {
		return i
	}
	return &itemProperties{
		properties:           properties,
		required:             i.required,
		additionalProperties: additionalProperties,
		patternProperties:    patternProperties,
	}
}

// itemIfThenElse represents if/then/else constraints
type itemIfThenElse struct {
	ifElem   internItem
	thenElem internItem
	elseElem internItem
}

func (it *itemIfThenElse) hash(h *maphash.Hash, u *uniqueItems) {
	u.writeHash(h, it.ifElem)
	u.writeHash(h, it.thenElem)
	u.writeHash(h, it.elseElem)
}

func (i *itemIfThenElse) generate(g *generator) ast.Expr {
	fields := []ast.Decl{makeField("if", i.ifElem.Value().generate(g))}
	if i.thenElem.Value() != nil {
		fields = append(fields, makeField("then", i.thenElem.Value().generate(g)))
	}
	if i.elseElem.Value() != nil {
		fields = append(fields, makeField("else", i.elseElem.Value().generate(g)))
	}
	return makeSchemaStructLit(fields...)
}

func (i *itemIfThenElse) apply(f func(internItem, *uniqueItems) internItem, u *uniqueItems) item {
	ifElem := f(i.ifElem, u)
	var thenElem, elseElem internItem
	if i.thenElem.Value() != nil {
		thenElem = f(i.thenElem, u)
	}
	if i.elseElem.Value() != nil {
		elseElem = f(i.elseElem, u)
	}

	if ifElem == i.ifElem && thenElem == i.thenElem && elseElem == i.elseElem {
		return i
	}
	return &itemIfThenElse{ifElem: ifElem, thenElem: thenElem, elseElem: elseElem}
}

func generateList(g *generator, items []internItem) ast.Expr {
	exprs := make([]ast.Expr, len(items))
	for i, it := range items {
		exprs[i] = it.Value().generate(g)
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

func writeMapHash[K cmp.Ordered](h *maphash.Hash, m map[K]internItem, u *uniqueItems) {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		maphash.WriteComparable(h, k)
		u.writeHash(h, m[k])
	}
}

// writeExprHash hashes an AST expression using its formatted representation.
// This is a simple approach that ensures structurally equivalent expressions
// hash to the same value.
func writeExprHash(h *maphash.Hash, expr ast.Expr) {
	// Use the formatted string representation of the expression for hashing.
	// This ensures that expressions that format the same way will hash the same.
	data, err := format.Node(expr, format.Simplify())
	if err != nil {
		panic(fmt.Errorf("invalid ast Expr: %v", err))
	}
	h.Write(data)
}

type uniqueItems struct {
	items *anyunique.Store[item, *uniqueItems]
}

func newUniqueItems() *uniqueItems {
	u := &uniqueItems{}
	u.items = anyunique.New[item, *uniqueItems](u)
	return u
}

func (u *uniqueItems) writeHash(h *maphash.Hash, it internItem) {
	u.items.WriteHash(h, it)
}

func (u *uniqueItems) apply(it internItem, f func(internItem, *uniqueItems) internItem) internItem {
	it1 := it.Value().apply(f, u)
	if it1 == it.Value() {
		return it
	}
	return u.items.Make(it1)
}

type internItem = anyunique.Handle[item]

func (u *uniqueItems) intern(it item) internItem {
	return u.items.Make(it)
}

// Hash implements [anyunique.Hasher.Hash].
func (u *uniqueItems) Hash(h *maphash.Hash, x item) {
	maphash.WriteComparable(h, reflect.TypeOf(x))
	x.hash(h, u)
}

// Equal implements [anyunique.Hasher.Equal] for two
// items x0 and x1.
func (u *uniqueItems) Equal(x0, x1 item) bool {
	if x0 == x1 {
		return true
	}
	// TODO although this is typically only called when items are
	// identical (because hash collisions are rare), it could made more
	// efficient. It would be better to have custom equality methods for
	// each type, or at least a reflect-based equality checker that
	// avoids unexported fields and compares [anyunique.Handle] values
	// without descending into them.
	return reflect.DeepEqual(x0, x1)
}

func applyMap(m map[string]internItem, f func(internItem, *uniqueItems) internItem, u *uniqueItems) (map[string]internItem, bool) {
	var m1 map[string]internItem
	for key, e := range m {
		e1 := f(e, u)
		if e1 == e {
			continue
		}
		if m1 == nil {
			m1 = make(map[string]internItem)
		}
		m1[key] = e1
	}
	if m1 == nil {
		return m, false
	}
	if len(m1) == len(m) {
		return m1, true
	}
	for key, e := range m {
		if _, ok := m1[key]; !ok {
			m1[key] = e
		}
	}
	return m1, true
}

func applyElems(elems []internItem, f func(internItem, *uniqueItems) internItem, u *uniqueItems) ([]internItem, bool) {
	changed := false
	for i, e := range elems {
		e1 := f(e, u)
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
