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
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/internal/anyhash"
)

// GenerateConfig configures JSON Schema generation from CUE values.
type GenerateConfig struct {
	// Version specifies the version of JSON Schema to generate.
	// The supported versions are [VersionDraft2020_12] (the default),
	// [VersionDraft7], and [VersionOpenAPI] (the JSON-Schema-like dialect
	// used by OpenAPI 3.0).
	//
	// Because CUE is more expressive than any JSON Schema dialect,
	// generation is best-effort: constraints that cannot be represented
	// in the target dialect are rendered as permissively as possible
	// rather than causing an error.
	Version Version

	// NameFunc is used to determine how a reference maps to a JSON Schema
	// definition name. It is passed the root value (usually a package)
	// and the path to that value within it, as returned by [cue.Value.ReferencePath].
	//
	// If both NameFunc and NamesFunc are nil, [DefaultNamesFunc] will be used.
	//
	// Deprecated: use [GenerateConfig.NamesFunc] instead, which
	// allows all references to be named with full knowledge of all
	// the other references.
	NameFunc func(root cue.Value, path cue.Path) string

	// NamesFunc is used to determine how references map to JSON Schema
	// definition names. It is passed all the distinct references made
	// by the schema being generated. It is the responsibility of the
	// function to set a different [CUERef.Name] for each reference.
	//
	// If this is nil and [GenerateConfig.NameFunc] is also nil,
	// [DefaultNamesFunc] will be used.
	NamesFunc func(refs []*CUERef)

	// DescriptionFunc, if non-nil, returns the description to use for the
	// schema generated from the given value. When it returns the empty string
	// no description is emitted. When nil, descriptions are derived from the
	// value's doc comments.
	//
	// If this is nil, [DefaultDescription] will be used.
	DescriptionFunc func(v cue.Value) string

	// ExplicitOpen, when true, will never close a schema with `additionalProperties: false`
	// (but _will_ explicitly open a schema with `additionalProperties: true`
	// when there is an explicit `...` or universal pattern in a struct).
	//
	// By default (when ExplicitOpen is false), all structs that are closed will
	// have an `additionalProperties: false` added.
	ExplicitOpen bool
}

type closedMode byte

const (
	open = closedMode(iota)
	closed
	closedRecursively
)

// descend returns the closed mode that applies to m when
// descending one level of struct field.
func (m closedMode) descend() closedMode {
	if m == closedRecursively {
		return m
	}
	return open
}

// tupleStyle describes how a dialect renders heterogeneous (tuple) lists.
type tupleStyle int

const (
	// tuplePrefixItems uses prefixItems for the known prefix and items
	// for the rest (JSON Schema 2020-12 and later).
	tuplePrefixItems tupleStyle = iota
	// tupleItemsArray uses an array-valued items keyword for the known
	// prefix and additionalItems for the rest (drafts up to and
	// including draft-07).
	tupleItemsArray
	// tupleWiden has no tuple support: the prefix and rest schemas are
	// widened into a single items schema (OpenAPI 3.0).
	tupleWiden
)

// generateDialect describes how a particular schema version renders the
// version-independent item tree built by [generator.makeItem]. All
// version-specific divergence in the generated output is expressed here.
type generateDialect struct {
	version Version

	// emitSchemaKeyword reports whether a top-level $schema keyword is
	// emitted. OpenAPI schema objects do not carry $schema.
	emitSchemaKeyword bool

	// refPrefix holds the JSON Pointer tokens identifying where
	// definitions live within the generated document, for example
	// {"$defs"}, {"definitions"} or {"components", "schemas"}. It is
	// used both to nest the definitions and to construct $ref pointers.
	refPrefix []string

	// allowTypeArray reports whether the type keyword may hold an array
	// of type names. OpenAPI 3.0 requires a single type string.
	allowTypeArray bool

	// nullViaNullable reports whether nullability is expressed with a
	// nullable: true keyword rather than including "null" in type.
	nullViaNullable bool

	// numericExclusive reports whether exclusiveMinimum/exclusiveMaximum
	// hold numbers (draft-06 and later). When false they are booleans
	// accompanying minimum/maximum (draft-04 and OpenAPI 3.0).
	numericExclusive bool

	// supportsConst reports whether the const keyword is available.
	// When false a single-valued enum is emitted instead.
	supportsConst bool

	// tuples describes how heterogeneous lists are rendered.
	tuples tupleStyle

	// supportsContains reports whether the contains keyword is available.
	supportsContains bool

	// supportsMinMaxContains reports whether minContains/maxContains are
	// available (2019-09 and later).
	supportsMinMaxContains bool

	// supportsPatternProperties reports whether patternProperties is
	// available.
	supportsPatternProperties bool

	// supportsIfThenElse reports whether if/then/else are available
	// (draft-07 and later).
	supportsIfThenElse bool

	// refComposesWithSiblings reports whether keywords adjacent to a
	// $ref are honored. Before 2019-09 (so for draft-07 and OpenAPI 3.0)
	// a $ref causes all sibling keywords to be ignored, so they must be
	// kept out of any schema object that contains a $ref.
	refComposesWithSiblings bool

	// bytesViaContentEncoding reports whether base64-encoded binary is
	// expressed with `contentEncoding: base64` (draft-07 and later) rather
	// than the OpenAPI 3.0 `format: byte` keyword.
	bytesViaContentEncoding bool
}

