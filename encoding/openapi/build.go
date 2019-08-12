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

package openapi

import (
	"fmt"
	"math"
	"math/big"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd/v2"
)

type buildContext struct {
	inst      *cue.Instance
	refPrefix string
	path      []string

	expandRefs  bool
	structural  bool
	nameFunc    func(inst *cue.Instance, path []string) string
	descFunc    func(v cue.Value) string
	fieldFilter *regexp.Regexp
	evalDepth   int // detect cycles when resolving references

	schemas *OrderedMap

	// Track external schemas.
	externalRefs map[string]*externalType
}

type externalType struct {
	ref   string
	inst  *cue.Instance
	path  []string
	value cue.Value
}

type oaSchema = OrderedMap

type typeFunc func(b *builder, a cue.Value)

func schemas(g *Generator, inst *cue.Instance) (schemas *OrderedMap, err error) {
	var fieldFilter *regexp.Regexp
	if g.FieldFilter != "" {
		fieldFilter, err = regexp.Compile(g.FieldFilter)
		if err != nil {
			return nil, errors.Newf(token.NoPos, "invalid field filter: %v", err)
		}

		// verify that certain elements are still passed.
		for _, f := range strings.Split(
			"version,title,allOf,anyOf,not,enum,Schema/properties,Schema/items"+
				"nullable,type", ",") {
			if fieldFilter.MatchString(f) {
				return nil, errors.Newf(token.NoPos, "field filter may not exclude %q", f)
			}
		}
	}

	c := buildContext{
		inst:         inst,
		refPrefix:    "components/schemas",
		expandRefs:   g.ExpandReferences,
		structural:   g.ExpandReferences,
		nameFunc:     g.ReferenceFunc,
		descFunc:     g.DescriptionFunc,
		schemas:      &OrderedMap{},
		externalRefs: map[string]*externalType{},
		fieldFilter:  fieldFilter,
	}

	defer func() {
		switch x := recover().(type) {
		case nil:
		case *openapiError:
			err = x
		default:
			panic(x)
		}
	}()

	// Although paths is empty for now, it makes it valid OpenAPI spec.

	i, err := inst.Value().Fields()
	if err != nil {
		return nil, err
	}
	for i.Next() {
		// message, enum, or constant.
		label := i.Label()
		if c.isInternal(label) {
			continue
		}
		ref := c.makeRef(inst, []string{label})
		if ref == "" {
			continue
		}
		c.schemas.Set(ref, c.build(label, i.Value()))
	}

	// keep looping until a fixed point is reached.
	for done := 0; len(c.externalRefs) != done; {
		done = len(c.externalRefs)

		// From now on, all references need to be expanded
		external := []string{}
		for k := range c.externalRefs {
			external = append(external, k)
		}
		sort.Strings(external)

		for _, k := range external {
			ext := c.externalRefs[k]
			c.schemas.Set(ext.ref, c.build(ext.ref, ext.value.Eval()))
		}
	}

	return c.schemas, nil
}

func (c *buildContext) build(name string, v cue.Value) *oaSchema {
	return newCoreBuilder(c).schema(nil, name, v)
}

// isInternal reports whether or not to include this type.
func (c *buildContext) isInternal(name string) bool {
	// TODO: allow a regexp filter in Config. If we have closed structs and
	// definitions, this will likely be unnecessary.
	return strings.HasSuffix(name, "_value")
}

func (b *builder) failf(v cue.Value, format string, args ...interface{}) {
	panic(&openapiError{
		errors.NewMessage(format, args),
		b.ctx.path,
		v.Pos(),
	})
}

func (b *builder) unsupported(v cue.Value) {
	if b.format == "" {
		// Not strictly an error, but consider listing it as a warning
		// in strict mode.
	}
}

func (b *builder) checkArgs(a []cue.Value, n int) {
	if len(a)-1 != n {
		b.failf(a[0], "%v must be used with %d arguments", a[0], len(a)-1)
	}
}

