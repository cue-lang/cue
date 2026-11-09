// Copyright 2020 CUE Authors
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

package astutil

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cueexperiment"
)

// TODO:
// - change field from foo to "foo" if it isn't referenced, rather than
//   relying on introducing a unique alias.

// SanitizeFiles sanitizes all CUE files belonging to a single package,
// detecting cross-file shadowing of predeclared identifiers.
//
// Every file is sanitized, whether or not an earlier one failed; the
// errors of all failing files are returned together, each positioned in
// the file it concerns.
func SanitizeFiles(files []*ast.File) error {
	names := make(map[string]bool)
	for _, f := range files {
		for _, d := range f.Decls {
			if x, ok := d.(*ast.Field); ok {
				if name := labelName(x.Label); name != "" {
					names[name] = true
				}
			}
		}
	}
	var errs errors.Error
	for _, f := range files {
		if err := sanitize(f, names); err != nil {
			errs = errors.Append(errs, errors.Promote(err, "sanitize"))
		}
	}
	if errs != nil {
		return errs
	}
	return nil
}

// requiresPostfixAliases reports whether an alias introduced into f must be
// written in the postfix form (a~X) rather than the prefix form (X=a). This
// is determined by f's experiments, aliasv2 being what enables the postfix
// form: from the language version where it is stable, or wherever the file
// enables it outright.
//
// The parser sets f's experiments; a generator sets whatever it was told
// applies. A file with neither says nothing about what it is written for, and
// the current version is the only reasonable reading of that.
func requiresPostfixAliases(f *ast.File) bool {
	exp := f.Experiment()
	if exp.LanguageVersion() == "" {
		exp = cueexperiment.Default()
	}
	return exp.AliasV2
}

