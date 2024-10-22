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

package jsonschema

// TODO:
// - replace converter from YAML to CUE to CUE (schema) to CUE.
// - define OpenAPI definitions als CUE.

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// rootDefs defines the top-level name of the map of definitions that do not
// have a valid identifier name.
//
// TODO: find something more principled, like allowing #."a-b" or `#a-b`.
const rootDefs = "#"

// A decoder converts JSON schema to CUE.
type decoder struct {
	cfg          *Config
	errs         errors.Error
	numID        int // for creating unique numbers: increment on each use
	mapURLErrors map[string]bool
	// self holds the struct literal that will eventually be embedded
	// in the top level file. It is only set when decoder.rootRef is
	// called.
	self *ast.StructLit
}

// addImport registers
func (d *decoder) addImport(n cue.Value, pkg string) *ast.Ident {
	spec := ast.NewImport(nil, pkg)
	info, err := astutil.ParseImportSpec(spec)
	if err != nil {
		d.errf(cue.Value{}, "invalid import %q", pkg)
	}
	ident := ast.NewIdent(info.Ident)
	ident.Node = spec
	ast.SetPos(ident, n.Pos())

	return ident
}

func (d *decoder) decode(v cue.Value) *ast.File {
	f := &ast.File{}

	if pkgName := d.cfg.PkgName; pkgName != "" {
		pkg := &ast.Package{Name: ast.NewIdent(pkgName)}
		f.Decls = append(f.Decls, pkg)
	}

	var a []ast.Decl

	if d.cfg.Root == "" {
		a = append(a, d.schema(nil, v)...)
	} else {
		ref := d.parseRef(token.NoPos, d.cfg.Root)
		if ref == nil {
			return f
		}
		var selectors []cue.Selector
		for _, r := range ref {
			selectors = append(selectors, cue.Str(r))
		}
		i, err := v.LookupPath(cue.MakePath(selectors...)).Fields()
		if err != nil {
			d.errs = errors.Append(d.errs, errors.Promote(err, ""))
			return nil
		}
		for i.Next() {
			ref := append(ref, i.Label())
			lab := d.mapRef(i.Value().Pos(), "", ref)
			if len(lab) == 0 {
				return nil
			}
			decls := d.schema(lab, i.Value())
			a = append(a, decls...)
		}
	}

	f.Decls = append(f.Decls, a...)

	_ = astutil.Sanitize(f)

	return f
}

func (d *decoder) schema(ref []ast.Label, v cue.Value) (a []ast.Decl) {
	root := state{
		decoder:       d,
		schemaVersion: d.cfg.DefaultVersion,
		isRoot:        true,
	}

	var name ast.Label
	inner := len(ref) - 1

	if inner >= 0 {
		name = ref[inner]
		root.isSchema = true
	}

	expr, state := root.schemaState(v, allTypes, nil)
	if state.allowedTypes == 0 {
		d.addErr(errors.Newf(state.pos.Pos(), "constraints are not possible to satisfy"))
	}

	tags := []string{}
	if state.schemaVersionPresent {
		// TODO use cue/literal.String
		tags = append(tags, fmt.Sprintf("schema=%q", state.schemaVersion))
	}

	if name == nil {
		if len(tags) > 0 {
			body := strings.Join(tags, ",")
			a = append(a, &ast.Attribute{
				Text: fmt.Sprintf("@jsonschema(%s)", body)})
		}

		if state.deprecated {
			a = append(a, &ast.Attribute{Text: "@deprecated()"})
		}
	} else {
		if len(tags) > 0 {
			a = append(a, addTag(name, "jsonschema", strings.Join(tags, ",")))
		}

		if state.deprecated {
			a = append(a, addTag(name, "deprecated", ""))
		}
	}

	if name != nil {
		f := &ast.Field{
			Label: name,
			Value: expr,
		}

		a = append(a, f)
	} else if st, ok := expr.(*ast.StructLit); ok && len(st.Elts) > 0 {
		a = append(a, st.Elts...)
	} else {
		a = append(a, &ast.EmbedDecl{Expr: expr})
	}

	if len(a) > 0 {
		state.doc(a[0])
	}
	for i := inner - 1; i >= 0; i-- {
		a = []ast.Decl{&ast.Field{
			Label: ref[i],
			Value: &ast.StructLit{Elts: a},
		}}
		expr = ast.NewStruct(ref[i], expr)
	}

	if root.self == nil {
		return a
	}
	root.self.Elts = a
	return []ast.Decl{
		&ast.EmbedDecl{Expr: d.rootRef()},
		&ast.Field{
			Label: d.rootRef(),
			Value: root.self,
		},
	}
}

