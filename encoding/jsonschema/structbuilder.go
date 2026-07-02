package jsonschema

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// structBuilder builds a struct value incrementally by
// putting values for its component paths.
// The [structBuilder.getRef] method can be used
// to obtain reliable references into the resulting struct.
type structBuilder struct {
	root structBuilderNode

	// seeding is set while base data is being added (see
	// [structBuilder.addBase]): nodes created while it is set are stamped
	// with a sequence number recording their relative order.
	seeding bool

	// seq holds the most recently allocated sequence number.
	seq int

	// refIdents records all the identifiers that refer to entries
	// at the top level of the struct, keyed by the selector
	// they're referring to.
	//
	// The [Ident.Node] field needs to refer to the field value rather
	// than the field label, and we don't know that until the syntax
	// method has been invoked, so we fix up the [Ident.Node] fields when
	// that happens.
	refIdents map[cue.Selector][]*ast.Ident

	// rootRefIdents is like refIdents but for references to the
	// struct root itself.
	rootRefIdents []*ast.Ident
}

// structBuilderNode represents one node in the tree of values
// being built.
type structBuilderNode struct {
	// value holds the value associated with the node, if any.
	// This does not include entries added underneath it by
	// [structBuilder.put].
	value ast.Expr

	// seq holds the node's ordering sequence number when the node was
	// created from base data, and zero otherwise. Base nodes render in
	// sequence order, before any other node in the same struct, and as
	// nested struct literals rather than flattened path declarations.
	seq int

	// comment holds any doc comment associated with the value.
	comment *ast.CommentGroup

	// entries holds the children of this node, keyed by the
	// name of each child's struct field selector.
	entries map[cue.Selector]*structBuilderNode
}

// addBase seeds the builder with the contents of the given file, recording
// the order of its fields so that [structBuilder.syntax] preserves it. The
// file must consist only of regular fields with identifier or simple string
// labels, holding values composed of struct literals, list literals and
// other expressions, which are treated as opaque leaf values. A field with
// the placeholder value _ records the position of its path without
// associating a value with it: a value put at that path later takes its
// place, and the placeholder remains if none is.
func (b *structBuilder) addBase(f *ast.File) error {
	b.seeding = true
	defer func() {
		b.seeding = false
	}()
	for _, d := range f.Decls {
		fld, ok := d.(*ast.Field)
		if !ok {
			return fmt.Errorf("unsupported declaration type %T in base data", d)
		}
		if err := b.addBaseField(nil, fld); err != nil {
			return err
		}
	}
	return nil
}

func (b *structBuilder) addBaseField(prefix []cue.Selector, fld *ast.Field) error {
	if fld.Constraint != token.ILLEGAL {
		return fmt.Errorf("unsupported field constraint %q in base data", fld.Constraint)
	}
	if fld.Alias != nil {
		return fmt.Errorf("unsupported field alias in base data")
	}
	var sel cue.Selector
	switch lbl := fld.Label.(type) {
	case *ast.Ident:
		name := lbl.Name
		if name == "_" || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "_") {
			return fmt.Errorf("unsupported label %q in base data", name)
		}
		sel = cue.Str(name)
	case *ast.BasicLit:
		if lbl.Kind != token.STRING {
			return fmt.Errorf("unsupported label %v in base data", lbl.Value)
		}
		name, err := literal.Unquote(lbl.Value)
		if err != nil {
			return fmt.Errorf("invalid label %v in base data: %v", lbl.Value, err)
		}
		sel = cue.Str(name)
	default:
		return fmt.Errorf("unsupported label type %T in base data", fld.Label)
	}
	return b.addBaseExpr(append(prefix, sel), fld.Value, baseDocComment(fld))
}