// Generate generates a JSON Schema for the given CUE value,
// with the returned AST representing the generated JSON result.
//
// The result is typically encoded as JSON, for example by obtaining a value via
// [cue.Context.BuildExpr] and then encoding it via [encoding/json.Marshal].
//
// Note: this functionality is currently experimental. The form
// of the generated schema may, and probably will, change
// from release to release.
func Generate(v cue.Value, cfg *GenerateConfig) (ast.Expr, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	g, err := newGenerator(cfg)
	if err != nil {
		return nil, err
	}

	// Phase 1: build the item tree, collecting all references.
	rootItem := g.buildRoot(v)

	// Phase 2: assign names to all collected references before
	// generating any AST, because CUERef.generate uses the Name.
	defKeys, err := g.assignNames()
	if err != nil {
		return nil, err
	}

	// Phase 3: optimize and generate the AST.
	st, err := g.renderRoot(rootItem, v)
	if err != nil {
		return nil, err
	}

	// Add schema version metadata and definitions.
	var fields []ast.Decl
	if g.dialect.emitSchemaKeyword {
		fields = append(fields, makeField("$schema", ast.NewString(g.cfg.Version.String())))
	}
	if len(defKeys) > 0 {
		defFields := make([]ast.Decl, 0, len(defKeys))
		for _, k := range defKeys {
			defFields = append(defFields, makeField(k.Name, g.renderDef(k)))
		}
		fields = append(fields, makeNestedField(g.dialect.refPrefix, &ast.StructLit{Elts: defFields}))
	}
	fields = append(fields, st.Elts...)

	if g.err != nil {
		return nil, g.err
	}
	// When the root schema is itself a $ref, keep it out of the same
	// schema object as $schema and the definitions for dialects that
	// would otherwise ignore those keywords.
	return isolateRef(g, makeSchemaStructLit(fields...)), nil
}

// GenerateMany is like [Generate] but generates several schemas at once,
// sharing a single pool of definitions between them.
//
// Instead of nesting a definitions block (such as $defs) inside each
// generated schema, references are emitted rooted at sharedSchemaRoot (a JSON
// Pointer such as "#/components/schemas") and the referred-to schemas are
// returned in the map, keyed by the reference name assigned by
// [GenerateConfig.NamesFunc]. Identical definitions referenced from more than
// one of the values are deduplicated into a single map entry.
//
// The returned expressions correspond one-to-one with values. They do not
// carry a $schema keyword; placing the returned schemas and shared
// definitions into a containing document is the caller's responsibility.
//
// THIS IS EXPERIMENTAL. API MIGHT CHANGE.
func GenerateMany(values []cue.Value, sharedSchemaRoot string, cfg *GenerateConfig) ([]ast.Expr, map[string]ast.Expr, error) {
	for _, v := range values {
		if err := v.Validate(); err != nil {
			return nil, nil, err
		}
	}
	g, err := newGenerator(cfg)
	if err != nil {
		return nil, nil, err
	}
	refPrefix, err := refPrefixFromRoot(sharedSchemaRoot)
	if err != nil {
		return nil, nil, err
	}
	g.dialect.refPrefix = refPrefix

	// Phase 1: build every item tree, collecting all references into the
	// single shared g.defs pool.
	rootItems := make([]internItem, len(values))
	for i, v := range values {
		rootItems[i] = g.buildRoot(v)
	}

	// Phase 2: assign names across the whole shared pool.
	defKeys, err := g.assignNames()
	if err != nil {
		return nil, nil, err
	}

	// Phase 3: render each root and the shared definitions.
	exprs := make([]ast.Expr, len(values))
	for i := range values {
		st, err := g.renderRoot(rootItems[i], values[i])
		if err != nil {
			return nil, nil, err
		}
		exprs[i] = isolateRef(g, makeSchemaStructLit(st.Elts...))
	}
	shared := make(map[string]ast.Expr, len(defKeys))
	for _, k := range defKeys {
		shared[k.Name] = g.renderDef(k)
	}

	if g.err != nil {
		return nil, nil, g.err
	}
	return exprs, shared, nil
}

// newGenerator returns a generator with a normalized copy of cfg.
func newGenerator(cfg *GenerateConfig) (*generator, error) {
	if cfg == nil {
		cfg = &GenerateConfig{}
	} else {
		// Prevent mutation of the argument.
		cfg = ref(*cfg)
	}
	if cfg.NamesFunc == nil {
		if cfg.NameFunc != nil {
			nameFunc := cfg.NameFunc
			cfg.NamesFunc = func(refs []*CUERef) {
				for _, ref := range refs {
					ref.Name = nameFunc(ref.Inst, ref.Path)
				}
			}
		} else {
			cfg.NamesFunc = DefaultNamesFunc
		}
	}
	if cfg.Version == VersionUnknown {
		cfg.Version = VersionDraft2020_12
	}
	if cfg.DescriptionFunc == nil {
		cfg.DescriptionFunc = DefaultDescription
	}
	d, err := newGenerateDialect(cfg.Version)
	if err != nil {
		return nil, err
	}
	return &generator{
		cfg:     cfg,
		dialect: d,
		defs:    anyhash.NewMap[*CUERef, internItem](cueRefHasher{}),
		unique:  newUniqueItems(),
	}, nil
}

// buildRoot builds the item tree for a single root value, collecting any
// references into g.defs. It is phase 1 of generation and may be called more
// than once (by [GenerateMany]) to share a single definitions pool.
func (g *generator) buildRoot(v cue.Value) internItem {
	mode := open
	switch {
	case v.IsClosedRecursively():
		mode = closedRecursively
	case v.IsClosed():
		mode = closed
	}
	return g.makeItem(v, mode)
}

// assignNames names every reference collected in g.defs, returning the
// definition keys sorted by name. It must be called once after all roots have
// been built (phase 2). It returns a nil slice when there are no references.
func (g *generator) assignNames() ([]*CUERef, error) {
	if g.defs.Len() == 0 {
		return nil, nil
	}
	defKeys := slices.Collect(g.defs.Keys())
	slices.SortFunc(defKeys, func(k1, k2 *CUERef) int {
		return k1.Path.Compare(k2.Path)
	})
	g.cfg.NamesFunc(defKeys)
	slices.SortFunc(defKeys, func(k1, k2 *CUERef) int {
		return cmp.Compare(k1.Name, k2.Name)
	})
	if defKeys[0].Name == "" {
		return nil, fmt.Errorf("NamesFunc did not set Name field in all *CUERef values")
	}
	prev := ""
	for _, k := range defKeys {
		if k.Name == prev {
			return nil, fmt.Errorf("NamesFunc returned non-unique name %q", k.Name)
		}
		prev = k.Name
	}
	return defKeys, nil
}

