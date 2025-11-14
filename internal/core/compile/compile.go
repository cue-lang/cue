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

package compile

import (
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/semver"
)

// A Scope represents a nested scope of Vertices.
type Scope interface {
	Parent() Scope
	Vertex() *adt.Vertex
}

// Config configures a compilation.
type Config struct {
	// Scope specifies a node in which to look up unresolved references. This
	// is useful for evaluating expressions within an already evaluated
	// configuration.
	Scope Scope

	// Imports allows unresolved identifiers to resolve to imports.
	//
	// Under normal circumstances, identifiers bind to import specifications,
	// which get resolved to an ImportReference. Use this option to
	// automatically resolve identifiers to imports.
	Imports func(x *ast.Ident) (pkgPath string)

	// pkgPath is used to qualify the scope of hidden fields. The default
	// scope is "_".
	pkgPath string
}

// Files compiles the given files as a single instance. It disregards
// the package names and it is the responsibility of the user to verify that
// the packages names are consistent. The pkgID must be a unique identifier
// for a package in a module, for instance as obtained from build.Instance.ID.
//
// Files may return a completed parse even if it has errors.
func Files(cfg *Config, r adt.Runtime, pkgID string, files ...*ast.File) (*adt.Vertex, errors.Error) {
	c := newCompiler(cfg, pkgID, r)

	v := c.compileFiles(files)

	if c.errs != nil {
		return v, c.errs
	}
	return v, nil
}

// Expr compiles the given expression into a conjunct. The pkgID must be a
// unique identifier for a package in a module, for instance as obtained from
// build.Instance.ID.
func Expr(cfg *Config, r adt.Runtime, pkgPath string, x ast.Expr) (adt.Conjunct, errors.Error) {
	c := newCompiler(cfg, pkgPath, r)

	v := c.compileExpr(x)

	if c.errs != nil {
		return v, c.errs
	}
	return v, nil
}

func newCompiler(cfg *Config, pkgPath string, r adt.Runtime) *compiler {
	c := &compiler{
		index: r,
	}
	if cfg != nil {
		c.Config = *cfg
	}
	if pkgPath == "" {
		pkgPath = "_"
	}
	c.Config.pkgPath = pkgPath
	return c
}

type compiler struct {
	Config
	upCountOffset int32 // 1 for files; 0 for expressions

	index adt.StringIndexer

	experiments cueexperiment.File

	stack      []frame
	inSelector int

	// refersToForVariable tracks whether an expression refers to a key or
	// value produced by a for comprehension embedded within a struct.
	// An Environment associated with such a comprehension value is collapsed
	// onto the destination.
	// Tracking this is necessary for let fields, which should not be unified
	// into the destination when referring to such values.
	// See https://cuelang.org/issue/2218.
	// TODO(perf): use this to compute when a field can be structure shared
	// across different iterations of the same field.
	refersToForVariable bool

	fileScope map[adt.Feature]bool

	num literal.NumInfo

	errs errors.Error
}

func (c *compiler) reset() {
	c.fileScope = nil
	c.stack = c.stack[:0]
	c.errs = nil
}

func (c *compiler) errf(n ast.Node, format string, args ...interface{}) *adt.Bottom {
	err := &compilerError{
		n:       n,
		path:    c.path(),
		Message: errors.NewMessagef(format, args...),
	}
	c.errs = errors.Append(c.errs, err)
	return &adt.Bottom{Err: err}
}

func (c *compiler) path() []string {
	a := []string{}
	for _, f := range c.stack {
		if f.label != nil {
			a = append(a, f.label.labelString())
		}
	}
	return a
}

type frame struct {
	label labeler  // path name leading to this frame.
	scope ast.Node // *ast.File or *ast.Struct
	field ast.Decl
	// scope   map[ast.Node]bool
	upCount int32 // 1 for field, 0 for embedding.

	// isComprehensionVar indicates that this scope refers to a for clause
	// that is part of a comprehension embedded in a struct.
	isComprehensionVar bool

	aliases map[string]aliasEntry
}

type aliasEntry struct {
	label   labeler
	srcExpr ast.Expr
	expr    adt.Expr
	source  ast.Node
	feature adt.Feature // For let declarations
	used    bool
}

