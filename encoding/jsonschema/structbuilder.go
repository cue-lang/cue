package jsonschema

import (
	"cmp"
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// structBuilder builds a struct value incrementally by
// putting values for its component paths.
// The [structBuilder.getRef] method can be used
// to obtain reliable references into the resulting struct.
type structBuilder struct {
	root structBuilderNode

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

	// comment holds any doc comment associated with the value.
	comment *ast.CommentGroup

	// entries holds the children of this node, keyed by the
	// name of each child's struct field selector.
	entries map[cue.Selector]*structBuilderNode
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
	e.comment = comment
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
	// TODO slices.SortedFunc(maps.Keys(n.entries), cmpSelector)
	for _, sel := range sortedKeys(n.entries, cmpSelector) {
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