func (b *builder) schema(core *builder, name string, v cue.Value) *oaSchema {
	oldPath := b.ctx.path
	b.ctx.path = append(b.ctx.path, name)
	defer func() { b.ctx.path = oldPath }()

	var c *builder
	if core == nil && b.ctx.structural {
		c = newCoreBuilder(b.ctx)
		c.buildCore(v)     // initialize core structure
		c.coreSchema(name) // build the
	} else {
		c = newRootBuilder(b.ctx)
		c.core = core
	}

	return c.fillSchema(v)
}

func (b *builder) getDoc(v cue.Value) {
	doc := []string{}
	if b.ctx.descFunc != nil {
		if str := b.ctx.descFunc(v); str != "" {
			doc = append(doc, str)
		}
	} else {
		for _, d := range v.Doc() {
			doc = append(doc, d.Text())
		}
	}
	if len(doc) > 0 {
		str := strings.TrimSpace(strings.Join(doc, "\n\n"))
		b.setSingle("description", str, true)
	}
}

func (b *builder) fillSchema(v cue.Value) *oaSchema {
	if b.filled != nil {
		return b.filled
	}

	b.setValueType(v)
	b.format = extractFormat(v)

	if b.core == nil || len(b.core.values) > 1 {
		isRef := b.value(v, nil)
		if isRef {
			b.typ = ""
		}

		if !isRef && !b.ctx.structural {
			b.getDoc(v)
		}
	}

	schema := b.finish()

	simplify(b, schema)

	sortSchema(schema)

	b.filled = schema
	return schema
}

func sortSchema(s *oaSchema) {
	sort.Slice(s.kvs, func(i, j int) bool {
		pi := fieldOrder[s.kvs[i].Key]
		pj := fieldOrder[s.kvs[j].Key]
		if pi != pj {
			return pi > pj
		}
		return s.kvs[i].Key < s.kvs[j].Key
	})
}

var fieldOrder = map[string]int{
	"description":      31,
	"type":             30,
	"format":           29,
	"required":         28,
	"properties":       27,
	"minProperties":    26,
	"maxProperties":    25,
	"minimum":          24,
	"exclusiveMinimum": 23,
	"maximum":          22,
	"exclusiveMaximum": 21,
	"minItems":         18,
	"maxItems":         17,
	"minLength":        16,
	"maxLength":        15,
	"items":            14,
	"enum":             13,
	"default":          12,
}

func (b *builder) resolve(v cue.Value) cue.Value {
	// Cycles are not allowed when expanding references. Right now we just
	// cap the depth of evaluation at 30.
	// TODO: do something more principled.
	const maxDepth = 30
	if b.ctx.evalDepth > maxDepth {
		b.failf(v, "maximum stack depth of %d reached", maxDepth)
	}
	b.ctx.evalDepth++
	defer func() { b.ctx.evalDepth-- }()

	switch op, a := v.Expr(); op {
	case cue.SelectorOp:
		field, _ := a[1].String()
		v = b.resolve(a[0]).Lookup(field)
	}
	return v
}