// rootRef returns a reference to the top of the file. We do this by
// creating a helper schema:
//
//	_schema: {...}
//	_schema
//
// This is created at the finalization stage, signaled by d.self being
// set, which rootRef does as a side-effect.
func (d *decoder) rootRef() *ast.Ident {
	ident := ast.NewIdent("_schema")
	if d.self == nil {
		d.self = &ast.StructLit{}
	}
	// Ensure that all self-references refer to the same node.
	ident.Node = d.self
	return ident
}

func (d *decoder) errf(n cue.Value, format string, args ...interface{}) ast.Expr {
	d.warnf(n.Pos(), format, args...)
	return &ast.BadExpr{From: n.Pos()}
}

func (d *decoder) warnf(p token.Pos, format string, args ...interface{}) {
	d.addErr(errors.Newf(p, format, args...))
}

func (d *decoder) addErr(err errors.Error) {
	d.errs = errors.Append(d.errs, err)
}

func (d *decoder) number(n cue.Value) ast.Expr {
	return n.Syntax(cue.Final()).(ast.Expr)
}

func (d *decoder) uint(nv cue.Value) ast.Expr {
	n, err := uint64Value(nv)
	if err != nil {
		d.errf(nv, "invalid uint")
	}
	return &ast.BasicLit{
		ValuePos: nv.Pos(),
		Kind:     token.FLOAT,
		Value:    strconv.FormatUint(n, 10),
	}
}

func (d *decoder) boolValue(n cue.Value) bool {
	x, err := n.Bool()
	if err != nil {
		d.errf(n, "invalid bool")
	}
	return x
}

func (d *decoder) string(n cue.Value) ast.Expr {
	return n.Syntax(cue.Final()).(ast.Expr)
}

func (d *decoder) strValue(n cue.Value) (s string, ok bool) {
	s, err := n.String()
	if err != nil {
		d.errf(n, "invalid string")
		return "", false
	}
	return s, true
}

func (d *decoder) regexpValue(n cue.Value) (ast.Expr, bool) {
	s, ok := d.strValue(n)
	if !ok {
		return nil, false
	}
	if !d.checkRegexp(n, s) {
		return nil, false
	}
	return d.string(n), true
}

func (d *decoder) checkRegexp(n cue.Value, s string) bool {
	_, err := syntax.Parse(s, syntax.Perl)
	if err == nil {
		return true
	}
	var regErr *syntax.Error
	if errors.As(err, &regErr) {
		switch regErr.Code {
		case syntax.ErrInvalidPerlOp:
			// It's Perl syntax that we'll never support because the CUE evaluation
			// engine uses Go's regexp implementation and because the missing
			// features are usually not there for good reason (e.g. exponential
			// runtime). In other words, this is a missing feature but not an invalid
			// regular expression as such.
			if d.cfg.StrictFeatures {
				d.errf(n, "unsupported Perl regexp syntax in %q: %v", s, err)
			}
			return false
		case syntax.ErrInvalidCharRange:
			// There are many more character class ranges than Go supports currently
			// (see https://go.dev/issue/14509) so treat an unknown character class
			// range as a feature error rather than a bad regexp.
			// TODO translate names to Go-supported class names when possible.
			if d.cfg.StrictFeatures {
				d.errf(n, "unsupported regexp character class in %q: %v", s, err)
			}
			return false
		}
	}
	d.errf(n, "invalid regexp %q: %v", s, err)
	return false
}