// renderRoot optimizes and renders a single root item into a struct literal
// (phase 3). The value v is used only for error reporting.
func (g *generator) renderRoot(rootItem internItem, v cue.Value) (*ast.StructLit, error) {
	rootItem = optimize(rootItem, g.unique)
	expr := rootItem.Value().generate(g)

	// Check if the result is a boolean literal.
	if lit, ok := expr.(*ast.BasicLit); ok && (lit.Kind == token.TRUE || lit.Kind == token.FALSE) {
		if lit.Kind == token.FALSE {
			if g.err == nil {
				g.addError(v, fmt.Errorf("schema cannot be satisfied"))
			}
			return nil, g.err
		}
		expr = &ast.StructLit{}
	}

	st, ok := expr.(*ast.StructLit)
	if !ok {
		return nil, fmt.Errorf("expected struct literal from generate, got %T", expr)
	}
	return st, nil
}

// renderDef optimizes and renders a single shared definition.
func (g *generator) renderDef(k *CUERef) ast.Expr {
	def := optimize(g.defs.At(k), g.unique)
	return def.Value().generate(g)
}

func optimize(it internItem, u *uniqueItems) internItem {
	it = mergeAllOf(it, u)
	return enumFromConst(it, u)
}

// mergeAllOf returns the item with adjacent itemAllOf nodes
// all merged into a single itemAllOf node with all
// the conjuncts in.
func mergeAllOf(it internItem, u *uniqueItems) internItem {
	switch it1 := it.Value().(type) {
	case *itemAllOf:
		it2 := &itemAllOf{
			elems: make([]internItem, 0, len(it1.elems)),
		}
		for e := range siblings(it1) {
			// Remove elements that are entirely redundant.
			// TODO we could unify itemType elements here, for example:
			// allOf(itemType(number), itemType(integer)) -> itemType(integer)
			if !slices.Contains(it2.elems, e) {
				it2.elems = append(it2.elems, mergeAllOf(e, u))
			}
		}
		if len(it2.elems) == 1 {
			return it2.elems[0]
		}
		return u.intern(it2)
	default:
		return u.apply(it, mergeAllOf)
	}
}

func itemConjuncts(it internItem) iter.Seq[internItem] {
	return func(yield func(internItem) bool) {
		it1, ok := it.Value().(*itemAllOf)
		if !ok {
			yield(it)
			return
		}
		yieldSiblings(it1, yield)
	}
}

func siblings[T elementsItem](it T) iter.Seq[internItem] {
	return func(yield func(internItem) bool) {
		yieldSiblings(it, yield)
	}
}