func (b *builder) value(v cue.Value, f typeFunc) (isRef bool) {
	count := 0
	disallowDefault := false
	var values cue.Value
	if b.ctx.expandRefs || b.format != "" {
		values = b.resolve(v)
		count = 1
	} else {
		dedup := map[string]bool{}
		hasNoRef := false
		for _, v := range appendSplit(nil, cue.AndOp, v) {
			// This may be a reference to an enum. So we need to check references before
			// dissecting them.
			switch p, r := v.Reference(); {
			case len(r) > 0:
				ref := b.ctx.makeRef(p, r)
				if ref == "" {
					v = b.resolve(v)
					break
				}
				if dedup[ref] {
					continue
				}
				dedup[ref] = true

				b.addRef(v, p, r)
				disallowDefault = true
				continue
			}
			hasNoRef = true
			count++
			values = values.Unify(v)
		}
		isRef = !hasNoRef && len(dedup) == 1
	}

	if count > 0 { // TODO: implement IsAny.
		// NOTE: Eval is not necessary here. Removing it will yield
		// different results that also are correct. The difference is that
		// Eval detects and eliminates impossible combinations at the
		// expense of having potentially much larger configurations due to
		// a combinatorial explosion. This rudimentary check picks the least
		// fo the two extreme forms.
		if eval := values.Eval(); countNodes(eval) < countNodes(values) {
			values = eval
		}

		for _, v := range appendSplit(nil, cue.AndOp, values) {
			switch {
			case isConcrete(v):
				b.dispatch(f, v)
				if !b.isNonCore() {
					b.set("enum", []interface{}{b.decode(v)})
				}
			default:
				if a := appendSplit(nil, cue.OrOp, v); len(a) > 1 {
					b.disjunction(a, f)
				} else {
					v = a[0]
					if err := v.Err(); err != nil {
						b.failf(v, "openapi: %v", err)
						return
					}
					b.dispatch(f, v)
				}
			}
		}
	}

	if v, ok := v.Default(); ok && v.IsConcrete() && !disallowDefault {
		// TODO: should we show the empty list default? This would be correct
		// but perhaps a bit too pedantic and noisy.
		switch {
		case v.Kind() == cue.ListKind:
			iter, _ := v.List()
			if !iter.Next() {
				// Don't show default for empty list.
				break
			}
			fallthrough
		default:
			if !b.isNonCore() {
				b.setFilter("Schema", "default", v)
			}
		}
	}
	return isRef
}

func appendSplit(a []cue.Value, splitBy cue.Op, v cue.Value) []cue.Value {
	op, args := v.Expr()
	if op == cue.NoOp && len(args) > 0 {
		// TODO: this is to deal with default value removal. This may change
		// whe we completely separate default values from values.
		a = append(a, args...)
	} else if op != splitBy {
		a = append(a, v)
	} else {
		for _, v := range args {
			a = appendSplit(a, splitBy, v)
		}
	}
	return a
}

func countNodes(v cue.Value) (n int) {
	switch op, a := v.Expr(); op {
	case cue.OrOp, cue.AndOp:
		for _, v := range a {
			n += countNodes(v)
		}
		n += len(a) - 1
	default:
		switch v.Kind() {
		case cue.ListKind:
			for i, _ := v.List(); i.Next(); {
				n += countNodes(i.Value())
			}
		case cue.StructKind:
			for i, _ := v.Fields(); i.Next(); {
				n += countNodes(i.Value()) + 1
			}
		}
	}
	return n + 1
}

// isConcrete reports whether v is concrete and not a struct (recursively).
// structs are not supported as the result of a struct enum depends on how
// conjunctions and disjunctions are distributed. We could consider still doing
// this if we define a normal form.
func isConcrete(v cue.Value) bool {
	if !v.IsConcrete() {
		return false
	}
	if v.Kind() == cue.StructKind {
		return false // TODO: handle struct kinds
	}
	for list, _ := v.List(); list.Next(); {
		if !isConcrete(list.Value()) {
			return false
		}
	}
	return true
}