func (b *structBuilder) addBaseExpr(path []cue.Selector, e ast.Expr, comment *ast.CommentGroup) error {
	cuePath := cue.MakePath(path...)
	switch e := e.(type) {
	case *ast.StructLit:
		if len(e.Elts) == 0 {
			// Preserve an empty struct as a leaf value.
			break
		}
		b.setBaseComment(cuePath, comment)
		for _, elt := range e.Elts {
			fld, ok := elt.(*ast.Field)
			if !ok {
				return fmt.Errorf("unsupported declaration type %T in base data", elt)
			}
			if err := b.addBaseField(path, fld); err != nil {
				return err
			}
		}
		return nil
	case *ast.ListLit:
		if len(e.Elts) == 0 {
			// Preserve an empty list as a leaf value.
			break
		}
		b.setBaseComment(cuePath, comment)
		for i, elt := range e.Elts {
			if err := b.addBaseExpr(append(path, cue.Index(i)), elt, nil); err != nil {
				return err
			}
		}
		return nil
	case *ast.Ident:
		if e.Name == "_" {
			// A placeholder: record the node position only.
			b.setBaseComment(cuePath, comment)
			return nil
		}
	}
	if !b.put(cuePath, e, comment) {
		return fmt.Errorf("duplicate value in base data at %v", cuePath)
	}
	return nil
}

// setBaseComment ensures the node at the given path exists (stamping its
// order) and associates the given doc comment with it if it has none.
func (b *structBuilder) setBaseComment(p cue.Path, comment *ast.CommentGroup) {
	e := b.entryForPath(p)
	if e.comment == nil {
		e.comment = comment
	}
}

// baseDocComment returns the doc comment associated with n, if any.
func baseDocComment(n ast.Node) *ast.CommentGroup {
	// TODO think about what it means to have multiple doc comment
	// groups here.
	cg := ast.DocComments(n)
	if len(cg) > 0 {
		return cg[0]
	}
	return nil
}

// put associates value with the given path. It reports whether
// the value was successfully put, returning false if a value
// already exists for the path.
func (b *structBuilder) put(p cue.Path, value ast.Expr, comment *ast.CommentGroup) bool {
	e := b.entryForPath(p)
	if e.value != nil {
		// redefinition
		return false
	}
	e.value = value
	if e.comment == nil {
		e.comment = comment
	}
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

		b.rootRefIdents = append(b.rootRefIdents, ref)
		return ref, nil
	}
	base, err := labelForSelector(sels[0])
	if err != nil {
		return nil, err
	}
	baseExpr, ok := base.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("initial element of path %q must be expressed as an identifier", p)
	}
	// The base identifier needs to refer to the
	// first element of the path; the rest doesn't matter.
	if b.refIdents == nil {
		b.refIdents = make(map[cue.Selector][]*ast.Ident)
	}
	b.refIdents[sels[0]] = append(b.refIdents[sels[0]], baseExpr)
	return pathRefSyntax(cue.MakePath(sels[1:]...), baseExpr)
}

func (b *structBuilder) entryForPath(p cue.Path) *structBuilderNode {
	if err := p.Err(); err != nil {
		panic(fmt.Errorf("invalid path %v", p))
	}
	sels := p.Selectors()

	n := &b.root
	for _, sel := range sels {
		if n.entries == nil {
			n.entries = make(map[cue.Selector]*structBuilderNode)
		}
		n1, ok := n.entries[sel]
		if !ok {
			n1 = &structBuilderNode{}
			if b.seeding {
				b.seq++
				n1.seq = b.seq
			}
			n.entries[sel] = n1
		}
		n = n1
	}
	return n
}

// syntax returns an expression for the whole struct.
func (b *structBuilder) syntax() (*ast.File, error) {
	var db declBuilder
	if err := b.appendDecls(&b.root, &db); err != nil {
		return nil, err
	}
	// Fix up references (we don't need to do this if the root is a single
	// expression, because that only happens when there's nothing
	// to refer to).
	for _, decl := range db.decls {
		if f, ok := decl.(*ast.Field); ok {
			for _, ident := range b.refIdents[selectorForLabel(f.Label)] {
				ident.Node = f.Value
			}
		}
	}

	var f *ast.File
	if len(b.rootRefIdents) == 0 {
		// No reference to root, so can use declarations as they are.
		f = &ast.File{
			Decls: db.decls,
		}
	} else {
		rootExpr := exprFromDecls(db.decls)
		// Fix up references to the root node.
		for _, ident := range b.rootRefIdents {
			ident.Node = rootExpr
		}
		rootRef, err := b.getRef(cue.Path{})
		if err != nil {
			return nil, err
		}
		f = &ast.File{
			Decls: []ast.Decl{
				&ast.EmbedDecl{Expr: rootRef},
				&ast.Field{
					Label: ast.NewIdent(rootIdentName),
					Value: rootExpr,
				},
			},
		}
	}
	if b.root.comment != nil {
		// If Doc is true, as it is for comments on fields,
		// then the CUE formatting will join it to any import
		// directives, which is not what we want, as then
		// it will no longer appear as a comment on the file.
		// So set Doc to false to prevent that happening.
		b.root.comment.Doc = false
		ast.SetComments(f, []*ast.CommentGroup{b.root.comment})
	}

	return f, nil
}