func yieldSiblings[T elementsItem](it T, yield func(internItem) bool) bool {
	for _, e := range it.elements() {
		if ae, ok := e.Value().(T); ok {
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
func enumFromConst(it0 internItem, u *uniqueItems) internItem {
	switch it := it0.Value().(type) {
	case *itemAnyOf:
		if slices.ContainsFunc(it.elems, func(it internItem) bool {
			_, ok := it.Value().(*itemConst)
			return !ok
		}) {
			// They're not all consts, so return as-is.
			return it0
		}
		// All items are const. We can make an enum from this.
		// TODO this doesn't cover cases where there are some
		// const values and some noncrete values.
		it1 := &itemEnum{
			values: make([]ast.Expr, 0, len(it.elems)),
		}
		for _, e := range it.elems {
			it1.values = append(it1.values, e.Value().(*itemConst).value)
		}
		return u.intern(it1)
	default:
		return u.apply(it0, enumFromConst)
	}
}

type generator struct {
	cfg *GenerateConfig

	// dialect describes how the target schema version renders the
	// item tree.
	dialect generateDialect

	// err holds any errors accumulated during translation.
	err errors.Error

	// defs holds any definitions made during the course of generation,
	// indexed by the CUE reference package/path.
	defs *anyhash.Map[*CUERef, internItem]

	// unique ensures that all items are comparable with
	// simple equality.
	unique *uniqueItems

	// redirectFrom and redirectTo are set temporarily when inlining
	// a non-definition into a closed definition. References to
	// redirectFrom are resolved as redirectTo instead, so that
	// recursive self-references within the inlined body generate
	// $ref to the enclosing definition rather than the open
	// non-definition.
	//
	// When a redirect is active, other non-definitions encountered
	// in a closed context are also inlined at the property level
	// (handling mutual recursion). inliningNonDef prevents unbounded
	// recursion by limiting property-level inlining to one level deep.
	redirectFrom   *CUERef
	redirectTo     *CUERef
	inliningNonDef bool
}

// Note this type definition is defined further away from [siblings]
// than it ideally would be because of https://go.dev/issue/78296
// TODO when that issue is fixed, move it back again.
type elementsItem interface {
	elements() []internItem
}

func (g *generator) addError(pos cue.Value, err error) {
	// TODO pos
	g.err = errors.Append(g.err, errors.Promote(err, ""))
}

// isDefinition reports whether the given path represents a definition.
// A definition is indicated by a selector with DefinitionLabel type.
func isDefinition(path cue.Path) bool {
	for _, sel := range path.Selectors() {
		if sel.LabelType() == cue.DefinitionLabel {
			return true
		}
	}
	return false
}

func (g *generator) addErrorf(pos cue.Value, f string, a ...any) {
	g.addError(pos, fmt.Errorf(f, a...))
}

// makeItem returns an item representing the JSON Schema
// for v in naive form.
func (g *generator) makeItem(v cue.Value, mode closedMode) internItem {
	it := g.unique.intern(g.makeItem0(v, mode))

	if desc := g.cfg.DescriptionFunc(v); desc != "" {
		it = g.unique.intern(&itemDescription{description: desc, elem: it})
	}
	return it
}

func (g *generator) makeItem0(v cue.Value, mode closedMode) item {
	op, args := v.Expr()
	switch op {
	case cue.NoOp, cue.SelectorOp:
		pkg, path := v.ReferencePath()
		if !pkg.Exists() {
			break
		}
		// Check if this is a reference to a known validator function.
		// For example, list.UniqueItems (without parens) should be treated
		// the same as list.UniqueItems().
		if it := g.makeCallItem(v, []cue.Value{v}, mode); it != nil {
			return it
		}
		// It's a reference: generate a definition for it.
		// TODO Not all references need or should have a definition; we
		// could add a Config.NeedsDefinition function to determine that.
		// Lookup path directly rather than following v
		// so that we get to see the reference in isolation
		// and can follow its value even if it's a reference itself.
		v1 := pkg.LookupPath(path)
		if !v1.Exists() {
			g.addErrorf(v, "reference %v not found", path)
		}
		v = v1
		ref := &CUERef{
			Inst: pkg,
			Path: path,
		}
		if actualRef, _, ok := g.defs.Get2(ref); ok {
			if g.redirectFrom != nil && mode != open {
				if (cueRefHasher{}).Equal(ref, g.redirectFrom) {
					return g.redirectTo
				}
				if !isDefinition(path) && !g.inliningNonDef {
					g.inliningNonDef = true
					result := g.makeItem0(v, mode)
					g.inliningNonDef = false
					return result
				}
			}
			return actualRef
		}
		g.defs.Set(ref, internItem{}) // Prevent infinite loops on cycles.
		defMode := open
		if isDefinition(path) {
			defMode = closedRecursively
		}
		defItem := g.makeItem(v, defMode)
		if defMode != open {
			if innerRef, ok := defItem.Value().(*CUERef); ok && !isDefinition(innerRef.Path) {
				savedFrom, savedTo := g.redirectFrom, g.redirectTo
				g.redirectFrom = innerRef
				g.redirectTo = ref
				defItem = g.makeItem(innerRef.Inst.LookupPath(innerRef.Path), defMode)
				g.redirectFrom, g.redirectTo = savedFrom, savedTo
			}
		}
		g.defs.Set(ref, defItem)
		if g.redirectFrom != nil && mode != open && !isDefinition(path) && !g.inliningNonDef {
			g.inliningNonDef = true
			result := g.makeItem0(v, mode)
			g.inliningNonDef = false
			return result
		}
		return ref
	case cue.AndOp:
		if v.Kind() == cue.StructKind {
			// It's a conjunction of structs: we want to see all the
			// top level fields in one coherent view because JSON
			// Schema requires `additionalProperties` to be at the
			// same level as `properties`.
			return &itemAllOf{
				elems: []internItem{
					g.unique.intern(g.makeStructItem(v, mode)),
					g.unique.intern(&itemType{kinds: []string{"object"}}),
				},
			}
		}
		// TODO technically the closedness mode should be passed down
		// through conjunctions, but we don't do that because have the
		// case above for passing it down for struct kinds, and when the
		// kind isn't a struct kind, closedness either doesn't matter
		// (it's a scalar) or it's some kind of disjunction, in which
		// case passing it down actually makes things worse because
		// we'll push the `additionalProperties: false` down into
		// individual arms of the conjunction, resulting in rejection of
		// valid data. Better to be overly lax than too strict.
		// To fix this properly, we'd probably need to lift all the fields from
		// within the arms of the conjunction to the top level so that
		// we can apply additionalProperties to them all at once.
		return &itemAllOf{
			elems: mapSlice(args, func(v cue.Value) internItem { return g.makeItem(v, open) }),
		}
	case cue.OrOp:
		return &itemAnyOf{
			elems: mapSlice(args, func(v cue.Value) internItem { return g.makeItem(v, mode) }),
		}
	case cue.SpreadOp:
		// SpreadOp opens its operand (e.g. #T...). The struct processing
		// after this switch handles it correctly by iterating the spread's fields.
		// Note: the reason this works is because this causes the logic
		// to ignore whether the spread value is a reference or not.
	case cue.RegexMatchOp,
		cue.NotRegexMatchOp:
		re, err := args[0].String()
		if err != nil {
			g.addError(args[0], err)
			return &itemFalse{}
		}
		m := g.unique.intern(&itemPattern{
			regexp: re,
		})
		if op == cue.NotRegexMatchOp {
			m = g.unique.intern(&itemNot{
				elem: m,
			})
		}
		return &itemAllOf{
			elems: []internItem{
				g.unique.intern(&itemType{
					kinds: []string{"string"},
				}),
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
		it := g.unique.intern(&itemConst{
			value: expr,
		})
		if op == cue.EqualOp {
			return it.Value()
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
		if !args[0].IsConcrete() {
			// The bound refers to a non-concrete value (for example a
			// bound on another field), so it can't be expressed in JSON
			// Schema; accept anything.
			return &itemTrue{}
		}
		switch kind := args[0].Kind(); kind {
		case cue.FloatKind, cue.IntKind:
			n, err := args[0].Float64()
			if err != nil {
				// Probably non-concrete.
				return &itemTrue{}
			}
			return &itemAllOf{
				elems: []internItem{
					g.unique.intern(&itemBounds{
						constraint: op,
						n:          n,
					}),
					g.unique.intern(&itemType{
						kinds: []string{"number"},
					}),
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
		if it := g.makeCallItem(v, args, mode); it != nil {
			return it
		}
		// For unknown functions, accept anything rather than fail.
		// This allows for gradual implementation of more function types.
		return &itemTrue{}
	}
	if !v.IsNull() {
		// We want to encode null as {type: "null"} not {const: null}
		// so then there's a possibility of collapsing it together in
		// the same type keyword.
		if e, ok := g.constExpr(v, mode); ok {
			return &itemConst{
				value: e,
			}
		}
	}
	kind := v.IncompleteKind()
	if kind == cue.BottomKind && isIncompleteStruct(v) {
		// A struct containing a comprehension whose condition cannot be
		// resolved (for example `if someBool { ... }` where someBool is
		// not concrete) reports a bottom kind even though it is
		// structurally a struct. Treat it as a struct so that its known
		// fields are still emitted rather than producing an empty schema.
		kind = cue.StructKind
	}
	if kind == cue.TopKind {
		return &itemTrue{}
	}
	var it item // additional constraints for some known types.
	switch kind {
	case cue.StructKind:
		it = g.makeStructItem(v, mode)
	case cue.ListKind:
		it = g.makeListItem(v, mode)
	case cue.BytesKind:
		it = &itemBytes{}
	}
	var elems []internItem
	if kinds := cueKindToJSONSchemaTypes(kind); len(kinds) > 0 {
		elems = append(elems, g.unique.intern(&itemType{
			kinds: kinds,
		}))
	}
	if it != nil {
		elems = append(elems, g.unique.intern(it))
	}
	switch len(elems) {
	case 0:
		return &itemTrue{}
	case 1:
		return elems[0].Value()
	}
	return &itemAllOf{
		elems: elems,
	}
}

// constExpr returns the "constant" value of a given
// cue value. There are a few possible ways to represent
// a JSON Schema const in CUE; some examples:
//
//	true
//	==true
//	close({a!: true})	// Note: this is the representation Extract uses
//	[==true]
//	[true]
//
// There's some overlap here with the unary == treatment
// in [generator.makeItem] but in that case we know that
// the argument must be constant, and this case we don't.
func (g *generator) constExpr(v cue.Value, mode closedMode) (ast.Expr, bool) {
	// Check for unary == operator (e.g., ==1, ==true)
	op, args := v.Expr()
	if op == cue.EqualOp && len(args) == 1 {
		// It's a unary equals: the argument must be concrete and
		// there's no need to use [constExpr] any more.
		syntax := args[0].Syntax()
		expr, ok := syntax.(ast.Expr)
		return expr, ok
	}

	switch kind := v.Kind(); kind {
	case cue.BottomKind:
		return nil, false
	case cue.StructKind:
		if mode == open {
			// Open struct is not const.
			return nil, false
		}
		// Closed struct: all fields must be required (no optional fields)
		// and we need to recursively check all field values are const
		iter, err := v.Fields(cue.Optional(true), cue.Patterns(true))
		if err != nil {
			return nil, false
		}
		var fields []ast.Decl
		for iter.Next() {
			sel := iter.Selector()
			// All fields must be required for the struct to be const
			if sel.ConstraintType() != cue.RequiredConstraint {
				return nil, false
			}
			// Recursively check if the field value is const
			fieldExpr, ok := g.constExpr(iter.Value(), mode)
			if !ok {
				return nil, false
			}
			// Create a regular field (not required marker)
			fields = append(fields, makeField(sel.Unquoted(), fieldExpr))
		}
		return &ast.StructLit{Elts: fields}, true
	case cue.ListKind:
		if v.LookupPath(cue.MakePath(cue.AnyIndex)).Exists() {
			// Open list is not const.
			return nil, false
		}
		// Closed list: recursively check all elements are const
		iter, err := v.List()
		if err != nil {
			return nil, false
		}
		var elems []ast.Expr
		for iter.Next() {
			elemExpr, ok := g.constExpr(iter.Value(), mode)
			if !ok {
				return nil, false
			}
			elems = append(elems, elemExpr)
		}
		return &ast.ListLit{Elts: elems}, true
	}
	// For other kinds (atoms), if it's concrete, return its syntax
	if !v.IsConcrete() {
		return nil, false
	}
	expr, ok := v.Syntax().(ast.Expr)
	return expr, ok
}

func (g *generator) makeCallItem(v cue.Value, args []cue.Value, mode closedMode) item {
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
	case "close":
		if mode == open {
			mode = closed
		}
		return g.makeItem(args[1], mode).Value()
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
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"string"}}),
				g.unique.intern(&itemLengthBounds{constraint: cue.GreaterThanEqualOp, n: int(n)}),
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
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"string"}}),
				g.unique.intern(&itemLengthBounds{constraint: cue.LessThanEqualOp, n: int(n)}),
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
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"number"}}),
				g.unique.intern(&itemMultipleOf{n: n}),
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
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"string"}}),
				g.unique.intern(&itemFormat{format: format}),
			},
		}
	case "list.UniqueItems", "list.UniqueItems()":
		return &itemAllOf{
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"array"}}),
				g.unique.intern(&itemUniqueItems{}),
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
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"array"}}),
				g.unique.intern(&itemItemsBounds{constraint: constraint, n: int(n)}),
			},
		}

	case "struct.MinFields", "struct.MaxFields":
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
		if funcName == "struct.MinFields" {
			constraint = cue.GreaterThanEqualOp
		} else {
			constraint = cue.LessThanEqualOp
		}
		return &itemAllOf{
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"object"}}),
				g.unique.intern(&itemPropertyBounds{constraint: constraint, n: int(n)}),
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
		var minVal, maxVal *int64

		op, opArgs := constraintVal.Expr()
		switch op {
		case cue.NoOp:
			// It's a simple expression, could be a literal or something more complex
			// Try to parse as an int literal for the minimum
			n, err := constraintVal.Int64()
			if err == nil {
				minVal = ref(n)
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
			minVal = ref(n)
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
			minVal = ref(n)

			// Second operand should be <=M
			op2, op2Args := opArgs[1].Expr()
			if op2 != cue.LessThanEqualOp || len(op2Args) != 1 {
				return &itemTrue{}
			}
			n, err = op2Args[0].Int64()
			if err != nil {
				return &itemTrue{}
			}
			maxVal = ref(n)
		default:
			// Unknown constraint pattern, accept anything
			return &itemTrue{}
		}

		// Get the schema element from the second argument
		// Check if it's bottom first (which represents "contains: false")
		// to avoid adding errors to the generator.
		var elem internItem
		elemVal := args[2]
		if err := elemVal.Err(); err != nil {
			// Bottom value - represents "contains: false"
			elem = g.unique.intern(&itemFalse{})
		} else {
			elem = g.makeItem(elemVal, open)
		}

		return &itemAllOf{
			elems: []internItem{
				g.unique.intern(&itemType{kinds: []string{"array"}}),
				g.unique.intern(&itemContains{elem: elem, min: minVal, max: maxVal}),
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

		var items []internItem
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
			items = append(items, g.makeItem(v, open))
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
			ifElem:   g.makeItem(args[1], open),
			thenElem: trueAsNil(g.makeItem(args[2], open)),
			elseElem: trueAsNil(g.makeItem(args[3], open)),
		}

	default:
		return nil
	}
}

