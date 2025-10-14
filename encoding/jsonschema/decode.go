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
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

const (
	// DefaultRootID is used as the absolute base URI for a schema
	// when no value is provided in [Config.ID].
	DefaultRootID     = "https://" + DefaultRootIDHost
	DefaultRootIDHost = "cue.jsonschema.invalid"
)

// rootDefs defines the top-level name of the map of definitions that do not
// have a valid identifier name.
//
// TODO: find something more principled, like allowing #("a-b").
const rootDefs = "#"

// A decoder converts JSON schema to CUE.
type decoder struct {
	cfg          *Config
	errs         errors.Error
	mapURLErrors map[string]bool

	root   cue.Value
	rootID *url.URL

	// defForValue holds an entry for internal values
	// that are known to map to a defined schema.
	// A nil entry is stored for nodes that have been
	// referred to but we haven't yet seen when walking
	// the schemas.
	defForValue *valueMap[*definedSchema]

	// danglingRefs records the number of nil entries in defForValue,
	// representing the number of references into the internal
	// structure that have not yet been resolved.
	danglingRefs int

	// defs holds the set of named schemas, indexed by URI (both
	// canonical, and root-relative if known), including external
	// schemas that aren't known.
	defs map[string]*definedSchema

	// builder is used to build the final syntax tree as it becomes known.
	builder structBuilder

	// needAnotherPass is set to true when we know that
	// we need another pass through the schema extraction
	// process. This can happen because `MapRef` might choose
	// a different location depending on whether a reference is local
	// or external. We don't know that until we've traversed the
	// entire schema and the `$ref` might be seen before the
	// schema it's referring to. Still more passes might be required
	// if a $ref is found to be referring to a node that would not normally
	// be considered part of the schema data.
	needAnotherPass bool
}