// const draftCutoff = 5

type coreType int

const (
	nullType coreType = iota
	boolType
	numType
	stringType
	arrayType
	objectType

	numCoreTypes
)

var coreToCUE = []cue.Kind{
	nullType:   cue.NullKind,
	boolType:   cue.BoolKind,
	numType:    cue.NumberKind, // Note: both int and float.
	stringType: cue.StringKind,
	arrayType:  cue.ListKind,
	objectType: cue.StructKind,
}

func kindToAST(k cue.Kind) ast.Expr {
	switch k {
	case cue.NullKind:
		// TODO: handle OpenAPI restrictions.
		return ast.NewNull()
	case cue.BoolKind:
		return ast.NewIdent("bool")
	case cue.NumberKind:
		return ast.NewIdent("number")
	case cue.IntKind:
		return ast.NewIdent("int")
	case cue.FloatKind:
		return ast.NewIdent("float")
	case cue.StringKind:
		return ast.NewIdent("string")
	case cue.ListKind:
		return ast.NewList(&ast.Ellipsis{})
	case cue.StructKind:
		return ast.NewStruct(&ast.Ellipsis{})
	}
	panic(fmt.Errorf("unexpected kind %v", k))
}

var coreTypeName = []string{
	nullType:   "null",
	boolType:   "bool",
	numType:    "number",
	stringType: "string",
	arrayType:  "array",
	objectType: "object",
}

type constraintInfo struct {
	// typ is an identifier for the root type, if present.
	// This can be omitted if there are constraints.
	typ         ast.Expr
	constraints []ast.Expr
}

func (c *constraintInfo) setTypeUsed(n cue.Value, t coreType) {
	c.typ = kindToAST(coreToCUE[t])
	setPos(c.typ, n)
	ast.SetRelPos(c.typ, token.NoRelPos)
}

func (c *constraintInfo) add(n cue.Value, x ast.Expr) {
	if !isAny(x) {
		setPos(x, n)
		ast.SetRelPos(x, token.NoRelPos)
		c.constraints = append(c.constraints, x)
	}
}

func (s *state) add(n cue.Value, t coreType, x ast.Expr) {
	s.types[t].add(n, x)
}

func (s *state) setTypeUsed(n cue.Value, t coreType) {
	if int(t) >= len(s.types) {
		panic(fmt.Errorf("type out of range %v/%v", int(t), len(s.types)))
	}
	s.types[t].setTypeUsed(n, t)
}

type state struct {
	*decoder

	isSchema bool // for omitting ellipsis in an ast.File

	up *state

	path []string

	// idRef is used to refer to this schema in case it defines an $id.
	idRef []label

	pos cue.Value

	// The constraints in types represent disjunctions per type.
	types    [numCoreTypes]constraintInfo
	all      constraintInfo // values and oneOf etc.
	nullable *ast.BasicLit  // nullable

	// allowedTypes holds the set of types that
	// this node is allowed to be.
	allowedTypes cue.Kind

	// knownTypes holds the set of types that this node
	// is known to be one of by virtue of the constraints inside
	// all. This is used to avoid adding redundant elements
	// to the disjunction created by [state.finalize].
	knownTypes cue.Kind

	default_     ast.Expr
	examples     []ast.Expr
	title        string
	description  string
	deprecated   bool
	exclusiveMin bool // For OpenAPI and legacy support.
	exclusiveMax bool // For OpenAPI and legacy support.

	// Keep track of whether a $ref keyword is present,
	// because pre-2019-09 schemas ignore sibling keywords
	// to $ref.
	hasRefKeyword bool

	// isRoot holds whether this state is at the root
	// of the schema.
	isRoot bool

	minContains *uint64
	maxContains *uint64

	ifConstraint   cue.Value
	thenConstraint cue.Value
	elseConstraint cue.Value

	schemaVersion        Version
	schemaVersionPresent bool

	id    *url.URL // base URI for $ref
	idPos token.Pos

	definitions []ast.Decl

	// Used for inserting definitions, properties, etc.
	obj *ast.StructLit
	// Complete at finalize.
	fieldRefs map[label]refs

	closeStruct bool
	patterns    []ast.Expr

	list *ast.ListLit

	// listItemsIsArray keeps track of whether the
	// value of the "items" keyword is an array.
	// Without this, we can't distinguish between
	//
	//	"items": true
	//
	// and
	//
	//	"items": []
	listItemsIsArray bool
}