func (c *compiler) insertAlias(id *ast.Ident, a aliasEntry) *adt.Bottom {
	k := len(c.stack) - 1
	m := c.stack[k].aliases
	if m == nil {
		m = map[string]aliasEntry{}
		c.stack[k].aliases = m
	}

	if id == nil || !ast.IsValidIdent(id.Name) {
		return c.errf(a.source, "invalid identifier name")
	}

	if e, ok := m[id.Name]; ok {
		return c.errf(a.source,
			"alias %q already declared; previous declaration at %s",
			id.Name, e.source.Pos())
	}
	if id.Name == "_" {
		return c.errf(id, "cannot use _ as alias or let clause")
	}

	m[id.Name] = a
	return nil
}

func (c *compiler) updateAlias(id *ast.Ident, expr adt.Expr) {
	k := len(c.stack) - 1
	m := c.stack[k].aliases

	x := m[id.Name]
	x.expr = expr
	x.label = nil
	x.srcExpr = nil
	m[id.Name] = x
}

// lookupAlias looks up an alias with the given name at the k'th stack position.
func (c *compiler) lookupAlias(k int, id *ast.Ident) aliasEntry {
	m := c.stack[k].aliases
	name := id.Name
	entry, ok := m[name]

	if !ok {
		err := c.errf(id,
			"could not find let or alias associated with identifier %q", name)
		return aliasEntry{expr: err}
	}

	switch {
	case entry.label != nil:
		// TODO: allow cyclic references in let expressions once these can be
		// encoded as a ValueReference.
		if entry.srcExpr == nil {
			entry.expr = c.errf(id, "cyclic references in let clause or alias")
			break
		}

		src := entry.srcExpr
		entry.srcExpr = nil // mark to allow detecting cycles
		m[name] = entry

		entry.expr = c.labeledExprAt(k, nil, entry.label, src)
		entry.label = nil
	}

	entry.used = true
	m[name] = entry
	return entry
}

func (c *compiler) pushScope(n labeler, upCount int32, id ast.Node) *frame {
	c.stack = append(c.stack, frame{
		label:   n,
		scope:   id,
		upCount: upCount,
	})
	return &c.stack[len(c.stack)-1]
}

func (c *compiler) popScope() {
	k := len(c.stack) - 1
	f := c.stack[k]
	for k, v := range f.aliases {
		if !v.used {
			c.errf(v.source, "unreferenced alias or let clause %s", k)
		}
	}
	c.stack = c.stack[:k]
}

func (c *compiler) compileFiles(a []*ast.File) *adt.Vertex { // Or value?
	c.fileScope = map[adt.Feature]bool{}
	c.upCountOffset = 1

	// TODO(resolve): this is also done in the runtime package, do we need both?

	// Populate file scope to handle unresolved references.
	// Excluded from cross-file resolution are:
	// - import specs
	// - aliases
	// - let declarations
	// - anything in an anonymous file
	//
	for _, f := range a {
		if f.PackageName() == "" {
			continue
		}

		for _, d := range f.Decls {
			if f, ok := d.(*ast.Field); ok {
				if id, ok := f.Label.(*ast.Ident); ok {
					c.fileScope[c.label(id)] = true
				}
			}
		}
	}

	// TODO: set doc.
	res := &adt.Vertex{}

	// env := &adt.Environment{Vertex: nil} // runtime: c.runtime

	env := &adt.Environment{}
	top := env

	for p := c.Config.Scope; p != nil; p = p.Parent() {
		top.Vertex = p.Vertex()
		top.Up = &adt.Environment{}
		top = top.Up
	}

	for _, file := range a {
		c.experiments = file.Pos().Experiment()

		c.pushScope(nil, 0, file) // File scope
		v := &adt.StructLit{Src: file}
		c.addDecls(v, file.Decls)
		res.InsertConjunct(adt.MakeRootConjunct(env, v))
		c.popScope()
	}

	return res
}

func (c *compiler) compileExpr(x ast.Expr) adt.Conjunct {
	expr := c.expr(x)

	env := &adt.Environment{}
	top := env

	for p := c.Config.Scope; p != nil; p = p.Parent() {
		top.Vertex = p.Vertex()
		top.Up = &adt.Environment{}
		top = top.Up
	}

	return adt.MakeRootConjunct(env, expr)
}

