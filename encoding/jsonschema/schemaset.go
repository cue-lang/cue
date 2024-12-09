package jsonschema

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// schemaSet holds a membership set of schemas indexed by position
// (a.k.a. JSON Pointer)
// It's designed so that it's cheap in the common case that a lookup
// returns false. It does that by using the source position of the
// schema as a first probe. Determining the source location of a value
// is very cheap, and in most practical cases, JSON Schema is being
// extracted from concrete JSON where there will be a bijective mapping
// between source location and path.
type schemaSet struct {
	byPos  map[token.Pos]bool
	byPath map[string]bool
}

func newSchemaSet() *schemaSet {
	return &schemaSet{
		byPos:  make(map[token.Pos]bool),
		byPath: make(map[string]bool),
	}
}

func (m *schemaSet) len() int {
	return len(m.byPath)
}

func (m *schemaSet) set(key cue.Value) {
	m.byPos[key.Pos()] = true
	m.byPath[key.Path().String()] = true
}

func (m *schemaSet) get(key cue.Value) bool {
	if !m.byPos[key.Pos()] {
		return false
	}
	return m.byPath[key.Path().String()]
}

// structBuilder builds a struct value incrementally by
// putting values for its component paths.
// The [structBuilder.getRef] method can be used
// to obtain reliable references into the resulting struct.
type structBuilder struct {
	// isPresent records whether the node has actually been created as the
	// target of a put call. Non-present entries are created as the
	// result of getRef calls.
	isPresent bool

	// value holds the value associated with the node, not including the
	// entries added underneath it.
	value ast.Expr

	// entries holds the entries isPresent under this node.
	entries map[cue.Selector]*structBuilder

	// refIdents records all the identifiers that refer to this node. As
	// the [Ident.Node] field needs to refer to the field value rather
	// than the field label, and we don't know that until the syntax
	// method has been invoked, we fix up the [Ident.Node] fields when
	// that happens.
	refIdents []*ast.Ident
}

// put associates value with the given path. It reports whether
// the value was successfully put, returning false if a value
// already exists for the path.
func (b *structBuilder) put(p cue.Path, value ast.Expr) bool {
	e := b.entryForPath(p, true)
	if e.value != nil {
		// redefinition
		return false
	}
	e.value = value
	return true
}

const rootIdentName = "_schema"

// getRef returns CUE syntax for a reference to the path p within b.
// It ensures that, if possible, the identifier at the start of the
// reference expression has the correct target node.
func (b *structBuilder) getRef(p cue.Path) (ast.Expr, error) {
	if err := p.Err(); err != nil {
		return nil, fmt.Errorf("invalid path %v", p)
	}
	sels := p.Selectors()
	if len(sels) == 0 {
		// There's no natural name for the root element,
		// so use an arbitrary one.
		ref := ast.NewIdent(rootIdentName)
		b.refIdents = append(b.refIdents, ref)
		return ref, nil
	}
	base, err := labelForSelector(sels[0])
	if err != nil {
		return nil, err
	}
	baseExpr, ok := base.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("initial element of path %q cannot be expressed as an identifier", p)
	}
	// The base identifier needs to refer to the
	// first element of the path; the rest doesn't matter.
	baseEntry := b.entryForPath(cue.MakePath(sels[:1]...), false)
	baseEntry.refIdents = append(baseEntry.refIdents, baseExpr)
	return pathRefSyntax(cue.MakePath(sels[1:]...), baseExpr)
}

func (b *structBuilder) entryForPath(p cue.Path, putting bool) *structBuilder {
	if err := p.Err(); err != nil {
		panic(fmt.Errorf("invalid path %v", p))
	}
	sels := p.Selectors()

	b.isPresent = b.isPresent || putting
	for _, sel := range sels {
		if b.entries == nil {
			b.entries = make(map[cue.Selector]*structBuilder)
		}
		b1, ok := b.entries[sel]
		if !ok {
			b1 = &structBuilder{}
			b.entries[sel] = b1
		}
		b = b1
		b.isPresent = b.isPresent || putting
	}
	return b
}

func (b *structBuilder) syntax() (ast.Expr, error) {
	if !b.isPresent {
		return nil, fmt.Errorf("no syntax to produce")
	}
	s, err := b.syntax0()
	if err != nil {
		return nil, err
	}
	if len(b.refIdents) == 0 {
		// No reference to root, so can return as is.
		return s, nil
	}
	rootRef, err := b.getRef(cue.Path{})
	if err != nil {
		return nil, err
	}
	b.updateIdents(s)
	return &ast.StructLit{
		Elts: []ast.Decl{
			&ast.EmbedDecl{Expr: rootRef},
			&ast.Field{
				Label: ast.NewIdent(rootIdentName),
				Value: s,
			},
		},
	}, nil
}

func (b *structBuilder) syntax0() (ast.Expr, error) {
	hasEntry := false
	for _, e := range b.entries {
		if e.isPresent {
			hasEntry = true
			break
		}
	}
	if !hasEntry {
		b.updateIdents(b.value)
		return b.value, nil
	}
	s := &ast.StructLit{}
	if b.value != nil {
		s.Elts = append(s.Elts, &ast.EmbedDecl{Expr: b.value})
	}
	for _, sel := range slices.SortedFunc(maps.Keys(b.entries), cmpSelector) {
		entry := b.entries[sel]
		if !entry.isPresent {
			continue
		}
		value, err := entry.syntax0()
		if err != nil {
			return nil, err
		}
		label, err := labelForSelector(sel)
		if err != nil {
			return nil, err // TODO we can do better for string fields that aren't valid idents.
		}
		s.Elts = append(s.Elts, &ast.Field{
			Label: label,
			Value: value,
		})
	}
	b.updateIdents(s)
	return s, nil
}

func (b *structBuilder) updateIdents(finalValue ast.Node) {
	for _, id := range b.refIdents {
		id.Node = finalValue
	}
}

func cmpSelector(s1, s2 cue.Selector) int {
	if s1 == s2 {
		return 0
	}
	if s1.Type() != s2.Type() {
		return cmp.Compare(s1.Type(), s2.Type())
	}
	return cmp.Compare(s1.String(), s2.String())
}