type label struct {
	name  string
	isDef bool
}

type refs struct {
	field *ast.Field
	ident string
	refs  []*ast.Ident
}

func (s *state) idTag() *ast.Attribute {
	return &ast.Attribute{
		At:   s.idPos,
		Text: fmt.Sprintf("@jsonschema(id=%q)", s.id)}
}

func (s *state) structCloseTag() *ast.Attribute {
	return &ast.Attribute{
		Text: astutil.TagCloseStruct,
	}
}

func (s *state) object(n cue.Value) *ast.StructLit {
	if s.obj == nil {
		s.obj = &ast.StructLit{}
		s.add(n, objectType, s.obj)

		if s.id != nil {
			s.obj.Elts = append(s.obj.Elts, s.idTag())
			ast.SetPos(s.obj, s.idPos)
		}
	}
	return s.obj
}

func (s *state) hasConstraints() bool {
	if len(s.all.constraints) > 0 {
		return true
	}
	for _, t := range s.types {
		if len(t.constraints) > 0 {
			return true
		}
	}
	return len(s.patterns) > 0 ||
		s.title != "" ||
		s.description != "" ||
		s.obj != nil ||
		s.id != nil
}

const allTypes = cue.NullKind | cue.BoolKind | cue.NumberKind | cue.IntKind |
	cue.StringKind | cue.ListKind | cue.StructKind