// labelName returns the name of a label, or "" if it cannot be determined.
func labelName(label ast.Label) string {
	switch x := label.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.Alias:
		if id, ok := x.Expr.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// Sanitize rewrites File f in place to be well-formed after automated
// construction of an AST.
//
// Rewrites:
//   - auto inserts imports associated with Idents
//   - unshadows imports associated with idents
//   - unshadows references for identifiers that were already resolved.
//   - renames, with their references, let clauses and aliases named by a
//     keyword such as "in", aliases named like a field in scope, and
//     comprehension clause variables shadowed where they are referred to.
//
// Deprecated: use [SanitizeFiles] to sanitize all files in a package together,
// to avoid issues such as one file shadowing a builtin name in the package scope.
func Sanitize(f *ast.File) error {
	return sanitize(f, nil)
}

func sanitize(f *ast.File, names map[string]bool) error {
	z := &sanitizer{
		file:           f,
		postfixAliases: requiresPostfixAliases(f),

		names:      map[string]bool{},
		pkgNames:   names,
		importMap:  map[string]*ast.ImportSpec{},
		referenced: map[ast.Node]bool{},
		altMap:     map[ast.Node]string{},
		toRename:   map[*ast.Ident]bool{},
		imports:    map[string]*ast.ImportSpec{},
		newNames:   map[*ast.Ident]string{},
		nodeNames:  map[ast.Node]string{},
	}

	for name := range names {
		z.names[name] = true
	}

	// Gather all names, and the declarations to rename.
	stack := make([]*scope, 0, 8)
	s := &scope{
		errFn:      z.errf,
		nameFn:     z.addName,
		identFn:    z.markUsed,
		conflictFn: z.renameAlias,
		scopeStack: &stack,
	}
	ast.Walk(f, s.Before, nil)
	if z.errs != nil {
		return z.errs
	}

	// Add imports and unshadow.
	stack = stack[:0]
	s = &scope{
		errFn:      z.errf,
		nameFn:     z.newName,
		identFn:    z.handleIdent,
		conflictFn: z.renameAlias,
		index:      make(map[string]entry),
		scopeStack: &stack,
	}
	z.fileScope = s
	// Put the referenced imports in scope from the start, so that a let
	// clause or alias of the same name is renamed wherever it is declared.
	// They are added to the file as their references are reached.
	for name, spec := range z.imports {
		s.insert(name, spec, spec, nil)
	}
	ast.Walk(f, s.Before, nil)
	// The walk has renamed the references, so the declarations must follow,
	// even if it failed.
	for id, name := range z.newNames {
		id.Name = name
	}
	if z.errs != nil {
		return z.errs
	}

	z.cleanImports()
	return z.errs
}

// declIdent returns the identifier declaring e if it may be renamed:
// a let clause, an alias, or a comprehension clause variable.
func declIdent(e entry) *ast.Ident {
	switch x := e.link.(type) {
	case *ast.LetClause:
		return x.Ident
	case *ast.Alias:
		return x.Ident
	case *ast.PostfixAlias:
		if e.field != nil {
			return x.Label
		}
		return x.Field
	case *ast.ForClause, *ast.TryClause:
		id, _ := e.node.(*ast.Ident)
		return id
	case *ast.Field:
		// A value alias.
		if a, ok := e.node.(*ast.Alias); ok {
			return a.Ident
		}
	}
	return nil
}

// renameAlias marks an alias named like a field in scope for renaming,
// and reports whether it could.
func (z *sanitizer) renameAlias(alias entry) bool {
	id := declIdent(alias)
	if id == nil {
		return false
	}
	z.toRename[id] = true
	return true
}

// newName picks a new name for a declaration marked for renaming, which
// the walk gives its references, and the declaration once it is done.
func (z *sanitizer) newName(name string, e entry) {
	id := declIdent(e)
	if id == nil || !z.toRename[id] {
		return
	}
	if _, ok := z.newNames[id]; ok {
		return
	}
	alt := z.uniqueName(name)
	z.newNames[id] = alt
	// Linked references look up the name by node, as a field may have
	// taken the declaration's place in the index.
	z.nodeNames[e.node] = alt
}

type sanitizer struct {
	file      *ast.File
	fileScope *scope

	// postfixAliases records whether an alias this sanitizer introduces must
	// be written in the postfix form to parse at the target language version.
	postfixAliases bool

	// names is all used names. Can be used to determine a new unique name.
	names map[string]bool
	// namer picks the new names which [sanitizer.uniqueName] adds to
	// [sanitizer.names].
	namer astinternal.UniqueNamer
	// pkgNames holds the top-level field names of all files in the package.
	pkgNames   map[string]bool
	referenced map[ast.Node]bool

	// altMap defines an alternative name for an existing entry link (a field,
	// alias or let clause). As new names are globally unique, they can be
	// safely reused for any unshadowing.
	altMap    map[ast.Node]string
	importMap map[string]*ast.ImportSpec
	// imports holds, by name, the imports referenced where nothing shadows
	// them; see [sanitizer.markUsed].
	imports map[string]*ast.ImportSpec

	// toRename holds the identifiers declaring the lets, aliases, and
	// clause variables to rename with their references. Their new names
	// are picked in the second walk, and recorded in newNames.
	toRename map[*ast.Ident]bool
	newNames map[*ast.Ident]string
	// nodeNames holds the new names by declaration node.
	nodeNames map[ast.Node]string

	errs errors.Error
}

func (z *sanitizer) errf(p token.Pos, msg string, args ...interface{}) {
	z.errs = errors.Append(z.errs, errors.Newf(p, msg, args...))
}

func (z *sanitizer) addName(name string, e entry) {
	z.names[name] = true
	id := declIdent(e)
	if id == nil {
		return
	}
	if token.Lookup(name).IsKeyword() {
		z.toRename[id] = true
	}
}

func (z *sanitizer) addRename(base string, n ast.Node) (alt string, new bool) {
	if name, ok := z.altMap[n]; ok {
		return name, false
	}

	name := z.uniqueName(base)
	z.altMap[n] = name
	return name, true
}

func (z *sanitizer) unshadow(parent ast.Node, base string, link ast.Node) string {
	name, ok := z.altMap[link]
	if !ok {
		name = z.uniqueName(base)
		z.altMap[link] = name

		// Insert new let clause at top to refer to a declaration in possible
		// other files.
		let := &ast.LetClause{
			Ident: ast.NewIdent(name),
			Expr:  ast.NewIdent(base),
		}

		var decls *[]ast.Decl

		switch x := parent.(type) {
		case *ast.File:
			decls = &x.Decls
		case *ast.StructLit:
			decls = &x.Elts
		default:
			// A Field scope declares a pattern constraint's label alias or a
			// value alias. The exporter links a reference past a value
			// alias, and names a label alias so that no field in its value
			// shadows it, so no reference is unshadowed within a Field.
			panic(fmt.Sprintf("impossible scope type %T", parent))
		}

		i := 0
		for ; i < len(*decls); i++ {
			if (*decls)[i] == link {
				break
			}
			if f, ok := (*decls)[i].(*ast.Field); ok {
				// The link is the node the name was declared by, which for a
				// field is its label, or the alias beside it.
				if f.Label == link || f.Alias == link {
					break
				}
			}
		}

		if i > 0 {
			ast.SetRelPos(let, token.NewSection)
		}

		a := append((*decls)[:i:i], let)
		*decls = append(a, (*decls)[i:]...)
	}
	return name
}

// aliasName returns a name by which a reference in s refers to the value
// bound by link and named by id: its new name if renamed, its name if
// visible, or else a new name bound to it by a let clause.
func (z *sanitizer) aliasName(s *scope, parent ast.Node, id *ast.Ident, link ast.Node) string {
	if name, ok := z.newNames[id]; ok {
		return name
	}
	if _, _, e := s.lookup(id.Name); e.link == link {
		return id.Name
	}
	return z.unshadow(parent, id.Name, link)
}

func (z *sanitizer) markUsed(s *scope, n *ast.Ident) bool {
	if n.Node != nil {
		if spec, ok := n.Node.(*ast.ImportSpec); ok {
			// The import is only added as the second walk reaches n,
			// so reserve its name, and put it in scope before then.
			info, _ := ParseImportSpec(spec)
			z.names[info.Ident] = true
			if _, ok := z.imports[info.Ident]; !ok {
				if _, _, e := s.lookup(info.Ident); e.node == nil {
					z.imports[info.Ident] = spec
				}
			}
		}
		z.markShadowedClauseVar(s, n)
		return false
	}
	_, _, entry := s.lookup(n.String())
	z.referenced[entry.link] = true
	return true
}

// markShadowedClauseVar marks the clause variable n refers to for renaming
// if another declaration shadows it at n: a clause has no struct to insert
// a let clause in, so renaming is the only way to reach the variable.
func (z *sanitizer) markShadowedClauseVar(s *scope, n *ast.Ident) {
	id, ok := n.Node.(*ast.Ident)
	if !ok {
		return
	}
	if _, _, cur := s.lookup(n.Name); cur.node == n.Node {
		return
	}
	switch node, _, _ := s.resolveScope(n.Name, n.Node); node.(type) {
	case *ast.ForClause, *ast.LetClause, *ast.TryClause:
		z.toRename[id] = true
	}
}

func (z *sanitizer) cleanImports() {
	for decl := range z.file.ImportDecls() {
		decl.Specs = slices.DeleteFunc(decl.Specs, func(spec *ast.ImportSpec) bool {
			_, ok := z.referenced[spec]
			return !ok
		})
	}
	// Ensure that the first import always starts a new section
	// so that if the file has a comment, it won't be associated with
	// the import comment rather than the file.
	for decl := range z.file.ImportDecls() {
		ast.SetRelPos(decl, token.NewSection)
		break
	}
}

// bindField binds name to the value of field x, whose label is the identifier
// y, in the syntax the target language version accepts. The postfix form sits
// beside the label; the prefix form wraps it, and so takes the label's
// formatting and comments with it.
func (z *sanitizer) bindField(x *ast.Field, y *ast.Ident, name string) {
	ident := ast.NewIdent(name)
	// A field already carrying a postfix alias takes the name into its free
	// half, whatever the target version would otherwise pick: a field cannot
	// hold both forms at once, and the one already there is the one the file
	// it belongs to evidently accepts.
	if x.Alias == nil && !z.postfixAliases {
		CopyMeta(ident, y)
		ast.SetRelPos(y, token.NoRelPos)
		ast.SetComments(y, nil)
		x.Label = &ast.Alias{Ident: ident, Expr: y}
		return
	}
	if x.Alias == nil {
		x.Alias = &ast.PostfixAlias{}
	}
	ast.SetRelPos(ident, token.NoSpace)
	x.Alias.Field = ident
}

func (z *sanitizer) handleIdent(s *scope, n *ast.Ident) bool {
	if len(z.newNames) > 0 {
		// New names are unique, so they need no unshadowing.
		var name string
		var ok bool
		if n.Node != nil {
			name, ok = z.nodeNames[n.Node]
		} else {
			_, _, node := s.lookup(n.Name)
			name, ok = z.newNames[declIdent(node)]
			if !ok && node.ambiguous {
				z.errf(n.Pos(), "reference %q is ambiguous: a declaration of that name was renamed", n.Name)
				return true
			}
		}
		if ok {
			n.Name = name
			n.Scope = nil
			return true
		}
	}
	if n.Node == nil {
		return true
	}

	_, _, node := s.lookup(n.Name)
	if node.node == nil {
		if n.IsPredeclared() {
			// Check if the predeclared name is shadowed by a top-level field
			// in another file of the same package.
			if z.pkgNames[n.Name] {
				n.Name = "__" + n.Name
			}
			n.Scope = nil
			return true
		}
		spec, ok := n.Node.(*ast.ImportSpec)
		if !ok {
			// Clear node. A reference may have been moved to a different
			// file. If not, it should be an error.
			n.Node = nil
			n.Scope = nil
			return false
		}

		_ = z.addImport(spec)
		info, _ := ParseImportSpec(spec)
		z.fileScope.insert(info.Ident, spec, spec, nil)
		return true
	}

	if x, ok := n.Node.(*ast.ImportSpec); ok {
		xi, _ := ParseImportSpec(x)

		if y, ok := node.node.(*ast.ImportSpec); ok {
			yi, _ := ParseImportSpec(y)
			if xi.ID == yi.ID { // name must be identical as a result of lookup.
				// The import may be in scope before it is declared.
				z.addImport(y)
				n.Node = x
				n.Scope = nil
				return false
			}
		}

		// Either:
		// - the import is shadowed
		// - an incorrect import is matched
		// In all cases we need to create a new import with a unique name or
		// use a previously created one.
		spec := z.importMap[xi.ID]
		if spec == nil {
			name := z.uniqueName(xi.Ident)
			spec = z.addImport(&ast.ImportSpec{
				Name: ast.NewIdent(name),
				Path: x.Path,
			})
			z.importMap[xi.ID] = spec
			z.fileScope.insert(name, spec, spec, nil)
		}

		info, _ := ParseImportSpec(spec)
		// TODO(apply): replace n itself directly
		n.Name = info.Ident
		n.Node = spec
		n.Scope = nil
		return false
	}

	if node.node == n.Node {
		return true
	}

	// A predeclared reference (e.g. "self") is shadowed by a local
	// declaration. Use the "__"-prefixed form to avoid the shadow.
	if n.IsPredeclared() {
		n.Name = "__" + n.Name
		n.Scope = nil
		return false
	}

	// n.Node != node and are both not nil and n.Node is not an ImportSpec.
	// This means that either n.Node is illegal or shadowed.
	// Look for the scope in which n.Node is defined and add an alias or let.

	parent, e, ok := s.resolveScope(n.Name, n.Node)
	if !ok {
		// The node isn't within a legal scope within this file. It may only
		// possibly shadow a value of another file. We add a top-level let
		// clause to refer to this value.

		// TODO(apply): better would be to have resolve use Apply so that we can replace
		// the entire ast.Ident, rather than modifying it.
		// TODO: resolve to new node or rely on another pass of Resolve?
		n.Name = z.unshadow(z.file, n.Name, n)
		n.Node = nil
		n.Scope = nil

		return false
	}

	var name string
	// var isNew bool
	switch x := e.link.(type) {
	case *ast.Field: // referring to regular field.
		name, ok = z.altMap[x]
		if ok {
			break
		}
		// If this field already binds its value to a name, use that name,
		// unshadowing it where needed. Otherwise introduce an alias with a
		// unique name.
		if a := x.Alias; a != nil && a.Field != nil && a.Field.Name != "_" {
			name = z.aliasName(s, parent, a.Field, a)
			break
		}
		switch y := x.Label.(type) {
		case *ast.Alias:
			name = z.aliasName(s, parent, y.Ident, y)

		case *ast.Ident:
			var isNew bool
			name, isNew = z.addRename(y.Name, x)
			if isNew {
				z.bindField(x, y, name)
			}

		default:
			// This is an illegal reference.
			return false
		}

	case *ast.LetClause:
		name = z.unshadow(parent, x.Ident.Name, x)

	case *ast.Alias:
		name = z.unshadow(parent, x.Ident.Name, x)

	case *ast.PostfixAlias:
		// n resolved to either half of the alias, so it already holds the
		// name that half was inserted under.
		name = z.unshadow(parent, n.Name, x)

	default:
		panic(fmt.Sprintf("unexpected link type %T", e.link))
	}

	// TODO(apply): better would be to have resolve use Apply so that we can replace
	// the entire ast.Ident, rather than modifying it.
	n.Name = name
	n.Node = nil
	n.Scope = nil

	return true
}

// uniqueName returns a new name, based on base but never base itself, which
// is unused in the file and the package's top level.
func (z *sanitizer) uniqueName(base string) string {
	name := z.namer.Name(base, func(name string) bool {
		return name == base || z.names[name]
	})
	z.names[name] = true
	return name
}

func (z *sanitizer) addImport(spec *ast.ImportSpec) *ast.ImportSpec {
	spec = insertImport(&z.file.Decls, spec)
	z.referenced[spec] = true
	return spec
}
