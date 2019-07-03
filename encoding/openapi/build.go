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
	nameFunc    func(inst *cue.Instance, path []string) string
	descFunc    func(v cue.Value) string
	fieldFilter *regexp.Regexp

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

func schemas(g *Generator, inst *cue.Instance) (schemas OrderedMap, err error) {
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
		refPrefix:    "components/schema",
		expandRefs:   g.ExpandReferences,
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
		c.schemas.set(c.makeRef(inst, []string{label}), c.build(label, i.Value()))
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
			c.schemas.set(ext.ref, c.build(ext.ref, ext.value.Eval()))
		}
	}

	return *c.schemas, nil
}

func (c *buildContext) build(name string, v cue.Value) *oaSchema {
	return newRootBuilder(c).schema(name, v)
}

// isInternal reports whether or not to include this type.
func (c *buildContext) isInternal(name string) bool {
	// TODO: allow a regexp filter in Config. If we have closed structs and
	// definitions, this will likely be unnecessary.
	return strings.HasSuffix(name, "_value")
}

// shouldExpand reports is the given identifier is not exported.
func (c *buildContext) shouldExpand(p *cue.Instance, ref []string) bool {
	return c.expandRefs
}

func (b *builder) failf(v cue.Value, format string, args ...interface{}) {
	panic(&openapiError{
		errors.NewMessage(format, args),
		b.ctx.path,
		v.Pos(),
	})
}

func (b *builder) schema(name string, v cue.Value) *oaSchema {
	oldPath := b.ctx.path
	b.ctx.path = append(b.ctx.path, name)
	defer func() { b.ctx.path = oldPath }()

	c := newRootBuilder(b.ctx)
	c.format = extractFormat(v)
	isRef := c.value(v, nil)
	schema := c.finish()

	if !isRef {
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
			schema.prepend("description", str)
		}
	}

	simplify(c, schema)

	return schema
}

func (b *builder) value(v cue.Value, f typeFunc) (isRef bool) {
	count := 0
	disallowDefault := false
	var values cue.Value
	p, a := v.Reference()
	if b.ctx.shouldExpand(p, a) {
		values = v
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
				if dedup[ref] {
					continue
				}
				dedup[ref] = true

				b.addRef(v, p, r)
				disallowDefault = true
			default:
				hasNoRef = true
				count++
				values = values.Unify(v)
			}
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
				b.set("enum", []interface{}{b.decode(v)})

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
			b.setFilter("Schema", "default", v)
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
		if len(enums) > 0 {
			b.set("enum", enums)
		}
		if nullable {
			b.set("nullable", true)
		}
		return
	}

	b.addConjunct(func(b *builder) {
		anyOf := []*oaSchema{}
		if len(enums) > 0 {
			anyOf = append(anyOf, b.kv("enum", enums))
		}

		for _, v := range disjuncts {
			c := newOASBuilder(b)
			c.value(v, f)
			anyOf = append(anyOf, c.finish())
		}

		// TODO: analyze CUE structs to figure out if it should be oneOf or
		// anyOf. As the source is protobuf for now, it is always oneOf.
		b.set("oneOf", anyOf)
		if nullable {
			b.set("nullable", true)
		}
	})
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
		b.set("nullable", true)

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
// - minProperties: minimum required fields in this struct.a
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

	required := []string{}
	for i, _ := v.Fields(cue.Optional(false), cue.Hidden(false)); i.Next(); {
		required = append(required, i.Label())
	}
	if len(required) > 0 {
		b.setFilter("Schema", "required", required)
	}

	properties := map[string]*oaSchema{}
	for i, _ := v.Fields(cue.Optional(true), cue.Hidden(false)); i.Next(); {
		properties[i.Label()] = b.schema(i.Label(), i.Value())
	}
	if len(properties) > 0 {
		b.set("properties", properties)
	}

	if t, ok := v.Elem(); ok {
		b.setFilter("Schema", "additionalProperties", b.schema("*", t))
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
	// Possible conjuncts:
	//   - one list (CUE guarantees merging all conjuncts)
	//   - no cap: is unified with list
	//   - unique items: at most one, but idempotent if multiple.
	// There is never a need for allOf or anyOf. Note that a CUE list
	// corresponds almost one-to-one to OpenAPI lists.
	items := []*oaSchema{}
	count := 0
	for i, _ := v.List(); i.Next(); count++ {
		items = append(items, b.schema(strconv.Itoa(count), i.Value()))
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
			t := b.schema("*", typ)
			if len(items) > 0 {
				b.setFilter("Schema", "additionalItems", t)
			} else {
				b.set("items", t)
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
	// TODO: support the following JSON schema constraints
	// - multipleOf
	// setIntConstraint(t, "multipleOf", a)

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
			b.kv("minItems", i),
			b.kv("maxItems", i),
		})

	case cue.NoOp:
		// TODO: extract format from specific type.

	default:
		// panic(fmt.Sprintf("unsupported of %v for number type", op))
	}
}

// Multiple Regexp conjuncts are represented as allOf all other
// constraints can be combined unless in the even of discontinuous
// lengths.

// string supports the following options:
//
// - maxLenght (Unicode codepoints)
// - minLenght (Unicode codepoints)
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
			b.setFilter("schema", "pattern", s)
		} else {
			b.setNot("pattern", s)
		}

		// TODO: support the following JSON schema constraints
		// - maxLength
		// - minLength

	case cue.NoOp, cue.SelectorOp:
		// TODO: determine formats from specific types.

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
	ctx     *buildContext
	typ     string
	format  string
	current *oaSchema
	allOf   []*oaSchema
	enums   []interface{}
}

func newRootBuilder(c *buildContext) *builder {
	return &builder{ctx: c}
}

func newOASBuilder(parent *builder) *builder {
	b := &builder{
		ctx:    parent.ctx,
		typ:    parent.typ,
		format: parent.format,
	}
	return b
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
		t.set("type", b.typ)
		if b.format != "" {
			t.set("format", b.format)
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

func (b *builder) set(key string, v interface{}) {
	if b.current == nil {
		b.current = &OrderedMap{}
		b.allOf = append(b.allOf, b.current)
		setType(b.current, b)
	} else if b.current.exists(key) {
		b.current = &OrderedMap{}
		b.allOf = append(b.allOf, b.current)
	}
	b.current.set(key, v)
}

func (b *builder) kv(key string, value interface{}) *oaSchema {
	constraint := &OrderedMap{}
	setType(constraint, b)
	constraint.set(key, value)
	return constraint
}

func (b *builder) setNot(key string, value interface{}) {
	not := &OrderedMap{}
	not.set("not", b.kv(key, value))
	b.add(not)
}

func (b *builder) finish() *oaSchema {
	switch len(b.allOf) {
	case 0:
		if b.typ == "" {
			b.failf(cue.Value{}, "no type specified at finish")
			return nil
		}
		t := &OrderedMap{}
		setType(t, b)
		return t

	case 1:
		setType(b.allOf[0], b)
		return b.allOf[0]

	default:
		t := &OrderedMap{}
		t.set("allOf", b.allOf)
		return t
	}
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