// verifyVersion checks whether n is a Builtin and then checks whether the
// Added version is compatible with the file version registered in c.
func (c *compiler) verifyVersion(src ast.Node, n adt.Expr) adt.Expr {
	var kind, name, added string
	switch x := n.(type) {
	default:
		return n

	case *adt.Builtin:
		if x.Added == "" {
			// No version check needed.
			return n
		}

		kind = "builtin"
		name = x.Name
		added = x.Added

	case *adt.ValueReference:
		// NOTE: this is always self or __self.
		kind = "predeclared identifier"
		name = x.Src.Name
		// Check if Self experiment is enabled
		if !c.experiments.AliasV2 {
			return c.errf(src, "%s %q requires @experiment(aliasv2)", kind, name)
		}
		x.Label = adt.MakeStringLabel(c.index, name)
		return n
	}

	v := c.experiments.LanguageVersion()
	if v == "" {
		// We assume "latest" if the file is not associated with a version.
		return n
	}

	if semver.Compare(added, v) <= 0 {
		// The feature is available in the file version.
		return n
	}

	// The feature is not available in the file version.
	// NonConcrete builtins are not allowed in older versions.
	return c.errf(src, "%s %q is not available in version %v; "+
		"it was added in version %q", kind, name, v, added)
}

// resolve assumes that all existing resolutions are legal. Validation should
// be done in a separate step if required.
//
// TODO: collect validation pass to verify that all resolutions are
// legal?
func (c *compiler) resolve(n *ast.Ident) adt.Expr {
	// X in import "path/X"
	// X in import X "path"
	if imp, ok := n.Node.(*ast.ImportSpec); ok {
		return &adt.ImportReference{
			Src:        n,
			ImportPath: c.label(imp.Path),
			Label:      c.label(n),
		}
	}

	label := c.label(n)

	if label == adt.InvalidLabel { // `_`
		return &adt.Top{Src: n}
	}

	// Unresolved field.
	if n.Node == nil {
		upCount := int32(0)
		for _, c := range c.stack {
			upCount += c.upCount
		}
		if c.fileScope[label] {
			return &adt.FieldReference{
				Src:     n,
				UpCount: upCount,
				Label:   label,
			}
		}
		upCount += c.upCountOffset
		for p := c.Scope; p != nil; p = p.Parent() {
			for _, a := range p.Vertex().Arcs {
				switch {
				case a.Label.IsLet() && a.Label.IdentString(c.index) == n.Name:
					label = a.Label
				case a.Label == label:
					return &adt.FieldReference{
						Src:     n,
						UpCount: upCount,
						Label:   label,
					}
				}
			}
			upCount++
		}

		if c.Config.Imports != nil {
			if pkgPath := c.Config.Imports(n); pkgPath != "" {
				return &adt.ImportReference{
					Src:        n,
					ImportPath: adt.MakeStringLabel(c.index, pkgPath),
					Label:      c.label(n),
				}
			}
		}

		if p := predeclared(n); p != nil {
			return c.verifyVersion(n, p)
		}

		return c.errf(n, "reference %q not found", n.Name)
	}

	//   X in [X=x]: y  Scope: Field  Node: Expr (x)
	//   X in X=[x]: y  Scope: Field  Node: Field
	//   X in x: X=y    Scope: Field  Node: Alias
	if f, ok := n.Scope.(*ast.Field); ok {
		upCount := int32(0)

		k := len(c.stack) - 1
		for ; k >= 0; k-- {
			if c.stack[k].field == f {
				break
			}
			upCount += c.stack[k].upCount
		}

		label := &adt.LabelReference{
			Src:     n,
			UpCount: upCount,
		}

		switch x := n.Node.(type) {
		case *ast.Ident:
			// If the identifier refers to a label alias, we link to that.
			if f.Alias != nil && f.Alias.Label == x {
				switch lab := f.Label.(type) {
				case *ast.Ident:
					if internal.IsDefOrHidden(lab.Name) {
						return c.errf(x, "label alias cannot reference definition or hidden field")
					}
					return c.expr(ast.NewString(lab.Name))
				case *ast.BasicLit:
					return c.expr(lab)
				}
			}
		case *ast.Field:
			var ident *ast.Ident
			if alias, _ := x.Label.(*ast.Alias); alias != nil {
				if x.Alias != nil {
					return c.errf(x,
						"field has both label alias and postfix alias")
				}
				ident = alias.Ident
			} else if x.Alias != nil {
				ident = x.Alias.Field
			} else {
				return c.errf(x, "label reference has no alias")
			}

			_ = c.lookupAlias(k, ident) // mark as used
			// The expression of field Label is always done in the same
			// Environment as pointed to by the UpCount of the DynamicReference
			// and the evaluation of a DynamicReference assumes this.
			// We therefore set the UpCount of the LabelReference to 0.
			label.UpCount = 0
			return &adt.DynamicReference{
				Src:     n,
				UpCount: upCount,
				Label:   label,
			}

		case *ast.Alias:
			_ = c.lookupAlias(k, x.Ident) // mark as used
			return &adt.ValueReference{
				Src:     n,
				UpCount: upCount,
				Label:   c.label(x.Ident),
			}
		}
		return label
	}

	upCount := int32(0)

	k := len(c.stack) - 1
	for ; k >= 0; k-- {
		if f := c.stack[k]; f.scope == n.Scope {
			if f.isComprehensionVar {
				c.refersToForVariable = true
			}
			break
		}
		upCount += c.stack[k].upCount
	}
	if k < 0 {
		// This is a programmatic error and should never happen if the users
		// just builds with the cue command or if astutil.Resolve is used
		// correctly.
		c.errf(n, "reference %q set to unknown node in AST; "+
			"this can result from incorrect API usage or a compiler bug",
			n.Name)
	}

	if n.Scope == nil {
		// Package.
		// Should have been handled above.
		return c.errf(n, "unresolved identifier %v", n.Name)
	}

	switch f := n.Node.(type) {
	// Local expressions
	case *ast.LetClause:
		entry := c.lookupAlias(k, n)
		if entry.expr == nil {
			panic("unreachable")
		}
		label = entry.feature

		// let x = y
		return &adt.LetReference{
			Src:     n,
			UpCount: upCount,
			Label:   label,
			X:       entry.expr, // TODO: remove usage
		}

	// Handle new-style postfix aliases: a~X or a~(K,V)
	case *ast.Field:
		var ident *ast.Ident
		lab := f.Label
		// Old-style label aliases: X=x: y, X=(x): y, X="\(x)":

		if a, ok := f.Label.(*ast.Alias); ok {
			ident = a.Ident
			if f.Alias != nil {
				return c.errf(f, "field has both label alias and postfix alias")
			}
			label, ok := a.Expr.(ast.Label)
			if !ok {
				return c.errf(a.Expr, "invalid label expression")
			}
			lab = label
		} else if f.Alias != nil {
			// Check if this identifier refers to the Field alias or Label alias
			// The Field alias (X or V) is the value reference
			// The Label alias (K) in dual form is a string reference
			if f.Alias.Field == nil {
				return c.errf(f, "postfix alias must have field component")
			}
			ident = f.Alias.Field
		} else {
			return c.errf(n, "illegal reference %s", n.Name)
		}

		aliasInfo := c.lookupAlias(k, ident) // marks alias as used.

		name, _, err := ast.LabelName(lab)
		switch {
		case errors.Is(err, ast.ErrIsExpression):
			if aliasInfo.expr == nil {
				// This can happen when we have a cyclic reference like (x)~x: 3
				// where the label expression references the alias being defined.
				return c.errf(n, "cyclic reference in field alias")
			}
			return &adt.DynamicReference{
				Src:     n,
				UpCount: upCount,
				Label:   aliasInfo.expr,
			}

		case err != nil:
			return c.errf(n, "invalid label: %v", err)

		case name != "":
			label = c.label(lab)

		default:
			return c.errf(n, "unsupported field alias %q", name)
		}
	}

	return &adt.FieldReference{
		Src:     n,
		UpCount: upCount,
		Label:   label,
	}
}