func (g *generator) makeStructItem(v cue.Value, mode closedMode) item {
	props := itemProperties{
		properties:        make(map[string]internItem),
		patternProperties: make(map[string]internItem),
	}
	required := make(map[string]bool)

	allOf := &itemAllOf{}
	addProperty := func(fieldName string, it internItem) {
		props.properties[fieldName] = join(props.properties[fieldName], it, g.unique)
	}
	addPatternProperty := func(pattern string, it internItem) {
		props.patternProperties[pattern] = join(props.patternProperties[pattern], it, g.unique)
	}
	hasUniversalConstraint := false
	for v := range valueConjuncts(v) {
		pkg, path := v.ReferencePath()
		if pkg.Exists() && mode != open && !isDefinition(path) && v.Kind() == cue.StructKind {
			// In a closed context, inline non-definition references so
			// their properties become local to additionalProperties.
			v = pkg.LookupPath(path)
		} else if pkg.Exists() || (v.Kind() != cue.StructKind && !isIncompleteStruct(v)) {
			// This conjunct is a reference or some other non-struct literal.
			// Let's keep it as such.
			allOf.elems = append(allOf.elems, g.makeItem(v, open))
			continue
		}
		iter, err := v.Fields(cue.Optional(true), cue.Patterns(true))
		if err != nil {
			g.addError(v, err)
			return &itemFalse{}
		}
		type pat struct {
			pattern     *regexp.Regexp
			constraints map[internItem]bool
		}
		// patternConstraints keeps track of the pattern constraints in this
		// particular conjunct so we can remove them from the individual fields.
		var patternConstraints []pat
	outer:
		for iter.Next() {
			sel := iter.Selector()
			switch sel.ConstraintType() {
			case cue.PatternConstraint:
				re, ok := regexpForValue(sel.Pattern())
				if ok {
					if re.String() == "" && acceptsAllString(sel.Pattern()) {
						// Record the fact that we've seen a universal constraint
						// because then we know that LookupPath(AnyString)
						// will return it.
						hasUniversalConstraint = true
					}
					constraint := g.makeItem(iter.Value(), mode.descend())
					addPatternProperty(re.String(), constraint)
					p := pat{
						pattern:     re,
						constraints: make(map[internItem]bool),
					}
					for c := range itemConjuncts(constraint) {
						p.constraints[c] = true
					}
					patternConstraints = append(patternConstraints, p)
				} else {
					// We can't express the constraint in JSON Schema, and it
					// might cover any number of possible labels, so the
					// only thing we can do is treat the whole thing as explicitly
					// open.
					addPatternProperty("", g.unique.intern(&itemTrue{}))
				}
				continue outer
			case cue.OptionalConstraint:
			case cue.RequiredConstraint:
				required[sel.Unquoted()] = true
			default:
				// It's a regular field. If it's concrete, then we can
				// consider the field to be optional because it's OK
				// to omit it. Otherwise it'll be required.
				if err := iter.Value().Validate(cue.Concrete(true)); err != nil {
					required[sel.Unquoted()] = true
				}
			}
			propItem := g.makeItem(iter.Value(), mode.descend())
			fieldName := sel.Unquoted()
			if len(patternConstraints) == 0 {
				addProperty(fieldName, propItem)
				continue
			}
			// There are pattern constraints which will have been unified in with
			// the constraints of any matching field. They're redundant with
			// respect to patternProperties, so remove them.
			// This has the potential to remove explicit constraints on the fields
			// themselves, but this will not change behavior, just result in a slightly
			// smaller resulting schema.
			allof, ok := propItem.Value().(*itemAllOf)
			if !ok || len(allof.elems) <= 1 {
				// No possibility of removing any conjuncts.
				addProperty(fieldName, propItem)
				continue
			}
			var elems []internItem
			for _, c := range patternConstraints {
				if !c.pattern.MatchString(fieldName) {
					continue
				}
				if elems == nil {
					elems = slices.Collect(siblings(allof))
				}
				// We've found a pattern constraint that unifies with the field name.
				// Its constraint will have been added to this property's constraints
				// but are redundant, so remove them.
				elems = slices.DeleteFunc(elems, func(it internItem) bool {
					return c.constraints[it]
				})
			}
			if len(elems) == 0 {
				propItem = g.unique.intern(&itemTrue{})
			} else {
				propItem = g.unique.intern(&itemAllOf{elems: elems})
			}
			addProperty(fieldName, propItem)
		}
	}

	ellipsis := v.LookupPath(cue.MakePath(cue.AnyString))
	if ellipsis.Exists() && !hasUniversalConstraint {
		constraint := g.makeItem(ellipsis, mode.descend())
		if isTrue(constraint) {
			// `... _` is indistingishable from `[_]: _` so set it as a
			// pattern property so we can treat it uniformly.
			addPatternProperty("", constraint)
		} else {
			// Note: currently this will never happen as the CUE evaluator
			// does not support `... T` in structs.
			props.additionalProperties = constraint
		}
	}

	if constraint, ok := props.patternProperties[""]; ok && isTrue(constraint) || len(props.properties) == 0 {
		// There's a universal pattern constraint and either no
		// properties or we accept anything. In both these cases it's
		// not possible to tell the difference between
		// `additionalProperties` (only applies to properties not
		// explicitly mentioned) and `patternProperties` (applies to all
		// properties regardless), so use `additionalProperties` in
		// preference as it's a little shorter and arguably more
		// obvious.
		props.additionalProperties = join(props.additionalProperties, constraint, g.unique)
		delete(props.patternProperties, "")
	}
	if mode != open && !g.cfg.ExplicitOpen && props.additionalProperties.Value() == nil {
		// Note: additionalProperties is lexical (applies only to fields
		// it's directly adjacent too) so it only makes sense to apply it
		// when the struct is genuinely empty or there are properties locally.
		if len(props.properties) > 0 || len(allOf.elems) == 0 {
			props.additionalProperties = g.unique.intern(&itemFalse{})
		}
	}
	props.required = slices.Sorted(maps.Keys(required))
	hasObjectConstraints :=
		len(props.properties) == 0 ||
			len(props.required) == 0 ||
			len(props.patternProperties) == 0
	if len(allOf.elems) > 0 {
		if !hasObjectConstraints {
			return allOf
		}
		allOf.elems = append(allOf.elems, g.unique.intern(&props))
		return allOf
	}
	if hasObjectConstraints {
		return &props
	}
	return &itemTrue{}
}