func (b *builder) disjunction(a []cue.Value, f typeFunc) {
	disjuncts := []cue.Value{}
	enums := []interface{}{} // TODO: unique the enums
	nullable := false        // Only supported in OpenAPI, not JSON schema

	for _, v := range a {
		switch {
		case v.Null() == nil:
			// TODO: for JSON schema, we need to fall through.
			nullable = true

		case isConcrete(v):
			enums = append(enums, b.decode(v))

		default:
			disjuncts = append(disjuncts, v)
		}
	}

	// Only one conjunct?
	if len(disjuncts) == 0 || (len(disjuncts) == 1 && len(enums) == 0) {
		if len(disjuncts) == 1 {
			b.value(disjuncts[0], f)
		}
		if len(enums) > 0 && !b.isNonCore() {
			b.set("enum", enums)
		}
		if nullable {
			b.setSingle("nullable", true, true) // allowed in Structural
		}
		return
	}

	anyOf := []*oaSchema{}
	if len(enums) > 0 {
		anyOf = append(anyOf, b.kv("enum", enums))
	}

	hasEmpty := false
	for _, v := range disjuncts {
		c := newOASBuilder(b)
		c.value(v, f)
		t := c.finish()
		if len(t.kvs) == 0 {
			hasEmpty = true
		}
		anyOf = append(anyOf, t)
	}

	// If any of the types was "any", a oneOf may be discarded.
	if !hasEmpty {
		b.set("oneOf", anyOf)
	}

	// TODO: analyze CUE structs to figure out if it should be oneOf or
	// anyOf. As the source is protobuf for now, it is always oneOf.
	if nullable {
		b.setSingle("nullable", true, true)
	}
}

func (b *builder) setValueType(v cue.Value) {
	if b.core != nil {
		return
	}

	switch v.IncompleteKind() &^ cue.BottomKind {
	case cue.BoolKind:
		b.typ = "boolean"
	case cue.FloatKind, cue.NumberKind:
		b.typ = "number"
	case cue.IntKind:
		b.typ = "integer"
	case cue.BytesKind:
		b.typ = "string"
	case cue.StringKind:
		b.typ = "string"
	case cue.StructKind:
		b.typ = "object"
	case cue.ListKind:
		b.typ = "array"
	}
}

func (b *builder) dispatch(f typeFunc, v cue.Value) {
	if f != nil {
		f(b, v)
		return
	}

	switch v.IncompleteKind() &^ cue.BottomKind {
	case cue.NullKind:
		// TODO: for JSON schema we would set the type here. For OpenAPI,
		// it must be nullable.
		b.setSingle("nullable", true, true)

	case cue.BoolKind:
		b.setType("boolean", "")
		// No need to call.

	case cue.FloatKind, cue.NumberKind:
		// TODO:
		// Common   Name	type	format	Comments
		// float	number	float
		// double	number	double
		b.setType("number", "") // may be overridden to integer
		b.number(v)

	case cue.IntKind:
		// integer	integer	int32	signed 	32 bits
		// long		integer	int64	signed 	64 bits
		b.setType("integer", "") // may be overridden to integer
		b.number(v)

		// TODO: for JSON schema, consider adding multipleOf: 1.

	case cue.BytesKind:
		// byte		string	byte	base64 	encoded characters
		// binary	string	binary	any 	sequence of octets
		b.setType("string", "byte")
		b.bytes(v)
	case cue.StringKind:
		// date		string			date	   As defined by full-date - RFC3339
		// dateTime	string			date-time  As defined by date-time - RFC3339
		// password	string			password   A hint to UIs to obscure input
		b.setType("string", "")
		b.string(v)
	case cue.StructKind:
		b.setType("object", "")
		b.object(v)
	case cue.ListKind:
		b.setType("array", "")
		b.array(v)
	}
}