func (c *compiler) addDecls(st *adt.StructLit, a []ast.Decl) {
	for _, d := range a {
		c.markAlias(d)
	}
	for _, d := range a {
		c.addLetDecl(d)
	}
	for _, d := range a {
		if x := c.decl(d); x != nil {
			st.Decls = append(st.Decls, x)
		}
	}
}

func isNonBlank(a *ast.Ident) bool {
	return a != nil && a.Name != "_"
}

func (c *compiler) markAlias(d ast.Decl) {
	switch x := d.(type) {
	case *ast.Field:
		lab := x.Label
		if a, ok := lab.(*ast.Alias); ok {
			if _, ok = a.Expr.(ast.Label); !ok {
				c.errf(a, "alias expression is not a valid label")
			}

			e := aliasEntry{source: a}

			c.insertAlias(a.Ident, e)
		}

		// Register postfix aliases for regular fields (not pattern constraints)
		// Pattern constraints register aliases in value scope only
		// Regular field: register in parent scope
		// Store the Field in the label so we can find it later
		// Skip _ (blank identifier)
		if a := x.Alias; a != nil && isNonBlank(a.Field) {
			c.insertAlias(a.Field, aliasEntry{source: a})
		}

	case *ast.LetClause:
		a := aliasEntry{
			label:   (*letScope)(x),
			srcExpr: x.Expr,
			source:  x,
			feature: adt.MakeLetLabel(c.index, x.Ident.Name),
		}
		c.insertAlias(x.Ident, a)

	case *ast.Alias:
		c.errf(x, "old-style alias no longer supported: use let clause; use cue fix to update.")
	}
}

