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
	"maps"
	"slices"

	"cuelang.org/go/cue"
)

// item represents a JSON Schema constraint or structure that can be
// converted to a map representation for serialization.
type item interface {
	// generate returns the JSON object representation of this item
	generate(g *generator) map[string]any

	// walk invokes f on each sub-item, replacing each with the
	// item returned, and returns the new item (or the same if nothing has changed).
	walk(f func(item) item) item
}

// itemTrue represents a schema that accepts any value (true schema)
type itemTrue struct{}

func (i *itemTrue) generate(g *generator) map[string]any {
	return map[string]any{}
}

func (i *itemTrue) walk(f func(item) item) item {
	return i
}

// itemFalse represents a schema that accepts no values (false schema)
type itemFalse struct{}

func (i *itemFalse) generate(g *generator) map[string]any {
	return singleKeyword("not", map[string]any{})
}

func (i *itemFalse) walk(f func(item) item) item {
	return i
}

// itemAllOf represents an allOf combinator
type itemAllOf struct {
	elems []item
}

func (i *itemAllOf) add(it item) {
	i.elems = append(i.elems, it)
}

func (i *itemAllOf) generate(g *generator) map[string]any {
	// Because a single json schema object is essentially an allOf itself,
	// we can merge objects that don't share keywords
	// but we also have to be careful not to merge keywords
	// that interact with one another (for example `properties` and `patternProperties`).
	var unmerged []map[string]any
	final := make(map[string]any)
	for _, e := range i.elems {
		m := e.generate(g)
		avoidMerging := false
		for k := range m {
			// If the keyword interacts with any member already in final,
			// avoid merging, or the keyword is already present in final.
			if _, ok := final[k]; ok {
				avoidMerging = true
				break
			}
			for _, ik := range keywordInteractions[k] {
				if _, ok := final[ik]; ok {
					avoidMerging = true
					break
				}
			}
		}
		if avoidMerging {
			unmerged = append(unmerged, m)
		} else {
			maps.Copy(final, m)
		}
	}
	if len(unmerged) == 0 {
		return final
	}
	unmerged = append(unmerged, final)
	return singleKeyword("allOf", unmerged)
}

func (i *itemAllOf) walk(f func(item) item) item {
	elems, changed := walkElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemAllOf{elems: elems}
}

// itemOneOf represents a oneOf combinator
type itemOneOf struct {
	elems []item
}

func (i *itemOneOf) generate(g *generator) map[string]any {
	return singleKeyword("oneOf", generateSlice(g, i.elems))
}

func (i *itemOneOf) walk(f func(item) item) item {
	elems, changed := walkElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemOneOf{elems: elems}
}

// itemAnyOf represents an anyOf combinator
type itemAnyOf struct {
	elems []item
}

func (i *itemAnyOf) generate(g *generator) map[string]any {
	return singleKeyword("anyOf", generateSlice(g, i.elems))
}

func (i *itemAnyOf) walk(f func(item) item) item {
	elems, changed := walkElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemAnyOf{elems: elems}
}

// itemNot represents a not combinator
type itemNot struct {
	elem item
}

func (i *itemNot) generate(g *generator) map[string]any {
	return singleKeyword("not", i.elem.generate(g))
}

func (i *itemNot) walk(f func(item) item) item {
	elem := i.elem.walk(f)
	if elem == i.elem {
		return i
	}
	return &itemNot{elem: elem}
}

// itemConst represents a constant value constraint
type itemConst struct {
	value json.RawMessage
}

func (i *itemConst) generate(g *generator) map[string]any {
	return singleKeyword("const", i.value)
}

func (i *itemConst) walk(f func(item) item) item {
	return i
}

type itemEnum struct {
	values []json.RawMessage
}

func (i *itemEnum) generate(g *generator) map[string]any {
	return singleKeyword("enum", i.values)
}

func (i *itemEnum) walk(f func(item) item) item {
	return i
}

type itemRef struct {
	defName string
}

func (i *itemRef) generate(g *generator) map[string]any {
	return singleKeyword("$ref", "#/$defs/"+i.defName)
}

func (i *itemRef) walk(f func(item) item) item {
	return i
}

// itemType represents a type constraint
type itemType struct {
	kinds []string
}

func (i *itemType) generate(g *generator) map[string]any {
	if len(i.kinds) == 1 {
		return singleKeyword("type", i.kinds[0])
	}
	return singleKeyword("type", i.kinds)
}

func (i *itemType) walk(f func(item) item) item {
	return i
}

// itemFormat represents a format constraint
type itemFormat struct {
	format string
}

func (i *itemFormat) generate(g *generator) map[string]any {
	return singleKeyword("format", i.format)
}

func (i *itemFormat) walk(f func(item) item) item {
	return i
}

// itemPattern represents a pattern constraint
type itemPattern struct {
	regexp string
}

func (i *itemPattern) generate(g *generator) map[string]any {
	return singleKeyword("pattern", i.regexp)
}

func (i *itemPattern) walk(f func(item) item) item {
	return i
}

// itemBounds represents numeric bounds constraints
type itemBounds struct {
	constraint cue.Op // LessThanEqualOp, LessThanOp, GreaterThanEqualOp, GreaterThanOp
	n          float64
}

func (i *itemBounds) generate(g *generator) map[string]any {
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
	}
	return singleKeyword(keyword, i.n)
}

func (i *itemBounds) walk(f func(item) item) item {
	return i
}