func (g *generator) makeListItem(v cue.Value, mode closedMode) item {
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
			// This can happen legitimately when we know that the type is a list
			// but we don't know anything about the number of items,
			// for example, a list validator. We'll treat this as if it's [... _]
			n = 0
			ellipsis = v.Context().CompileString("_")
		}
	}
	prefix := make([]internItem, n)
	for i := range n {
		elem := v.LookupPath(cue.MakePath(cue.Index(i)))
		if !elem.Exists() {
			g.addErrorf(v, "cannot get value at index %d in %v", i, v)
			return &itemFalse{}
		}
		prefix[i] = g.makeItem(elem, mode)
	}
	a := &itemAllOf{
		elems: []internItem{g.unique.intern(&itemType{kinds: []string{"array"}})},
	}
	items := &itemItems{}
	if len(prefix) > 0 {
		a.elems = append(a.elems, g.unique.intern(&itemItemsBounds{
			constraint: cue.GreaterThanEqualOp,
			n:          len(prefix),
		}))
		items.prefix = prefix
	}
	if ellipsis.Exists() {
		items.rest = trueAsNil(g.makeItem(ellipsis, mode))
	} else {
		a.elems = append(a.elems, g.unique.intern(&itemItemsBounds{
			constraint: cue.LessThanEqualOp,
			n:          len(prefix),
		}))
	}
	if items.rest.Value() != nil || len(items.prefix) > 0 {
		a.elems = append(a.elems, g.unique.intern(items))
	}
	return a
}