func (c *compiler) decl(d ast.Decl) adt.Decl {
	switch x := d.(type) {
	case *ast.BadDecl:
		return c.errf(d, "")

	case *ast.Field:
		lab := x.Label
		if a, ok := lab.(*ast.Alias); ok {
			if lab, ok = a.Expr.(ast.Label); !ok {
				return c.errf(a, "alias expression is not a valid label")
			}
		}

		v := x.Value
		var value adt.Expr

		// Handle value aliases. Deprecated in new aliases.
		if a, ok := v.(*ast.Alias); ok {
			c.pushScope(nil, 0, a)
			c.insertAlias(a.Ident, aliasEntry{source: a})
			value = c.labeledExpr(x, (*fieldLabel)(x), a.Expr)
			c.popScope()
		} else {
			value = c.labeledExpr(x, (*fieldLabel)(x), v)
		}

		switch l := lab.(type) {
		case *ast.Ident, *ast.BasicLit:
			label := c.label(lab)

			if label == adt.InvalidLabel {
				return c.errf(x, "cannot use _ as label")
			}

			return &adt.Field{
				Src:     x,
				Label:   label,
				ArcType: adt.ConstraintFromToken(x.Constraint),
				Value:   value,
			}

		case *ast.ListLit:
			if len(l.Elts) != 1 {
				// error
				return c.errf(x, "list label must have one element")
			}
			var label adt.Feature
			elem := l.Elts[0]
			// TODO: record alias for error handling? In principle it is okay
			// to have duplicates, but we do want it to be used.
			if a, ok := elem.(*ast.Alias); ok {
				label = c.label(a.Ident)
				elem = a.Expr
			}

			// For postfix aliases, use the Field identifier (X or V)
			// For dual form ~(K,V), we use V as the primary label
			if a := x.Alias; a != nil && isNonBlank(a.Label) {
				label = c.label(a.Label)
			}

			return &adt.BulkOptionalField{
				Src:    x,
				Filter: c.expr(elem),
				Value:  value,
				Label:  label,
			}

		case *ast.ParenExpr:
			return &adt.DynamicField{
				Src:     x,
				Key:     c.expr(l),
				ArcType: adt.ConstraintFromToken(x.Constraint),
				Value:   value,
			}

		case *ast.Interpolation:
			return &adt.DynamicField{
				Src:     x,
				Key:     c.expr(l),
				ArcType: adt.ConstraintFromToken(x.Constraint),
				Value:   value,
			}
		}

	case *ast.LetClause:
		m := c.stack[len(c.stack)-1].aliases
		entry := m[x.Ident.Name]

		// A reference to the let should, in principle, be interpreted as a
		// value reference, not field reference:
		// - this is syntactically consistent for the use of =
		// - this is semantically the only valid interpretation
		// In practice this amounts to the same thing, as let expressions cannot
		// be addressed from outside their scope. But it will matter once
		// expressions may refer to a let from within the let.

		savedUses := c.refersToForVariable
		c.refersToForVariable = false
		value := c.labeledExpr(x, (*letScope)(x), x.Expr)
		refsCompVar := c.refersToForVariable
		c.refersToForVariable = savedUses || refsCompVar

		return &adt.LetField{
			Src:     x,
			Label:   entry.feature,
			IsMulti: refsCompVar,
			Value:   value,
		}

	case *ast.CommentGroup:
		// Nothing to do for a free-floating comment group.

	case *ast.Attribute:
		// Nothing to do for now for an attribute declaration.

	case *ast.Ellipsis:
		return &adt.Ellipsis{
			Src:   x,
			Value: c.expr(x.Type),
		}

	case *ast.Comprehension:
		return c.comprehension(x, false)

	case *ast.EmbedDecl: // Deprecated
		for _, c := range ast.Comments(x.Expr) {
			ast.AddComment(x, c)
		}
		ast.SetComments(x.Expr, x.Comments())
		return c.expr(x.Expr)

	case ast.Expr:
		return c.expr(x)
	}
	return nil
}