// finalize constructs a CUE type from the collected constraints.
func (s *state) finalize() (e ast.Expr) {
	if s.allowedTypes == 0 {
		// Nothing is possible. This isn't a necessarily a problem, as
		// we might be inside an allOf or oneOf with other valid constraints.
		return bottom()
	}

	conjuncts := []ast.Expr{}
	disjuncts := []ast.Expr{}

	// Sort literal structs and list last for nicer formatting.
	sort.SliceStable(s.types[arrayType].constraints, func(i, j int) bool {
		_, ok := s.types[arrayType].constraints[i].(*ast.ListLit)
		return !ok
	})
	sort.SliceStable(s.types[objectType].constraints, func(i, j int) bool {
		_, ok := s.types[objectType].constraints[i].(*ast.StructLit)
		return !ok
	})

	type excludeInfo struct {
		pos      token.Pos
		typIndex int
	}
	var excluded []excludeInfo

	needsTypeDisjunction := s.allowedTypes != s.knownTypes
	if !needsTypeDisjunction {
		for i, t := range s.types {
			k := coreToCUE[i]
			if len(t.constraints) > 0 && s.allowedTypes&k != 0 {
				// We need to include at least one type-specific
				// constraint in the disjunction.
				needsTypeDisjunction = true
				break
			}
		}
	}

	if needsTypeDisjunction {
		npossible := 0
		nexcluded := 0
		for i, t := range s.types {
			k := coreToCUE[i]
			allowed := s.allowedTypes&k != 0
			switch {
			case len(t.constraints) > 0:
				npossible++
				if !allowed {
					nexcluded++
					for _, c := range t.constraints {
						excluded = append(excluded, excludeInfo{c.Pos(), i})
					}
					continue
				}
				x := ast.NewBinExpr(token.AND, t.constraints...)
				disjuncts = append(disjuncts, x)
			case allowed:
				npossible++
				if s.knownTypes&k != 0 {
					disjuncts = append(disjuncts, kindToAST(k))
				}
			}
		}
		if nexcluded == npossible {
			// All possibilities have been excluded: this is an impossible
			// schema.
			for _, e := range excluded {
				s.addErr(errors.Newf(e.pos,
					"constraint not allowed because type %s is excluded",
					coreTypeName[e.typIndex],
				))
			}
		}
	}
	conjuncts = append(conjuncts, s.all.constraints...)
	obj := s.obj
	if obj == nil {
		obj, _ = s.types[objectType].typ.(*ast.StructLit)
	}
	if obj != nil {
		obj.Elts = append(obj.Elts, &ast.Ellipsis{})
		if s.closeStruct {
			obj.Elts = append(obj.Elts, s.structCloseTag())
		}
	}

	if len(disjuncts) > 0 {
		conjuncts = append(conjuncts, ast.NewBinExpr(token.OR, disjuncts...))
	}

	if len(conjuncts) == 0 {
		// There are no conjuncts, which can only happen when there
		// are no disjuncts, which can only happen when the entire
		// set of disjuncts is redundant with respect to the types
		// already implied by s.all. As we've already checked that
		// s.allowedTypes is non-zero (so we know that
		// it's not bottom) and we need _some_ expression
		// to be part of the subequent syntax, we use top.
		e = top()
	} else {
		e = ast.NewBinExpr(token.AND, conjuncts...)
	}

	a := []ast.Expr{e}
	if s.nullable != nil {
		a = []ast.Expr{s.nullable, e}
	}

outer:
	switch {
	case s.default_ != nil:
		// check conditions where default can be skipped.
		switch x := s.default_.(type) {
		case *ast.ListLit:
			if s.allowedTypes == cue.ListKind && len(x.Elts) == 0 {
				break outer
			}
		}
		a = append(a, &ast.UnaryExpr{Op: token.MUL, X: s.default_})
	}

	e = ast.NewBinExpr(token.OR, a...)

	if len(s.definitions) > 0 {
		if st, ok := e.(*ast.StructLit); ok {
			st.Elts = append(st.Elts, s.definitions...)
		} else {
			st = ast.NewStruct()
			st.Elts = append(st.Elts, &ast.EmbedDecl{Expr: e})
			st.Elts = append(st.Elts, s.definitions...)
			e = st
		}
	}

	s.linkReferences()

	// If an "$id" exists and has not been included in any object constraints
	if s.id != nil && s.obj == nil {
		if st, ok := e.(*ast.StructLit); ok {
			st.Elts = append([]ast.Decl{s.idTag()}, st.Elts...)
			if s.closeStruct {
				st.Elts = append(st.Elts, s.structCloseTag())
			}
		} else {
			st = &ast.StructLit{Elts: []ast.Decl{s.idTag()}}
			if s.closeStruct {
				st.Elts = append(st.Elts, s.structCloseTag())
			}
			st.Elts = append(st.Elts, &ast.EmbedDecl{Expr: e})
			e = st
		}
	}

	// Now that we've expressed the schema as actual syntax,
	// all the allowed types are actually explicit and will not
	// need to be mentioned again.
	s.knownTypes = s.allowedTypes
	return e
}

func (s *state) comment() *ast.CommentGroup {
	// Create documentation.
	doc := strings.TrimSpace(s.title)
	if s.description != "" {
		if doc != "" {
			doc += "\n\n"
		}
		doc += s.description
		doc = strings.TrimSpace(doc)
	}
	// TODO: add examples as well?
	if doc == "" {
		return nil
	}
	return internal.NewComment(true, doc)
}

func (s *state) doc(n ast.Node) {
	doc := s.comment()
	if doc != nil {
		ast.SetComments(n, []*ast.CommentGroup{doc})
	}
}