func (b *structBuilder) appendDecls(n *structBuilderNode, db *declBuilder) (_err error) {
	if isList, err := isListNode(n); err != nil {
		return fmt.Errorf("at path %v: %v", cue.MakePath(db.path...), err)
	} else if isList {
		if n.value != nil {
			return fmt.Errorf("cannot combine value and list elements at path %v", cue.MakePath(db.path...))
		}
		list, err := b.listSyntax(n)
		if err != nil {
			return err
		}
		db.decls, _err = appendField(db.decls, cue.MakePath(db.path...), list, n.comment)
		return _err
	}
	if n.seq != 0 {
		return b.appendBaseDecls(n, db)
	}
	if n.value != nil {
		if len(n.entries) > 0 {
			// We've got a value associated with this node and also some entries inside it.
			// We need to make a struct literal to hold the value and those entries
			// because the value might be scalar and
			//	#x: string
			//	#x: #y: bool
			// is not allowed.
			//
			// So make a new declBuilder instance with a fresh empty path
			// to build the declarations to put inside a struct literal.
			db0 := db
			db = &declBuilder{}
			defer func() {
				if _err != nil {
					return
				}
				db0.decls, _err = appendField(db0.decls, cue.MakePath(db0.path...), exprFromDecls(db.decls), n.comment)
			}()
		}
		// Note: when the path is empty, we rely on the outer level
		// to add any doc comment required.
		db.decls, _err = appendField(db.decls, cue.MakePath(db.path...), n.value, n.comment)
		if _err != nil {
			return _err
		}
	}
	for _, sel := range b.sortedEntries(n) {
		entry := n.entries[sel]
		db.pushPath(sel)
		err := b.appendDecls(entry, db)
		db.popPath()
		if err != nil {
			return err
		}
	}
	return nil
}

// appendBaseDecls renders a node that was created from base data (see
// [structBuilder.addBase]): as a single field holding the node's contents,
// preserving the base field order, rather than as the flattened
// one-declaration-per-value form used for schema definitions.
func (b *structBuilder) appendBaseDecls(n *structBuilderNode, db *declBuilder) error {
	if len(n.entries) == 0 {
		v := n.value
		if v == nil {
			// A placeholder position that nothing was put into.
			v = ast.NewIdent("_")
		}
		var err error
		db.decls, err = appendField(db.decls, cue.MakePath(db.path...), v, n.comment)
		return err
	}
	if len(n.entries) == 1 && n.value == nil {
		// A single entry reads best as part of a flattened path
		// declaration (a: b: c: value).
		for sel, entry := range n.entries {
			db.pushPath(sel)
			err := b.appendDecls(entry, db)
			db.popPath()
			if err != nil {
				return err
			}
		}
		return nil
	}
	inner := &declBuilder{}
	if n.value != nil {
		inner.decls = appendDeclsExpr(inner.decls, n.value)
	}
	for _, sel := range b.sortedEntries(n) {
		inner.pushPath(sel)
		err := b.appendDecls(n.entries[sel], inner)
		inner.popPath()
		if err != nil {
			return err
		}
	}
	// Leave the layout of the fields to the formatter.
	for _, d := range inner.decls {
		ast.SetRelPos(d, token.NoRelPos)
	}
	var err error
	db.decls, err = appendField(db.decls, cue.MakePath(db.path...), &ast.StructLit{Elts: inner.decls}, n.comment)
	return err
}