// itemMultipleOf represents a multipleOf constraint
type itemMultipleOf struct {
	n float64
}

func (i *itemMultipleOf) generate(g *generator) map[string]any {
	return singleKeyword("multipleOf", i.n)
}

func (i *itemMultipleOf) walk(f func(item) item) item {
	return i
}

// itemLengthBounds represents string length constraints
type itemLengthBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (i *itemLengthBounds) generate(g *generator) map[string]any {
	var keyword string
	switch i.constraint {
	case cue.LessThanEqualOp:
		keyword = "maxLength"
	case cue.GreaterThanEqualOp:
		keyword = "minLength"
	default:
		panic("unexpected constraint in length bounds")
	}

	return singleKeyword(keyword, i.n)
}

func (i *itemLengthBounds) walk(f func(item) item) item {
	return i
}

// itemItemsBounds represents array length constraints
type itemItemsBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (i *itemItemsBounds) generate(g *generator) map[string]any {
	var keyword string
	switch i.constraint {
	case cue.LessThanEqualOp:
		keyword = "maxItems"
	case cue.GreaterThanEqualOp:
		keyword = "minItems"
	default:
		panic("unexpected constraint in items bounds")
	}
	return singleKeyword(keyword, i.n)
}

func (i *itemItemsBounds) walk(f func(item) item) item {
	return i
}

// itemPropertyBounds represents object property count constraints
type itemPropertyBounds struct {
	constraint cue.Op // LessThanEqualOp, GreaterThanEqualOp
	n          int
}

func (i *itemPropertyBounds) generate(g *generator) map[string]any {
	var keyword string
	switch i.constraint {
	case cue.LessThanEqualOp:
		keyword = "maxProperties"
	case cue.GreaterThanEqualOp:
		keyword = "minProperties"
	default:
		panic("unexpected constraint in items bounds")
	}
	return singleKeyword(keyword, i.n)
}

func (i *itemPropertyBounds) walk(f func(item) item) item {
	return i
}

// itemItems represents an items constraint for arrays
type itemItems struct {
	elem item
}

func (i *itemItems) generate(g *generator) map[string]any {
	return singleKeyword("items", i.elem.generate(g))
}

func (i *itemItems) walk(f func(item) item) item {
	elem := i.elem.walk(f)
	if elem == i.elem {
		return i
	}
	return &itemItems{elem: elem}
}

// itemPrefixItems represents prefixItems constraint for arrays
type itemPrefixItems struct {
	elems []item
}

func (i *itemPrefixItems) generate(g *generator) map[string]any {
	return singleKeyword("items", generateSlice(g, i.elems))
}

func (i *itemPrefixItems) walk(f func(item) item) item {
	elems, changed := walkElems(i.elems, f)
	if !changed {
		return i
	}
	return &itemPrefixItems{elems: elems}
}

// itemContains represents a contains constraint for arrays
type itemContains struct {
	elem item
	min  *int
	max  *int
}

func (i *itemContains) generate(g *generator) map[string]any {
	m := singleKeyword("contains", i.elem.generate(g))
	if i.min != nil {
		m["minContains"] = *i.min
	}
	if i.max != nil {
		m["maxContains"] = *i.max
	}
	return m
}

func (i *itemContains) walk(f func(item) item) item {
	elem := i.elem.walk(f)
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

func (i *itemProperties) generate(g *generator) map[string]any {
	properties := make(map[string]any)
	for _, prop := range i.elems {
		properties[prop.name] = prop.item.generate(g)
	}
	m := singleKeyword("properties", properties)
	if len(i.required) > 0 {
		m["required"] = i.required
	}
	return m
}

func (i *itemProperties) walk(f func(item) item) item {
	changed := false
	elems := i.elems
	for j, prop := range elems {
		if it := prop.item.walk(f); it != prop.item {
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

func walkElems(elems []item, f func(item) item) ([]item, bool) {
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

func (i *itemIfThenElse) generate(g *generator) map[string]any {
	m := map[string]any{
		"if": i.ifElem.generate(g),
	}
	if i.thenElem != nil {
		m["then"] = i.thenElem.generate(g)
	}
	if i.elseElem != nil {
		m["else"] = i.elseElem.generate(g)
	}
	return m
}

func (i *itemIfThenElse) walk(f func(item) item) item {
	ifElem := i.ifElem.walk(f)
	var thenElem, elseElem item
	if i.thenElem != nil {
		thenElem = i.thenElem.walk(f)
	}
	if i.elseElem != nil {
		elseElem = i.elseElem.walk(f)
	}

	if ifElem == i.ifElem && thenElem == i.thenElem && elseElem == i.elseElem {
		return i
	}
	return &itemIfThenElse{ifElem: ifElem, thenElem: thenElem, elseElem: elseElem}
}

func generateSlice(g *generator, items []item) []any {
	return mapSlice(items, func(it item) any {
		return it.generate(g)
	})
}

func singleKeyword(name string, val any) map[string]any {
	return map[string]any{
		name: val,
	}
}

var keywordGroups = [][]string{
	{"contains", "maxContains", "minContains"},
	{"properties", "patternProperties", "additionalProperties"},
	{"items", "additionalItems", "prefixItems"},
	{"if", "then", "else"},
}

var keywordInteractions = func() map[string][]string {
	m := make(map[string][]string)
	for _, ks := range keywordGroups {
		for _, k := range ks {
			m[k] = ks
		}
	}
	return m
}()