func (s *state) schema(n cue.Value, idRef ...label) ast.Expr {
	expr, _ := s.schemaState(n, allTypes, idRef)
	// TODO: report unused doc.
	return expr
}

// schemaState returns a new state value derived from s.
// n holds the JSONSchema node to translate to a schema.
// types holds the set of possible types that the value can hold.
// idRef holds the path to the value.
// isLogical specifies whether the caller is a logical operator like anyOf, allOf, oneOf, or not.
func (s0 *state) schemaState(n cue.Value, types cue.Kind, idRef []label) (ast.Expr, *state) {
	s := &state{
		up:            s0,
		schemaVersion: s0.schemaVersion,
		isSchema:      s0.isSchema,
		decoder:       s0.decoder,
		allowedTypes:  types,
		knownTypes:    allTypes,
		idRef:         idRef,
		pos:           n,
		isRoot:        s0.isRoot && n == s0.pos,
	}
	if n.Kind() == cue.BoolKind {
		if vfrom(VersionDraft6).contains(s.schemaVersion) {
			// From draft6 onwards, boolean values signify a schema that always passes or fails.
			return boolSchema(s.boolValue(n)), s
		}
		return s.errf(n, "boolean schemas not supported in %v", s.schemaVersion), s
	}
	if n.Kind() != cue.StructKind {
		return s.errf(n, "schema expects mapping node, found %s", n.Kind()), s
	}

	// do multiple passes over the constraints to ensure they are done in order.
	for pass := 0; pass < numPhases; pass++ {
		s.processMap(n, func(key string, value cue.Value) {
			if pass == 0 && key == "$ref" {
				// Before 2019-19, keywords alongside $ref are ignored so keep
				// track of whether we've seen any non-$ref keywords so we can
				// ignore those keywords. This could apply even when the schema
				// is >=2019-19 because $schema could be used to change the version.
				s.hasRefKeyword = true
			}
			if strings.HasPrefix(key, "x-") {
				// A keyword starting with a leading x- is clearly
				// not intended to be a valid keyword, and is explicitly
				// allowed by OpenAPI. It seems reasonable that
				// this is not an error even with StrictKeywords enabled.
				return
			}
			// Convert each constraint into a either a value or a functor.
			c := constraintMap[key]
			if c == nil {
				if pass == 0 && s.cfg.StrictKeywords {
					// TODO: value is not the correct position, albeit close. Fix this.
					s.warnf(value.Pos(), "unknown keyword %q", key)
				}
				return
			}
			if c.phase != pass {
				return
			}
			if !c.versions.contains(s.schemaVersion) {
				if s.cfg.StrictKeywords {
					s.warnf(value.Pos(), "keyword %q is not supported in JSON schema version %v", key, s.schemaVersion)
				}
				return
			}
			if pass > 0 && !vfrom(VersionDraft2019_09).contains(s.schemaVersion) && s.hasRefKeyword && key != "$ref" {
				// We're using a schema version that ignores keywords alongside $ref.
				//
				// Note that we specifically exclude pass 0 (the pass in which $schema is checked)
				// from this check, because hasRefKeyword is only set in pass 0 and we
				// can get into a self-contradictory situation ($schema says we should
				// ignore keywords alongside $ref, but $ref says we should ignore the $schema
				// keyword itself). We could make that situation an explicit error, but other
				// implementations don't, and it would require an entire extra pass just to do so.
				if s.cfg.StrictKeywords {
					s.warnf(value.Pos(), "ignoring keyword %q alongside $ref", key)
				}
				return
			}
			c.fn(key, value, s)
		})
	}
	constraintIfThenElse(s)

	return s.finalize(), s
}