// definedSchema records information for a schema or subschema.
type definedSchema struct {
	// importPath is empty for internal schemas.
	importPath string

	// path holds the location of the schema relative to importPath.
	path cue.Path

	// schema holds the actual syntax for the schema. This
	// is nil if the entry was created by a reference only.
	schema ast.Expr

	// comment holds any doc comment associated with the above schema.
	comment *ast.CommentGroup
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
	var defsRoot cue.Value
	// docRoot represents the root of the actual data, by contrast
	// with the "root" value as specified in [Config.Root] which
	// represents the root of the schemas to be decoded.
	docRoot := v
	if d.cfg.Root != "" {
		rootPath, err := parseRootRef(d.cfg.Root)
		if err != nil {
			d.errf(cue.Value{}, "invalid Config.Root value %q: %v", d.cfg.Root, err)
			return nil
		}
		root := v.LookupPath(rootPath)
		if !root.Exists() && !d.cfg.AllowNonExistentRoot {
			d.errf(v, "root value at path %v does not exist", d.cfg.Root)
			return nil
		}
		if d.cfg.SingleRoot {
			v = root
		} else {
			if !root.Exists() {
				root = v.Context().CompileString("{}")
			}
			if root.Kind() != cue.StructKind {
				d.errf(root, "value at path %v must be struct containing definitions but is actually %v", d.cfg.Root, root)
				return nil
			}
			defsRoot = root
		}
	}

	var rootInfo schemaInfo
	// extraSchemas records any nodes that are referred to
	// but not part of the regular schema traversal.
	var extraSchemas []cue.Value
	// basePass records the last time that any new schemas were
	// added for inspection. This can be set whenever new schemas
	// not part of the regular traversal are found.
	basePass := 0

	for pass := 0; ; pass++ {
		if pass > 10 {
			// Should never happen: the most we should ever see in practice
			// should be 2, but some pathological cases could end up with more.
			d.errf(v, "internal error: too many passes without resolution")
			return nil
		}
		root := &state{
			decoder: d,
			schemaInfo: schemaInfo{
				schemaVersion: d.cfg.DefaultVersion,
				id:            d.rootID,
			},
			isRoot: true,
			pos:    docRoot,
		}

		if defsRoot.Exists() {
			// When d.cfg.Root is non-empty, it points to a struct
			// containing a field for each definition.
			constraintAddDefinitions("schemas", defsRoot, root)
		} else {
			expr, state := root.schemaState(v, allTypes, func(s *state) {
				// We want the top level state to be treated as root even
				// though it's some levels below the actual document top level.
				s.isRoot = true
			})
			if state.allowedTypes == 0 {
				root.errf(v, "constraints are not possible to satisfy")
				return nil
			}
			if !d.builder.put(cue.Path{}, expr, state.comment()) {
				root.errf(v, "duplicate definition at root") // TODO better error message
				return nil
			}
			rootInfo = state
		}
		if d.danglingRefs > 0 && pass == basePass+1 {
			// There are still dangling references but we've been through the
			// schema twice, so we know that there's a reference
			// to a non-schema node. Technically this is not necessarily valid,
			// but we do see this in the wild. This should be rare,
			// so efficiency (re-parsing paths) shouldn't be a great issue.
			for path, def := range d.defForValue.byPath {
				if def != nil {
					continue
				}
				n := d.root.LookupPath(cue.ParsePath(path))
				if !n.Exists() {
					panic("failed to find entry for dangling reference")
				}
				extraSchemas = append(extraSchemas, n)
				basePass = pass
			}
		}
		for _, n := range extraSchemas {
			// As the ID namespace isn't well-defined we treat all such
			// schemas as if they were directly under the root.
			// See https://json-schema.org/draft/2020-12/json-schema-core#section-9.4.2
			root.schema(n)
		}
		if !d.needAnotherPass && d.danglingRefs == 0 {
			break
		}

		d.builder = structBuilder{}
		for _, def := range d.defs {
			def.schema = nil
		}
		d.needAnotherPass = false
	}
	if d.cfg.DefineSchema != nil {
		// Let the caller know about any internal schemas that
		// have been mapped to an external location.
		for _, def := range d.defs {
			if def.schema != nil && def.importPath != "" {
				d.cfg.DefineSchema(def.importPath, def.path, def.schema, def.comment)
			}
		}
	}
	f, err := d.builder.syntax()
	if err != nil {
		d.errf(v, "cannot build final syntax: %v", err)
		return nil
	}
	var preamble []ast.Decl
	if d.cfg.PkgName != "" {
		preamble = append(preamble, &ast.Package{Name: ast.NewIdent(d.cfg.PkgName)})
	}
	if rootInfo.schemaVersionPresent {
		// TODO use cue/literal.String
		// TODO is this actually useful information: why is knowing the schema
		// version of the input useful?
		preamble = append(preamble, &ast.Attribute{
			Text: fmt.Sprintf("@jsonschema(schema=%q)", rootInfo.schemaVersion),
		})
	}
	if rootInfo.deprecated {
		preamble = append(preamble, &ast.Attribute{Text: "@deprecated()"})
	}
	if len(preamble) > 0 {
		f.Decls = append(preamble, f.Decls...)
	}
	return f
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
				// TODO: could fall back to  https://github.com/dlclark/regexp2 instead
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

// ensureDefinition ensures that node n will
// be a defined schema.
func (d *decoder) ensureDefinition(n cue.Value) {
	if _, ok := d.defForValue.lookup(n); !ok {
		d.defForValue.set(n, nil)
		d.danglingRefs++
	}
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

func kindToAST(k cue.Kind, explicitOpen bool) ast.Expr {
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
		if explicitOpen {
			return ast.NewStruct()
		}
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

func (c *constraintInfo) setTypeUsed(n cue.Value, t coreType, explicitOpen bool) {
	c.typ = kindToAST(coreToCUE[t], explicitOpen)
	setPos(c.typ, n)
	ast.SetRelPos(c.typ, token.NoRelPos)
}

func (c *constraintInfo) add(n cue.Value, x ast.Expr) {
	if !isTop(x) {
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
	s.types[t].setTypeUsed(n, t, s.cfg.OpenOnlyWhenExplicit)
}

type state struct {
	*decoder
	schemaInfo

	up *state

	pos cue.Value

	// The constraints in types represent disjunctions per type.
	types    [numCoreTypes]constraintInfo
	all      constraintInfo // values and oneOf etc.
	nullable *ast.BasicLit  // nullable

	exclusiveMin bool // For OpenAPI and legacy support.
	exclusiveMax bool // For OpenAPI and legacy support.

	// isRoot holds whether this state is at the root
	// of the schema.
	isRoot bool

	minContains *uint64
	maxContains *uint64

	ifConstraint   cue.Value
	thenConstraint cue.Value
	elseConstraint cue.Value

	definitions []ast.Decl

	// Used for inserting definitions, properties, etc.
	obj  *ast.StructLit
	objN cue.Value // used for adding obj to constraints

	patterns []ast.Expr

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

	// The following fields are used when the version is
	// [VersionKubernetesCRD] to check that "properties" and
	// "additionalProperties" may not be specified together.
	hasProperties           bool
	hasAdditionalProperties bool

	// Keep track of whether "items" and "type": "array" have been specified, because
	// in OpenAPI it's mandatory when "type" is "array".
	hasItems bool
	isArray  bool

	// Keep track of whether a $ref keyword is present,
	// because pre-2019-09 schemas ignore sibling keywords
	// to $ref.
	hasRefKeyword bool

	// Keep track of whether we're preserving existing fields,
	// which is preserved recursively by default, and is
	// reset within properties or additionalProperties.
	preserveUnknownFields bool

	// k8sResourceKind and k8sAPIVersion record values from the
	// x-kubernetes-group-version-kind keyword
	// for the kind and apiVersion properties respectively.
	k8sResourceKind string
	k8sAPIVersion   string

	// Keep track of whether the object has been explicitly
	// closed or opened (see [Config.OpenOnlyWhenExplicit]).
	openness openness
}

type openness int

const (
	implicitlyOpen   openness = iota
	explicitlyOpen            // explicitly opened, e.g. additionalProperties: true
	explicitlyClosed          // explicitly closed, e.g. additionalProperties: false
	allFieldsCovered          // complete pattern present, e.g. additionalProperties: type: string
)

// schemaInfo holds information about a schema
// after it has been created.
type schemaInfo struct {
	// allowedTypes holds the set of types that
	// this node is allowed to be.
	allowedTypes cue.Kind

	// knownTypes holds the set of types that this node
	// is known to be one of by virtue of the constraints inside
	// all. This is used to avoid adding redundant elements
	// to the disjunction created by [state.finalize].
	knownTypes cue.Kind

	title       string
	description string

	// id holds the absolute URI of the schema if has a $id field .
	// It's the base URI for $ref or nested $id fields.
	id         *url.URL
	deprecated bool

	schemaVersion        Version
	schemaVersionPresent bool

	hasConstraints bool
}

func (s *state) idTag() *ast.Attribute {
	return &ast.Attribute{Text: fmt.Sprintf("@jsonschema(id=%q)", s.id)}
}

func (s *state) object(n cue.Value) *ast.StructLit {
	if s.obj == nil {
		s.obj = &ast.StructLit{}
		s.objN = n
	}
	return s.obj
}

func (s *state) finalizeObject() {
	if s.obj == nil && s.schemaVersion == VersionKubernetesCRD && (s.allowedTypes&cue.StructKind) != 0 && s.preserveUnknownFields {
		// When x-kubernetes-preserve-unknown-fields is set, we need
		// an explicit ellipsis even though kindToAST won't have added
		// one, so make sure there's an object.
		_ = s.object(s.pos)
	}
	if s.obj == nil {
		return
	}
	if s.preserveUnknownFields {
		s.openness = explicitlyOpen
	}
	var e ast.Expr = s.obj
	if s.cfg.OpenOnlyWhenExplicit && s.openness == implicitlyOpen {
		// Nothing to do: the struct is implicitly open but
		// we've been directed to leave it like that.
	} else if s.openness == allFieldsCovered {
		// Nothing to do: there is a pattern constraint that covers all
		// possible fields.
	} else if s.openness == explicitlyClosed {
		e = ast.NewCall(ast.NewIdent("close"), s.obj)
	} else {
		s.obj.Elts = append(s.obj.Elts, &ast.Ellipsis{})
	}
	s.add(s.objN, objectType, e)
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

const allTypes = cue.BoolKind |
	cue.ListKind |
	cue.NullKind |
	cue.NumberKind |
	cue.IntKind |
	cue.StringKind |
	cue.StructKind

// finalize constructs CUE syntax from the collected constraints.
func (s *state) finalize() (e ast.Expr) {
	if s.allowedTypes == 0 {
		// Nothing is possible. This isn't a necessarily a problem, as
		// we might be inside an allOf or oneOf with other valid constraints.
		return errorDisallowed()
	}

	s.finalizeObject()

	conjuncts := []ast.Expr{}
	disjuncts := []ast.Expr{}

	// Sort literal structs and list last for nicer formatting.
	// Use a stable sort so that the relative order of constraints
	// is otherwise kept as-is, for the sake of deterministic output.
	slices.SortStableFunc(s.types[arrayType].constraints, func(a, b ast.Expr) int {
		_, aList := a.(*ast.ListLit)
		_, bList := b.(*ast.ListLit)
		return cmpBool(aList, bList)
	})
	slices.SortStableFunc(s.types[objectType].constraints, func(a, b ast.Expr) int {
		_, aStruct := a.(*ast.StructLit)
		_, bStruct := b.(*ast.StructLit)
		return cmpBool(aStruct, bStruct)
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
					disjuncts = append(disjuncts, kindToAST(k, s.cfg.OpenOnlyWhenExplicit))
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

	// If an "$id" exists, make sure it's present in the output.
	if s.id != nil {
		if st, ok := e.(*ast.StructLit); ok {
			st.Elts = append([]ast.Decl{s.idTag()}, st.Elts...)
		} else {
			e = &ast.StructLit{Elts: []ast.Decl{s.idTag(), &ast.EmbedDecl{Expr: e}}}
		}
	}

	// Now that we've expressed the schema as actual syntax,
	// all the allowed types are actually explicit and will not
	// need to be mentioned again.
	s.knownTypes = s.allowedTypes
	return e
}

// cmpBool returns
//
//	-1 if x is less than y,
//	 0 if x equals y,
//	+1 if x is greater than y,
//
// where false is ordered before true.
func cmpBool(x, y bool) int {
	switch {
	case !x && y:
		return -1
	case x && !y:
		return +1
	default:
		return 0
	}
}

func (s schemaInfo) comment() *ast.CommentGroup {
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

func (s *state) schema(n cue.Value) ast.Expr {
	expr, _ := s.schemaState(n, allTypes, nil)
	return expr
}

// schemaState returns a new state value derived from s.
// n holds the JSONSchema node to translate to a schema.
// types holds the set of possible types that the value can hold.
//
// If init is not nil, it is called on the newly created state value
// before doing anything else.
func (s0 *state) schemaState(n cue.Value, types cue.Kind, init func(*state)) (expr ast.Expr, info schemaInfo) {
	s := &state{
		up: s0,
		schemaInfo: schemaInfo{
			schemaVersion: s0.schemaVersion,
			allowedTypes:  types,
			knownTypes:    allTypes,
		},
		decoder:               s0.decoder,
		pos:                   n,
		isRoot:                s0.isRoot && n == s0.pos,
		preserveUnknownFields: s0.preserveUnknownFields,
	}
	if init != nil {
		init(s)
	}
	defer func() {
		// Perhaps replace the schema expression with a reference.
		expr = s.maybeDefine(expr, info)
	}()
	if n.Kind() == cue.BoolKind {
		if s.schemaVersion.is(vfrom(VersionDraft6)) {
			// From draft6 onwards, boolean values signify a schema that always passes or fails.
			// TODO if false, set s.allowedTypes and s.knownTypes to zero?
			return boolSchema(s.boolValue(n)), s.schemaInfo
		}
		return s.errf(n, "boolean schemas not supported in %v", s.schemaVersion), s.schemaInfo
	}
	if n.Kind() != cue.StructKind {
		return s.errf(n, "schema expects mapping node, found %s", n.Kind()), s.schemaInfo
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
			// Convert each constraint into a either a value or a functor.
			c := constraintMap[key]
			if c == nil {
				if strings.HasPrefix(key, "x-") {
					// A keyword starting with a leading x- is clearly
					// not intended to be a valid keyword, and is explicitly
					// allowed by OpenAPI. It seems reasonable that
					// this is not an error even with StrictKeywords enabled.
					return
				}
				if pass == 0 && s.cfg.StrictKeywords {
					// TODO: value is not the correct position, albeit close. Fix this.
					s.warnUnrecognizedKeyword(key, value, "unknown keyword %q", key)
				}
				return
			}
			if c.phase != pass {
				return
			}
			if !s.schemaVersion.is(c.versions) {
				s.warnUnrecognizedKeyword(key, value, "keyword %q is not supported in JSON schema version %v", key, s.schemaVersion)
				return
			}
			if pass > 0 && !s.schemaVersion.is(vfrom(VersionDraft2019_09)) && s.hasRefKeyword && key != "$ref" {
				// We're using a schema version that ignores keywords alongside $ref.
				//
				// Note that we specifically exclude pass 0 (the pass in which $schema is checked)
				// from this check, because hasRefKeyword is only set in pass 0 and we
				// can get into a self-contradictory situation ($schema says we should
				// ignore keywords alongside $ref, but $ref says we should ignore the $schema
				// keyword itself). We could make that situation an explicit error, but other
				// implementations don't, and it would require an entire extra pass just to do so.
				s.warnUnrecognizedKeyword(key, value, "ignoring keyword %q alongside $ref", key)
				return
			}
			c.fn(key, value, s)
		})
		if s.schemaVersion == VersionKubernetesCRD && s.isRoot {
			// The root of a CRD is always a resource, so treat it as if it contained
			// the x-kubernetes-embedded-resource keyword
			// TODO remove this behavior now that we have an explicit
			// ExtractCRDs function which does a better job at doing this.
			c := constraintMap["x-kubernetes-embedded-resource"]
			if c.phase != pass {
				continue
			}
			// Note: there is no field value for the embedded-resource keyword,
			// but it's not actually used except for its position so passing
			// the parent object should work fine.
			c.fn("x-kubernetes-embedded-resource", n, s)
		}
	}
	if s.id != nil {
		// If there's an ID, it can be referred to.
		s.ensureDefinition(s.pos)
	}
	constraintIfThenElse(s)
	if s.schemaVersion == VersionKubernetesCRD {
		if s.hasProperties && s.hasAdditionalProperties {
			s.errf(n, "additionalProperties may not be combined with properties in %v", s.schemaVersion)
		}
	}
	if s.schemaVersion.is(openAPILike) {
		if s.isArray && !s.hasItems {
			// From https://github.com/OAI/OpenAPI-Specification/blob/3.0.0/versions/3.0.0.md#schema-object
			// "`items` MUST be present if the `type` is `array`."
			s.errf(n, `"items" must be present when the "type" is "array" in %v`, s.schemaVersion)
		}
	}

	schemaExpr := s.finalize()
	s.schemaInfo.hasConstraints = s.hasConstraints()
	return schemaExpr, s.schemaInfo
}

func (s *state) warnUnrecognizedKeyword(key string, n cue.Value, msg string, args ...any) {
	if !s.cfg.StrictKeywords {
		return
	}
	if s.schemaVersion.is(openAPILike) && strings.HasPrefix(key, "x-") {
		// Unimplemented x- keywords are allowed even with strict keywords
		// under OpenAPI-like versions, because those versions enable
		// strict keywords by default.
		return
	}
	s.errf(n, msg, args...)
}

// maybeDefine checks whether we might need a definition
// for n given its actual schema syntax expression. If
// it does, it creates the definition as appropriate and returns
// an expression that refers to that definition; if not,
// it just returns expr itself.
// TODO also report whether the schema has been defined at a place
// where it can be unified with something else?
func (s *state) maybeDefine(expr ast.Expr, info schemaInfo) ast.Expr {
	def := s.definedSchemaForNode(s.pos)
	if def == nil || len(def.path.Selectors()) == 0 {
		return expr
	}
	def.schema = expr
	def.comment = info.comment()
	if def.importPath == "" {
		// It's a local definition that's not at the root.
		if !s.builder.put(def.path, expr, s.comment()) {
			s.errf(s.pos, "redefinition of schema CUE path %v", def.path)
			return expr
		}
	}
	return s.refExpr(s.pos, def.importPath, def.path)
}

// definedSchemaForNode returns the definedSchema value
// for the given node in the JSON schema, or nil
// if the node does not need a definition.
func (s *state) definedSchemaForNode(n cue.Value) *definedSchema {
	def, ok := s.defForValue.lookup(n)
	if !ok {
		return nil
	}
	if def != nil {
		// We've either made a definition in a previous pass
		// or it's a redefinition.
		// TODO if it's a redefinition, error.
		return def
	}
	// This node has been referred to but not actually defined. We'll
	// need another pass to sort out the reference even though the
	// reference is no longer dangling.
	s.needAnotherPass = true

	def = s.addDefinition(n)
	if def == nil {
		return nil
	}
	s.defForValue.set(n, def)
	s.danglingRefs--
	return def
}

func (s *state) addDefinition(n cue.Value) *definedSchema {
	var loc SchemaLoc
	schemaRoot := s.schemaRoot()
	loc.ID = ref(*schemaRoot.id)
	loc.ID.Fragment = mustCUEPathToJSONPointer(relPath(n, schemaRoot.pos))
	idStr := loc.ID.String()
	def, ok := s.defs[idStr]
	if ok {
		// We've already got a definition for this ID.
		// TODO if it's been defined in the same pass, then it's a redefinition
		// s.errf(n, "redefinition of schema %s at %v", idStr, n.Path())
		return def
	}
	loc.IsLocal = true
	loc.Path = relPath(n, s.root)
	importPath, path, err := s.cfg.MapRef(loc)
	if err != nil {
		s.errf(n, "cannot get reference for %v: %v", loc, err)
		return nil
	}
	def = &definedSchema{
		importPath: importPath,
		path:       path,
	}
	s.defs[idStr] = def
	return def
}

// refExpr returns a CUE expression to refer to the given path within the given
// imported CUE package. If importPath is empty, it returns a reference
// relative to the root of the schema being generated.
func (s *state) refExpr(n cue.Value, importPath string, path cue.Path) ast.Expr {
	if importPath == "" {
		// Internal reference
		expr, err := s.builder.getRef(path)
		if err != nil {
			s.errf(n, "cannot generate reference: %v", err)
			return nil
		}
		return expr
	}
	// External reference
	ip := ast.ParseImportPath(importPath)
	if ip.Qualifier == "" {
		// TODO choose an arbitrary name here.
		s.errf(n, "cannot determine package name from import path %q", importPath)
		return nil
	}
	ident := ast.NewIdent(ip.Qualifier)
	ident.Node = &ast.ImportSpec{Path: ast.NewString(importPath)}
	expr, err := pathRefSyntax(path, ident)
	if err != nil {
		s.errf(n, "cannot determine CUE path: %v", err)
		return nil
	}
	return expr
}

func (s *state) constValue(n cue.Value) ast.Expr {
	k := n.Kind()
	switch k {
	case cue.ListKind:
		a := []ast.Expr{}
		for i, _ := n.List(); i.Next(); {
			a = append(a, s.constValue(i.Value()))
		}
		return setPos(ast.NewList(a...), n)

	case cue.StructKind:
		a := []ast.Decl{}
		s.processMap(n, func(key string, n cue.Value) {
			a = append(a, &ast.Field{
				Label:      ast.NewString(key),
				Value:      s.constValue(n),
				Constraint: token.NOT,
			})
		})
		return setPos(ast.NewCall(ast.NewIdent("close"), &ast.StructLit{Elts: a}), n)
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
		f(i.Selector().Unquoted(), i.Value())
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

func errorDisallowed() ast.Expr {
	return ast.NewCall(ast.NewIdent("error"), ast.NewString("disallowed"))
}

func isErrorCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	target, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	return target.Name == "error"
}

func top() ast.Expr {
	return ast.NewIdent("_")
}

func boolSchema(ok bool) ast.Expr {
	if ok {
		return top()
	}
	return errorDisallowed()
}

func isTop(s ast.Expr) bool {
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