func (c *compiler) addLetDecl(d ast.Decl) {
	switch x := d.(type) {
	case *ast.Field:
		lab := x.Label
		var ident *ast.Ident
		if a, ok := lab.(*ast.Alias); ok {
			if x.Alias != nil {
				c.errf(x, "field has both label alias and postfix alias")
				return
			}

			if lab, ok = a.Expr.(ast.Label); !ok {
				// error reported elsewhere
				return
			}
			ident = a.Ident

		} else if a := x.Alias; a != nil && isNonBlank(a.Field) {
			ident = x.Alias.Field
		} else {
			break
		}

		switch lab.(type) {
		case *ast.Ident, *ast.BasicLit, *ast.ListLit:
			// Even though we won't need the alias, we still register it
			// for duplicate and failed reference detection.
		default:
			c.updateAlias(ident, c.expr(lab.(ast.Expr)))
		}

	case *ast.Alias:
		c.errf(x, "old-style alias no longer supported: use let clause; use cue fix to update.")
	}
}

func (c *compiler) elem(n ast.Expr) adt.Elem {
	switch x := n.(type) {
	case *ast.Ellipsis:
		return &adt.Ellipsis{
			Src:   x,
			Value: c.expr(x.Type),
		}

	case *ast.Comprehension:
		return c.comprehension(x, true)

	case ast.Expr:
		return c.expr(x)
	}
	return nil
}

func (c *compiler) comprehension(x *ast.Comprehension, inList bool) adt.Elem {
	var a []adt.Yielder
	for _, v := range x.Clauses {
		switch x := v.(type) {
		case *ast.ForClause:
			var key adt.Feature
			if x.Key != nil {
				key = c.label(x.Key)
			}
			y := &adt.ForClause{
				Syntax: x,
				Key:    key,
				Value:  c.label(x.Value),
				Src:    c.expr(x.Source),
			}
			f := c.pushScope((*forScope)(x), 1, v)
			defer c.popScope()
			f.isComprehensionVar = !inList
			a = append(a, y)

		case *ast.IfClause:
			y := &adt.IfClause{
				Src:       x,
				Condition: c.expr(x.Condition),
			}
			a = append(a, y)

		case *ast.LetClause:
			// Check if any references in the expression refer to a for
			// comprehension.
			savedUses := c.refersToForVariable
			c.refersToForVariable = false
			expr := c.expr(x.Expr)
			refsCompVar := c.refersToForVariable
			c.refersToForVariable = savedUses || refsCompVar

			y := &adt.LetClause{
				Src:   x,
				Label: c.label(x.Ident),
				Expr:  expr,
			}
			f := c.pushScope((*letScope)(x), 1, v)
			defer c.popScope()
			f.isComprehensionVar = !inList && refsCompVar
			a = append(a, y)
		}

		if _, ok := a[0].(*adt.LetClause); ok {
			return c.errf(x,
				"first comprehension clause must be 'if' or 'for'")
		}
	}

	// TODO: make x.Value an *ast.StructLit and this is redundant.
	if y, ok := x.Value.(*ast.StructLit); !ok {
		return c.errf(x.Value,
			"comprehension value must be struct, found %T", y)
	}

	y := c.expr(x.Value)

	st, ok := y.(*adt.StructLit)
	if !ok {
		// Error must have been generated.
		return y
	}

	if len(a) == 0 {
		return c.errf(x, "comprehension value without clauses")
	}

	return &adt.Comprehension{
		Syntax:  x,
		Clauses: a,
		Value:   st,
	}
}

func (c *compiler) labeledExpr(f ast.Decl, lab labeler, expr ast.Expr) adt.Expr {
	k := len(c.stack) - 1
	return c.labeledExprAt(k, f, lab, expr)
}

func (c *compiler) labeledExprAt(k int, f ast.Decl, lab labeler, expr ast.Expr) adt.Expr {
	saved := c.stack[k]
	savedStack := c.stack

	c.stack[k].label = lab
	c.stack[k].field = f

	if k < len(c.stack)-1 {
		// Limit the capacity, so that if there is growth, we don't overwrite
		// any values we need to restore later. This shouldn't happen too often,
		// as this will result in a non-reclaimable allocation.
		c.stack = c.stack[: k+1 : k+1]
	}

	value := c.expr(expr)

	c.stack = savedStack
	c.stack[k] = saved
	return value
}