// newGenerateDialect returns the dialect used to generate the given version.
func newGenerateDialect(v Version) (generateDialect, error) {
	switch v {
	case VersionDraft2020_12:
		return generateDialect{
			version:                   v,
			emitSchemaKeyword:         true,
			refPrefix:                 []string{"$defs"},
			allowTypeArray:            true,
			nullViaNullable:           false,
			numericExclusive:          true,
			supportsConst:             true,
			tuples:                    tuplePrefixItems,
			supportsContains:          true,
			supportsMinMaxContains:    true,
			supportsPatternProperties: true,
			supportsIfThenElse:        true,
			refComposesWithSiblings:   true,
			bytesViaContentEncoding:   true,
		}, nil
	case VersionDraft7:
		return generateDialect{
			version:                   v,
			emitSchemaKeyword:         true,
			refPrefix:                 []string{"definitions"},
			allowTypeArray:            true,
			nullViaNullable:           false,
			numericExclusive:          true,
			supportsConst:             true,
			tuples:                    tupleItemsArray,
			supportsContains:          true,
			supportsMinMaxContains:    false,
			supportsPatternProperties: true,
			supportsIfThenElse:        true,
			refComposesWithSiblings:   false,
			bytesViaContentEncoding:   true,
		}, nil
	case VersionOpenAPI:
		return generateDialect{
			version:                   v,
			emitSchemaKeyword:         false,
			refPrefix:                 []string{"components", "schemas"},
			allowTypeArray:            false,
			nullViaNullable:           true,
			numericExclusive:          false,
			supportsConst:             false,
			tuples:                    tupleWiden,
			supportsContains:          false,
			supportsMinMaxContains:    false,
			supportsPatternProperties: false,
			supportsIfThenElse:        false,
			refComposesWithSiblings:   false,
		}, nil
	}
	return generateDialect{}, fmt.Errorf("version %v is not supported for generating JSON Schema", v)
}

func join(it1, it2 internItem, u *uniqueItems) internItem {
	if it1.Value() == nil || isTrue(it1) {
		return it2
	}
	if it2.Value() == nil || isTrue(it2) {
		return it1
	}
	return u.intern(&itemAllOf{
		elems: []internItem{it1, it2},
	})
}