// object supports the following
// - maxProperties: maximum allowed fields in this struct.
// - minProperties: minimum required fields in this struct.
// - patternProperties: [regexp]: schema
//   TODO: we can support this once .kv(key, value) allow
//      foo [=~"pattern"]: type
//      An instance field must match all schemas for which a regexp matches.
//   Even though it is not supported in OpenAPI, we should still accept it
//   when receiving from OpenAPI. We could possibly use disjunctions to encode
//   this.
// - dependencies: what?
// - propertyNames: schema
//   every property name in the enclosed schema matches that of
func (b *builder) object(v cue.Value) {
	// TODO: discriminator objects: we could theoretically derive discriminator
	// objects automatically: for every object in a oneOf/allOf/anyOf, or any
	// object composed of the same type, if a property is required and set to a
	// constant value for each type, it is a discriminator.

	switch op, a := v.Expr(); op {
	case cue.CallOp:
		name := fmt.Sprint(a[0])
		switch name {
		case "struct.MinFields":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "minProperties", b.int(a[1]))
			return

		case "struct.MaxFields":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "maxProperties", b.int(a[1]))
			return

		default:
			b.unsupported(a[0])
			return
		}

	case cue.NoOp:
		// TODO: extract format from specific type.

	default:
		b.failf(v, "unsupported op %v for object type (%v)", op, v)
		return
	}

	required := []string{}
	for i, _ := v.Fields(cue.Optional(false), cue.Hidden(false)); i.Next(); {
		required = append(required, i.Label())
	}
	if len(required) > 0 {
		b.setFilter("Schema", "required", required)
	}

	var properties *OrderedMap
	if b.singleFields != nil {
		properties = b.singleFields.getMap("properties")
	}
	hasProps := properties != nil
	if !hasProps {
		properties = &OrderedMap{}
	}

	for i, _ := v.Fields(cue.Optional(true), cue.Hidden(false)); i.Next(); {
		label := i.Label()
		var core *builder
		if b.core != nil {
			core = b.core.properties[label]
		}
		schema := b.schema(core, label, i.Value())
		if !b.isNonCore() || len(schema.kvs) > 0 {
			properties.Set(label, schema)
		}
	}

	if !hasProps && len(properties.kvs) > 0 {
		b.setSingle("properties", properties, false)
	}

	if t, ok := v.Elem(); ok && (b.core == nil || b.core.items == nil) {
		schema := b.schema(nil, "*", t)
		if len(schema.kvs) > 0 {
			b.setSingle("additionalProperties", schema, true) // Not allowed in structural.
		}
	}

	// TODO: maxProperties, minProperties: can be done once we allow cap to
	// unify with structs.
}

// List constraints:
//
// Max and min items.
// - maxItems: int (inclusive)
// - minItems: int (inclusive)
// - items (item type)
//   schema: applies to all items
//   array of schemas:
//       schema at pos must match if both value and items are defined.
// - additional items:
//   schema: where items must be an array of schemas, intstance elements
//   succeed for if they match this value for any value at a position
//   greater than that covered by items.
// - uniqueItems: bool
//   TODO: support with list.Unique() unique() or comprehensions.
//   For the latter, we need equality for all values, which is doable,
//   but not done yet.
//
// NOT SUPPORTED IN OpenAPI:
// - contains:
//   schema: an array instance is valid if at least one element matches
//   this schema.
func (b *builder) array(v cue.Value) {

	switch op, a := v.Expr(); op {
	case cue.CallOp:
		name := fmt.Sprint(a[0])
		switch name {
		case "list.UniqueItems":
			b.checkArgs(a, 0)
			b.setFilter("Schema", "uniqueItems", true)
			return

		case "list.MinItems":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "minItems", b.int(a[1]))
			return

		case "list.MaxItems":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "maxItems", b.int(a[1]))
			return

		default:
			b.unsupported(a[0])
			return
		}

	case cue.NoOp:
		// TODO: extract format from specific type.

	default:
		b.failf(v, "unsupported op %v for array type", op)
		return
	}

	// Possible conjuncts:
	//   - one list (CUE guarantees merging all conjuncts)
	//   - no cap: is unified with list
	//   - unique items: at most one, but idempotent if multiple.
	// There is never a need for allOf or anyOf. Note that a CUE list
	// corresponds almost one-to-one to OpenAPI lists.
	items := []*oaSchema{}
	count := 0
	for i, _ := v.List(); i.Next(); count++ {
		items = append(items, b.schema(nil, strconv.Itoa(count), i.Value()))
	}
	if len(items) > 0 {
		// TODO: per-item schema are not allowed in OpenAPI, only in JSON Schema.
		// Perhaps we should turn this into an OR after first normalizing
		// the entries.
		b.set("items", items)
		// panic("per-item types not supported in OpenAPI")
	}

	// TODO:
	// A CUE cap can be a set of discontinuous ranges. If we encounter this,
	// we can create an allOf(list type, anyOf(ranges)).
	cap := v.Len()
	hasMax := false
	maxLength := int64(math.MaxInt64)

	if n, capErr := cap.Int64(); capErr == nil {
		maxLength = n
		hasMax = true
	} else {
		b.value(cap, (*builder).listCap)
	}

	if !hasMax || int64(len(items)) < maxLength {
		if typ, ok := v.Elem(); ok {
			var core *builder
			if b.core != nil {
				core = b.core.items
			}
			t := b.schema(core, "*", typ)
			if len(items) > 0 {
				b.setFilter("Schema", "additionalItems", t) // Not allowed in structural.
			} else if !b.isNonCore() || len(t.kvs) > 0 {
				b.setSingle("items", t, true)
			}
		}
	}
}