// sortedEntries returns the entry selectors of n in rendering order: entries
// created from base data first, in base order, followed by the rest in
// deterministic selector order.
func (b *structBuilder) sortedEntries(n *structBuilderNode) []cue.Selector {
	return slices.SortedFunc(maps.Keys(n.entries), func(s1, s2 cue.Selector) int {
		n1, n2 := n.entries[s1], n.entries[s2]
		switch {
		case n1.seq != 0 && n2.seq != 0:
			return cmp.Compare(n1.seq, n2.seq)
		case n1.seq != 0:
			return -1
		case n2.seq != 0:
			return 1
		}
		return cmpSelector(s1, s2)
	})
}

// isListNode reports whether n's entries hold list elements, identified
// by index selectors. It returns an error when index and field entries
// are mixed under the same node.
func isListNode(n *structBuilderNode) (bool, error) {
	numIndex := 0
	for sel := range n.entries {
		if sel.LabelType() == cue.IndexLabel {
			numIndex++
		}
	}
	switch {
	case numIndex == 0:
		return false, nil
	case numIndex < len(n.entries):
		return false, fmt.Errorf("cannot mix list-index and field entries")
	}
	return true, nil
}

// listSyntax returns a list literal holding n's entries, all of which
// hold index selectors, at their respective indices. Indices with no
// entry are filled with a top (_) placeholder.
func (b *structBuilder) listSyntax(n *structBuilderNode) (ast.Expr, error) {
	maxIndex := -1
	for sel := range n.entries {
		maxIndex = max(maxIndex, sel.Index())
	}
	elts := make([]ast.Expr, maxIndex+1)
	for i := range elts {
		elts[i] = ast.NewIdent("_")
	}
	for sel, entry := range n.entries {
		var db declBuilder
		if err := b.appendDecls(entry, &db); err != nil {
			return nil, err
		}
		if len(db.decls) == 0 {
			// The entry has no value or sub-entries (e.g. it was only
			// the target of a getRef call); leave the placeholder.
			continue
		}
		// Drop the new-section positioning that appendField gives
		// declarations, leaving the layout to the formatter.
		for _, d := range db.decls {
			ast.SetRelPos(d, token.NoRelPos)
		}
		expr := exprFromDecls(db.decls)
		ast.SetRelPos(expr, token.NoRelPos)
		elts[sel.Index()] = expr
	}
	return &ast.ListLit{Elts: elts}, nil
}

type declBuilder struct {
	decls []ast.Decl
	path  []cue.Selector
}

func (b *declBuilder) pushPath(sel cue.Selector) {
	b.path = append(b.path, sel)
}

func (b *declBuilder) popPath() {
	b.path = b.path[:len(b.path)-1]
}

func exprFromDecls(decls []ast.Decl) ast.Expr {
	if len(decls) == 1 {
		if decl, ok := decls[0].(*ast.EmbedDecl); ok {
			// It's a single embedded expression which we can use directly.
			return decl.Expr
		}
	}
	return &ast.StructLit{
		Elts: decls,
	}
}

func appendDeclsExpr(decls []ast.Decl, expr ast.Expr) []ast.Decl {
	switch expr := expr.(type) {
	case *ast.StructLit:
		decls = append(decls, expr.Elts...)
	default:
		elt := &ast.EmbedDecl{Expr: expr}
		ast.SetRelPos(elt, token.NewSection)
		decls = append(decls, elt)
	}
	return decls
}

func appendField(decls []ast.Decl, path cue.Path, v ast.Expr, comment *ast.CommentGroup) ([]ast.Decl, error) {
	if len(path.Selectors()) == 0 {
		return appendDeclsExpr(decls, v), nil
	}
	expr, err := exprAtPath(path, v)
	if err != nil {
		return nil, err
	}
	// exprAtPath will always return a struct literal with exactly
	// one element when the path is non-empty.
	structLit := expr.(*ast.StructLit)
	elt := structLit.Elts[0]
	if comment != nil {
		ast.SetComments(elt, []*ast.CommentGroup{comment})
	}
	ast.SetRelPos(elt, token.NewSection)
	return append(decls, elt), nil
}

func cmpSelector(s1, s2 cue.Selector) int {
	if s1 == s2 {
		// Avoid String allocation when we can.
		return 0
	}
	if c := cmp.Compare(s1.Type(), s2.Type()); c != 0 {
		return c
	}
	return cmp.Compare(s1.String(), s2.String())
}