// isIncompleteStruct reports whether v is structurally a struct that carries
// an incomplete error and therefore reports a bottom kind. This happens, for
// example, when a struct contains a comprehension whose condition is not
// concrete (`if someBool { ... }`): the whole struct becomes incomplete even
// though its unconditional fields are known. Such values must still be treated
// as structs so that those fields are emitted rather than an empty schema.
//
// It is a heuristic: v is deemed to be such a struct if it holds an incomplete
// error and its field iterator yields at least one field. Requiring a field
// distinguishes an incomplete struct from other incomplete values that also
// report a bottom kind, such as an unresolved call (`strconv.Atoi(x)`) or a
// genuine conflict; for those, [cue.Value.Fields] either errors or is empty.
func isIncompleteStruct(v cue.Value) bool {
	if !cue.IsIncomplete(v.Err()) {
		return false
	}
	iter, err := v.Fields(cue.Optional(true))
	if err != nil {
		return false
	}
	return iter.Next()
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

// regexpForValue tries to interpret v as a regular expression constraint,
// It returns  the regular expression and reports whether it succeeded.
func regexpForValue(v cue.Value) (*regexp.Regexp, bool) {
	s, ok := regexpForValue1(v)
	if !ok {
		return nil, false
	}
	pat, err := regexp.Compile(s)
	return pat, err == nil
}

func regexpForValue1(v cue.Value) (string, bool) {
	op, args := v.Expr()
	if op == cue.RegexMatchOp {
		if len(args) != 1 {
			return "", false
		}
		s, err := args[0].String()
		if err != nil {
			return "", false
		}
		return s, true
	}
	s, err := v.String()
	if err == nil {
		// Exact match.
		return "^" + regexp.QuoteMeta(s) + "$", true
	}
	if acceptsAllString(v) {
		// It matches all possible string labels: return
		// a regular expression that matches all possible
		// labels too.
		return "", true
	}
	return "", false
}

func acceptsAllString(v cue.Value) bool {
	// TODO return v.AcceptsAll(cue.StringKind) if/when that
	// method is implemented.
	sv := v.Context().CompileString("string")
	return v.Unify(sv).Subsume(sv, cue.Final()) == nil
}

// trueAsNil returns the nil item if the item
// is *itemTrue (top).
func trueAsNil(it internItem) internItem {
	if isTrue(it) {
		return internItem{}
	}
	return it
}

func isTrue(it internItem) bool {
	_, ok := it.Value().(*itemTrue)
	return ok
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
//
// Deprecated: use [DefaultNamesFunc] instead.
func DefaultNameFunc(inst cue.Value, ref cue.Path) string {
	var buf strings.Builder
	for i, sel := range ref.Selectors() {
		if i > 0 {
			buf.WriteByte('.')
		}
		buf.WriteString(sel.String())
	}
	return buf.String()
}

// DefaultNamesFunc holds the default function used by [Generate]
// to generate JSON Schema definition names from references.
// See [GenerateConfig.NamesFunc] for more information.
//
// It uses the shortest unique suffix of each reference path,
// stripping '#' from definition selectors where possible.
// When stripping '#' would cause a clash (e.g. both #Foo and Foo
// are referenced), the '#' is preserved for the definition.
func DefaultNamesFunc(refs []*CUERef) {
	if len(refs) == 0 {
		return
	}
	type refState struct {
		sels  []cue.Selector
		depth int
		raw   bool
	}
	states := make([]refState, len(refs))
	for i, ref := range refs {
		sels := ref.Path.Selectors()
		states[i] = refState{sels: sels, depth: 1}
		ref.Name = defName(sels, 1, false)
	}
	for {
		groups := make(map[string][]int)
		for i, ref := range refs {
			groups[ref.Name] = append(groups[ref.Name], i)
		}
		allUnique := true
		changed := false
		for name, indices := range groups {
			// A unique, non-empty name is done. An empty name (which happens
			// when a reference path ends in the anonymous # definition) must be
			// deepened even when it is unique, so that it becomes non-empty.
			if name != "" && len(indices) <= 1 {
				continue
			}
			allUnique = false
			anyDepthIncreased := false
			for _, idx := range indices {
				if s := &states[idx]; s.depth < len(s.sels) {
					s.depth++
					refs[idx].Name = defName(s.sels, s.depth, s.raw)
					changed = true
					anyDepthIncreased = true
				}
			}
			if anyDepthIncreased {
				continue
			}
			for _, idx := range indices {
				if s := &states[idx]; !s.raw {
					s.raw = true
					refs[idx].Name = defName(s.sels, s.depth, true)
					changed = true
				}
			}
		}
		if allUnique || !changed {
			break
		}
	}
}

// defName builds a definition name from the last depth selectors of sels.
// When raw is false, '#' is stripped from definition selectors; a selector that
// strips to nothing (the anonymous # definition) is skipped, so the result
// never contains an empty component. When every selector in the window strips
// away (a reference to the bare anonymous # definition), the result is empty and
// the caller deepens or falls back to raw.
func defName(sels []cue.Selector, depth int, raw bool) string {
	start := max(len(sels)-depth, 0)
	var b strings.Builder
	for _, sel := range sels[start:] {
		s := sel.String()
		if !raw && (sel.LabelType() == cue.DefinitionLabel || sel.LabelType() == cue.HiddenDefinitionLabel) {
			s = strings.TrimPrefix(s, "_")
			s = strings.TrimPrefix(s, "#")
		}
		if s == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s)
	}
	return b.String()
}

// refPrefixFromRoot converts a shared-schema-root JSON Pointer such as
// "#/components/schemas" into the sequence of tokens used as the
// [generateDialect.refPrefix].
func refPrefixFromRoot(root string) ([]string, error) {
	s := strings.TrimPrefix(root, "#")
	if s != "" && !strings.HasPrefix(s, "/") {
		return nil, fmt.Errorf("invalid shared schema root %q: must be a JSON Pointer", root)
	}
	return slices.Collect(json.Pointer(s).Tokens()), nil
}

// DefaultDescription returns the value to use for a schema's
// description. It's used as the default value for [GenerateConfig.DescriptionFunc].
func DefaultDescription(v cue.Value) string {
	docs := v.Doc()
	switch len(docs) {
	case 0:
		return ""
	case 1:
		return strings.TrimSpace(docs[0].Text())
	default:
		var b strings.Builder
		for i, d := range docs {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(strings.TrimSpace(d.Text()))
		}
		return b.String()
	}
}

// cueRefHasher implements [anyunique.Hasher] for the [CUERef] type.
//
// The [CUERef.Name] field is not included in the hash or equality
// because it's set after the map is populated.
type cueRefHasher struct{}

func (cueRefHasher) Hash(h *maphash.Hash, r *CUERef) {
	maphash.WriteComparable(h, r.Inst)
	// TODO consider adding a Hash method to cue.Path to avoid the
	// allocation from String.
	h.WriteString(r.Path.String())
}

func (cueRefHasher) Equal(x, y *CUERef) bool {
	return x.Inst == y.Inst && x.Path.Compare(y.Path) == 0
}

// mapSlice returns a slice of f(x) for each x in xs.
func mapSlice[T1, T2 any](xs []T1, f func(T1) T2) []T2 {
	xs1 := make([]T2, len(xs))
	for i, x := range xs {
		xs1[i] = f(x)
	}
	return xs1
}

func valueConjuncts(v cue.Value) iter.Seq[cue.Value] {
	return func(yield func(cue.Value) bool) {
		yieldValueConjuncts(v, yield)
	}
}

func yieldValueConjuncts(v cue.Value, yield func(cue.Value) bool) bool {
	op, args := v.Expr()
	if op != cue.AndOp {
		return yield(v)
	}
	for _, v := range args {
		if !yieldValueConjuncts(v, yield) {
			return false
		}
	}
	return true
}