func (s *state) value(n cue.Value) ast.Expr {
	k := n.Kind()
	switch k {
	case cue.ListKind:
		a := []ast.Expr{}
		for i, _ := n.List(); i.Next(); {
			a = append(a, s.value(i.Value()))
		}
		return setPos(ast.NewList(a...), n)

	case cue.StructKind:
		a := []ast.Decl{}
		s.processMap(n, func(key string, n cue.Value) {
			a = append(a, &ast.Field{
				Label: ast.NewString(key),
				Value: s.value(n),
			})
		})
		return setPos(&ast.StructLit{Elts: a}, n)

	default:
		if !n.IsConcrete() {
			s.errf(n, "invalid non-concrete value")
		}
		return n.Syntax(cue.Final()).(ast.Expr)
	}
}

// processMap processes a yaml node, expanding merges.
//
// TODO: in some cases we can translate merges into CUE embeddings.
// This may also prevent exponential blow-up (as may happen when
// converting YAML to JSON).
func (s *state) processMap(n cue.Value, f func(key string, n cue.Value)) {

	// TODO: intercept references to allow for optimized performance.
	for i, _ := n.Fields(); i.Next(); {
		f(i.Label(), i.Value())
	}
}

func (s *state) listItems(name string, n cue.Value, allowEmpty bool) (a []cue.Value) {
	if n.Kind() != cue.ListKind {
		s.errf(n, `value of %q must be an array, found %v`, name, n.Kind())
	}
	for i, _ := n.List(); i.Next(); {
		a = append(a, i.Value())
	}
	if !allowEmpty && len(a) == 0 {
		s.errf(n, `array for %q must be non-empty`, name)
	}
	return a
}

// excludeFields returns either an empty slice (if decls is empty)
// or a slice containing a CUE expression that can be used to exclude the
// fields of the given declaration in a label expression. For instance, for
//
//	{ foo: 1, bar: int }
//
// it creates a slice holding the expression
//
//	!~ "^(foo|bar)$"
//
// which can be used in a label expression to define types for all fields but
// those existing:
//
//	[!~"^(foo|bar)$"]: string
func excludeFields(decls []ast.Decl) []ast.Expr {
	if len(decls) == 0 {
		return nil
	}
	var buf strings.Builder
	first := true
	buf.WriteString("^(")
	for _, d := range decls {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		str, _, _ := ast.LabelName(f.Label)
		if str != "" {
			if !first {
				buf.WriteByte('|')
			}
			buf.WriteString(regexp.QuoteMeta(str))
			first = false
		}
	}
	buf.WriteString(")$")
	return []ast.Expr{
		&ast.UnaryExpr{Op: token.NMAT, X: ast.NewString(buf.String())},
	}
}

func bottom() ast.Expr {
	return &ast.BottomLit{}
}

func top() ast.Expr {
	return ast.NewIdent("_")
}

func boolSchema(ok bool) ast.Expr {
	if ok {
		return top()
	}
	return bottom()
}

func isAny(s ast.Expr) bool {
	i, ok := s.(*ast.Ident)
	return ok && i.Name == "_"
}

func addTag(field ast.Label, tag, value string) *ast.Field {
	return &ast.Field{
		Label: field,
		Value: top(),
		Attrs: []*ast.Attribute{
			{Text: fmt.Sprintf("@%s(%s)", tag, value)},
		},
	}
}

func setPos(e ast.Expr, v cue.Value) ast.Expr {
	ast.SetPos(e, v.Pos())
	return e
}

// uint64Value is like v.Uint64 except that it
// also allows floating point constants, as long
// as they have no fractional part.
func uint64Value(v cue.Value) (uint64, error) {
	n, err := v.Uint64()
	if err == nil {
		return n, nil
	}
	f, err := v.Float64()
	if err != nil {
		return 0, err
	}
	intPart, fracPart := math.Modf(f)
	if fracPart != 0 {
		return 0, errors.Newf(v.Pos(), "%v is not a whole number", v)
	}
	if intPart < 0 || intPart > math.MaxUint64 {
		return 0, errors.Newf(v.Pos(), "%v is out of bounds", v)
	}
	return uint64(intPart), nil
}