func (c *compiler) expr(expr ast.Expr) adt.Expr {
	switch n := expr.(type) {
	case nil:
		return nil
	case *ast.Ident:
		return c.resolve(n)

	case *ast.Func:
		// We don't yet support function types natively in
		// CUE.  ast.Func exists only to support external
		// interpreters. Function values (really, adt.Builtin)
		// are only created by the runtime, or injected by
		// external interpreters.
		//
		// TODO: revise this when we add function types.
		return c.resolve(ast.NewIdent("_"))

	case *ast.StructLit:
		c.pushScope(nil, 1, n)
		v := &adt.StructLit{Src: n}
		c.addDecls(v, n.Elts)
		c.popScope()
		return v

	case *ast.ListLit:
		c.pushScope(nil, 1, n)
		v := &adt.ListLit{Src: n}
		elts, ellipsis := listEllipsis(n)
		for _, d := range elts {
			elem := c.elem(d)

			switch x := elem.(type) {
			case nil:
			case adt.Elem:
				v.Elems = append(v.Elems, x)
			default:
				c.errf(d, "type %T not allowed in ListLit", d)
			}
		}
		if ellipsis != nil {
			d := &adt.Ellipsis{
				Src:   ellipsis,
				Value: c.expr(ellipsis.Type),
			}
			v.Elems = append(v.Elems, d)
		}
		c.popScope()
		return v

	case *ast.SelectorExpr:
		c.inSelector++
		x := c.expr(n.X)
		// TODO: check if x is an ImportReference, and if so, check if it a
		// standard library, look up the builtin, and check its version. The
		// index of standard libraries is available in c.index, which is really
		// an adt.Runtime under the hood.
		ret := &adt.SelectorExpr{
			Src: n,
			X:   x,
			Sel: c.label(n.Sel)}
		c.inSelector--
		return ret

	case *ast.IndexExpr:
		return &adt.IndexExpr{
			Src:   n,
			X:     c.expr(n.X),
			Index: c.expr(n.Index),
		}

	case *ast.SliceExpr:
		slice := &adt.SliceExpr{Src: n, X: c.expr(n.X)}
		if n.Low != nil {
			slice.Lo = c.expr(n.Low)
		}
		if n.High != nil {
			slice.Hi = c.expr(n.High)
		}
		return slice

	case *ast.BottomLit:
		return &adt.Bottom{
			Src:  n,
			Code: adt.LegacyUserError,
			Err:  errors.Newf(n.Pos(), "explicit error (_|_ literal) in source"),
		}

	case *ast.BadExpr:
		return c.errf(n, "invalid expression")

	case *ast.BasicLit:
		return c.parse(n)

	case *ast.Interpolation:
		if len(n.Elts) == 0 {
			return c.errf(n, "invalid interpolation")
		}
		first, ok1 := n.Elts[0].(*ast.BasicLit)
		last, ok2 := n.Elts[len(n.Elts)-1].(*ast.BasicLit)
		if !ok1 || !ok2 {
			return c.errf(n, "invalid interpolation")
		}
		if len(n.Elts) == 1 {
			return c.expr(n.Elts[0])
		}
		lit := &adt.Interpolation{Src: n}
		info, prefixLen, _, err := literal.ParseQuotes(first.Value, last.Value)
		if err != nil {
			return c.errf(n, "invalid interpolation: %v", err)
		}
		if info.IsDouble() {
			lit.K = adt.StringKind
		} else {
			lit.K = adt.BytesKind
		}
		prefix := ""
		for i := 0; i < len(n.Elts); i += 2 {
			l, ok := n.Elts[i].(*ast.BasicLit)
			if !ok {
				return c.errf(n, "invalid interpolation")
			}
			s := l.Value
			if !strings.HasPrefix(s, prefix) {
				return c.errf(l, "invalid interpolation: unmatched ')'")
			}
			s = l.Value[prefixLen:]
			x := parseString(c, l, info, s)
			lit.Parts = append(lit.Parts, x)
			if i+1 < len(n.Elts) {
				lit.Parts = append(lit.Parts, c.expr(n.Elts[i+1]))
			}
			prefix = ")"
			prefixLen = 1
		}
		return lit

	case *ast.ParenExpr:
		return c.expr(n.X)

	case *ast.CallExpr:
		call := &adt.CallExpr{Src: n, Fun: c.expr(n.Fun)}
		for _, a := range n.Args {
			call.Args = append(call.Args, c.expr(a))
		}
		return call

	case *ast.UnaryExpr:
		switch n.Op {
		case token.NOT, token.ADD, token.SUB:
			return &adt.UnaryExpr{
				Src: n,
				Op:  adt.OpFromToken(n.Op),
				X:   c.expr(n.X),
			}
		case token.EQL:
			if !c.experiments.StructCmp {
				return c.errf(n, "unsupported unary operator %q", n.Op)
			}
			fallthrough
		case token.GEQ, token.GTR, token.LSS, token.LEQ,
			token.NEQ, token.MAT, token.NMAT:
			return &adt.BoundExpr{
				Src:  n,
				Op:   adt.OpFromToken(n.Op),
				Expr: c.expr(n.X),
			}

		case token.MUL:
			return c.errf(n, "preference mark not allowed at this position")
		default:
			return c.errf(n, "unsupported unary operator %q", n.Op)
		}

	case *ast.BinaryExpr:
		switch n.Op {
		case token.OR:
			d := &adt.DisjunctionExpr{Src: n}
			c.addDisjunctionElem(d, n.X, false)
			c.addDisjunctionElem(d, n.Y, false)
			return d

		default:
			op := adt.OpFromToken(n.Op)
			x := c.expr(n.X)
			y := c.expr(n.Y)
			if op != adt.AndOp {
				c.assertConcreteIsPossible(n.X, op, x)
				c.assertConcreteIsPossible(n.Y, op, y)
			}
			// return updateBin(c,
			return &adt.BinaryExpr{Src: n, Op: op, X: x, Y: y} // )
		}

	case *ast.PostfixExpr:
		switch n.Op {
		case token.ELLIPSIS:
			if c.experiments.ExplicitOpen {
				return &adt.OpenExpr{
					Src: n,
					X:   c.expr(n.X),
				}
			}
			return c.errf(n, "postfix ... operator requires @experiment(explicitopen)")
		default:
			return c.errf(n, "unsupported postfix operator %s", n.Op)
		}

	default:
		return c.errf(n, "%s values not allowed in this position", ast.Name(n))
	}
}