func (b *builder) listCap(v cue.Value) {
	switch op, a := v.Expr(); op {
	case cue.LessThanOp:
		b.setFilter("Schema", "maxItems", b.int(a[0])-1)
	case cue.LessThanEqualOp:
		b.setFilter("Schema", "maxItems", b.int(a[0]))
	case cue.GreaterThanOp:
		b.setFilter("Schema", "minItems", b.int(a[0])+1)
	case cue.GreaterThanEqualOp:
		if b.int(a[0]) > 0 {
			b.setFilter("Schema", "minItems", b.int(a[0]))
		}
	case cue.NoOp:
		// must be type, so okay.
	case cue.NotEqualOp:
		i := b.int(a[0])
		b.setNot("allOff", []*oaSchema{
			b.kv("minItems", i),
			b.kv("maxItems", i),
		})

	default:
		b.failf(v, "unsupported op for list capacity %v", op)
		return
	}
}

func (b *builder) number(v cue.Value) {
	// Multiple conjuncts mostly means just additive constraints.
	// Type may be number of float.

	switch op, a := v.Expr(); op {
	case cue.LessThanOp:
		b.setFilter("Schema", "exclusiveMaximum", b.big(a[0]))

	case cue.LessThanEqualOp:
		b.setFilter("Schema", "maximum", b.big(a[0]))

	case cue.GreaterThanOp:
		b.setFilter("Schema", "exclusiveMinimum", b.big(a[0]))

	case cue.GreaterThanEqualOp:
		b.setFilter("Schema", "minimum", b.big(a[0]))

	case cue.NotEqualOp:
		i := b.big(a[0])
		b.setNot("allOff", []*oaSchema{
			b.kv("minimum", i),
			b.kv("maximum", i),
		})

	case cue.CallOp:
		name := fmt.Sprint(a[0])
		switch name {
		case "math.MultipleOf":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "multipleOf", b.int(a[1]))

		default:
			b.unsupported(a[0])
			return
		}

	case cue.NoOp:
		// TODO: extract format from specific type.

	default:
		b.failf(v, "unsupported op for number %v", op)
	}
}

// Multiple Regexp conjuncts are represented as allOf all other
// constraints can be combined unless in the even of discontinuous
// lengths.

// string supports the following options:
//
// - maxLength (Unicode codepoints)
// - minLength (Unicode codepoints)
// - pattern (a regexp)
//
// The regexp pattern is as follows, and is limited to be a  strict subset of RE2:
// Ref: https://tools.ietf.org/html/draft-wright-json-schema-validation-01#section-3.3
//
// JSON schema requires ECMA 262 regular expressions, but
// limited to the following constructs:
//   - simple character classes: [abc]
//   - range character classes: [a-z]
//   - complement character classes: [^abc], [^a-z]
//   - simple quantifiers: +, *, ?, and lazy versions +? *? ??
//   - range quantifiers: {x}, {x,y}, {x,}, {x}?, {x,y}?, {x,}?
//   - begin and end anchors: ^ and $
//   - simple grouping: (...)
//   - alteration: |
// This is a subset of RE2 used by CUE.
//
// Most notably absent:
//   - the '.' for any character (not sure if that is a doc bug)
//   - character classes \d \D [[::]] \pN \p{Name} \PN \P{Name}
//   - word boundaries
//   - capturing directives.
//   - flag setting
//   - comments
//
// The capturing directives and comments can be removed without
// compromising the meaning of the regexp (TODO). Removing
// flag setting will be tricky. Unicode character classes,
// boundaries, etc can be compiled into simple character classes,
// although the resulting regexp will look cumbersome.
//
func (b *builder) string(v cue.Value) {
	switch op, a := v.Expr(); op {

	case cue.RegexMatchOp, cue.NotRegexMatchOp:
		s, err := a[0].String()
		if err != nil {
			// TODO: this may be an unresolved interpolation or expression. Consider
			// whether it is reasonable to treat unevaluated operands as wholes and
			// generate a compound regular expression.
			b.failf(v, "regexp value must be a string: %v", err)
			return
		}
		if op == cue.RegexMatchOp {
			b.setFilter("Schema", "pattern", s)
		} else {
			b.setNot("pattern", s)
		}

	case cue.NoOp, cue.SelectorOp:

	case cue.CallOp:
		name := fmt.Sprint(a[0])
		switch name {
		case "strings.MinRunes":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "minLength", b.int(a[1]))
			return

		case "strings.MaxRunes":
			b.checkArgs(a, 1)
			b.setFilter("Schema", "maxLength", b.int(a[1]))
			return

		default:
			b.unsupported(a[0])
			return
		}

	default:
		b.failf(v, "unsupported op %v for string type", op)
	}
}

func (b *builder) bytes(v cue.Value) {
	switch op, a := v.Expr(); op {

	case cue.RegexMatchOp, cue.NotRegexMatchOp:
		s, err := a[0].Bytes()
		if err != nil {
			// TODO: this may be an unresolved interpolation or expression. Consider
			// whether it is reasonable to treat unevaluated operands as wholes and
			// generate a compound regular expression.
			b.failf(v, "regexp value must be of type bytes: %v", err)
			return
		}

		if op == cue.RegexMatchOp {
			b.setFilter("Schema", "pattern", s)
		} else {
			b.setNot("pattern", s)
		}

		// TODO: support the following JSON schema constraints
		// - maxLength
		// - minLength

	case cue.NoOp, cue.SelectorOp:

	default:
		b.failf(v, "unsupported op %v for bytes type", op)
	}
}

type builder struct {
	ctx          *buildContext
	typ          string
	format       string
	singleFields *oaSchema
	current      *oaSchema
	allOf        []*oaSchema

	// Building structural schema
	core       *builder
	kind       cue.Kind
	filled     *oaSchema
	values     []cue.Value // in structural mode, all values of not and *Of.
	keys       []string
	properties map[string]*builder
	items      *builder
}

func newRootBuilder(c *buildContext) *builder {
	return &builder{ctx: c}
}

func newOASBuilder(parent *builder) *builder {
	core := parent
	if parent.core != nil {
		core = parent.core
	}
	b := &builder{
		core:   core,
		ctx:    parent.ctx,
		typ:    parent.typ,
		format: parent.format,
	}
	return b
}

func (b *builder) isNonCore() bool {
	return b.core != nil
}

func (b *builder) setType(t, format string) {
	if b.typ == "" {
		b.typ = t
		if format != "" {
			b.format = format
		}
	}
}