// listEllipsis reports the list type and remaining elements of a list. If we
// ever relax the usage of ellipsis, this function will likely change. Using
// this function will ensure keeping correct behavior or causing a compiler failure.
func listEllipsis(n *ast.ListLit) (elts []ast.Expr, e *ast.Ellipsis) {
	elts = n.Elts
	if n := len(elts); n > 0 {
		var ok bool
		if e, ok = elts[n-1].(*ast.Ellipsis); ok {
			elts = elts[:n-1]
		}
	}
	return elts, e
}

func (c *compiler) assertConcreteIsPossible(src ast.Node, op adt.Op, x adt.Expr) {
	if !adt.AssertConcreteIsPossible(op, x) {
		str := astinternal.DebugStr(src)
		c.errf(src, "invalid operand %s ('%s' requires concrete value)", str, op)
	}
}

func (c *compiler) addDisjunctionElem(d *adt.DisjunctionExpr, n ast.Expr, mark bool) {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.OR {
			c.addDisjunctionElem(d, x.X, mark)
			c.addDisjunctionElem(d, x.Y, mark)
			return
		}
	case *ast.UnaryExpr:
		if x.Op == token.MUL {
			d.HasDefaults = true
			c.addDisjunctionElem(d, x.X, true)
			return
		}
	}
	d.Values = append(d.Values, adt.Disjunct{Val: c.expr(n), Default: mark})
}

// TODO(perf): validate that regexps are cached at the right time.

func (c *compiler) parse(l *ast.BasicLit) (n adt.Expr) {
	s := l.Value
	if s == "" {
		return c.errf(l, "invalid literal %q", s)
	}
	switch l.Kind {
	case token.STRING:
		info, nStart, _, err := literal.ParseQuotes(s, s)
		if err != nil {
			return c.errf(l, "%s", err.Error())
		}
		s := s[nStart:]
		return parseString(c, l, info, s)

	case token.FLOAT, token.INT:
		err := literal.ParseNum(s, &c.num)
		if err != nil {
			return c.errf(l, "parse error: %v", err)
		}
		kind := adt.FloatKind
		if c.num.IsInt() {
			kind = adt.IntKind
		}
		n := &adt.Num{Src: l, K: kind}
		if err = c.num.Decimal(&n.X); err != nil {
			return c.errf(l, "error converting number to decimal: %v", err)
		}
		return n

	case token.TRUE:
		return &adt.Bool{Src: l, B: true}

	case token.FALSE:
		return &adt.Bool{Src: l, B: false}

	case token.NULL:
		return &adt.Null{Src: l}

	default:
		return c.errf(l, "unknown literal type")
	}
}

// parseString decodes a string without the starting and ending quotes.
func parseString(c *compiler, node ast.Expr, q literal.QuoteInfo, s string) (n adt.Expr) {
	str, err := q.Unquote(s)
	if err != nil {
		return c.errf(node, "invalid string: %v", err)
	}
	if q.IsDouble() {
		return &adt.String{Src: node, Str: str, RE: nil}
	}
	return &adt.Bytes{Src: node, B: []byte(str), RE: nil}
}