func setType(t *oaSchema, b *builder) {
	if b.typ != "" {
		if b.core == nil || (b.core.typ != b.typ && !b.ctx.structural) {
			if !t.exists("type") {
				t.Set("type", b.typ)
			}
		}
	}
	if b.format != "" {
		if b.core == nil || b.core.format != b.format {
			t.Set("format", b.format)
		}
	}
}

// setFilter is like set, but allows the key-value pair to be filtered.
func (b *builder) setFilter(schema, key string, v interface{}) {
	if re := b.ctx.fieldFilter; re != nil && re.MatchString(path.Join(schema, key)) {
		return
	}
	b.set(key, v)
}

// setSingle sets a value of which there should only be one.
func (b *builder) setSingle(key string, v interface{}, drop bool) {
	if b.singleFields == nil {
		b.singleFields = &OrderedMap{}
	}
	if b.singleFields.exists(key) {
		if !drop {
			b.failf(cue.Value{}, "more than one value added for key %q", key)
		}
	}
	b.singleFields.Set(key, v)
}

func (b *builder) set(key string, v interface{}) {
	if b.current == nil {
		b.current = &OrderedMap{}
		b.allOf = append(b.allOf, b.current)
	} else if b.current.exists(key) {
		b.current = &OrderedMap{}
		b.allOf = append(b.allOf, b.current)
	}
	b.current.Set(key, v)
}

func (b *builder) kv(key string, value interface{}) *oaSchema {
	constraint := &OrderedMap{}
	constraint.Set(key, value)
	return constraint
}

func (b *builder) setNot(key string, value interface{}) {
	not := &OrderedMap{}
	not.Set("not", b.kv(key, value))
	b.add(not)
}

func (b *builder) finish() (t *oaSchema) {
	if b.filled != nil {
		return b.filled
	}
	switch len(b.allOf) {
	case 0:
		t = &OrderedMap{}

	case 1:
		t = b.allOf[0]

	default:
		t = &OrderedMap{}
		t.Set("allOf", b.allOf)
	}
	if b.singleFields != nil {
		b.singleFields.kvs = append(b.singleFields.kvs, t.kvs...)
		t = b.singleFields
	}
	setType(t, b)
	sortSchema(t)
	return t
}

func (b *builder) add(t *oaSchema) {
	b.allOf = append(b.allOf, t)
}

func (b *builder) addConjunct(f func(*builder)) {
	c := newOASBuilder(b)
	f(c)
	b.add(c.finish())
}

func (b *builder) addRef(v cue.Value, inst *cue.Instance, ref []string) {
	name := b.ctx.makeRef(inst, ref)
	b.addConjunct(func(b *builder) {
		b.set("$ref", path.Join("#", b.ctx.refPrefix, name))
	})

	if b.ctx.inst != inst {
		b.ctx.externalRefs[name] = &externalType{
			ref:   name,
			inst:  inst,
			path:  ref,
			value: v,
		}
	}
}

func (b *buildContext) makeRef(inst *cue.Instance, ref []string) string {
	a := make([]string, 0, len(ref)+3)
	if b.nameFunc != nil {
		a = append(a, b.nameFunc(inst, ref))
	} else {
		a = append(a, ref...)
	}
	return path.Join(a...)
}

func (b *builder) int(v cue.Value) int64 {
	i, err := v.Int64()
	if err != nil {
		b.failf(v, "could not retrieve int: %v", err)
	}
	return i
}

func (b *builder) decode(v cue.Value) interface{} {
	var d interface{}
	if err := v.Decode(&d); err != nil {
		b.failf(v, "decode error: %v", err)
	}
	return d
}

func (b *builder) big(v cue.Value) interface{} {
	var mant big.Int
	exp, err := v.MantExp(&mant)
	if err != nil {
		b.failf(v, "value not a number: %v", err)
		return nil
	}
	return &decimal{apd.NewWithBigInt(&mant, int32(exp))}
}
