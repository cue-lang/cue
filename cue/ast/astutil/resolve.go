// Copyright 2018 The CUE Authors
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

// This file implements scopes and the objects they contain.

package astutil

import (
	"fmt"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// An ErrFunc processes errors.
type ErrFunc func(pos token.Pos, msg string, args ...interface{})

// TODO: future development
//
// Resolution currently assigns values along the table below. This is based on
// Go's resolver and is not quite convenient for CUE's purposes. For one, CUE
// allows manually setting resolution and than call astutil.Sanitize to
// normalize the ast.File. Manually assigning resolutions according to the
// below table is rather tedious though.
//
// Instead of using the Scope and Node fields in identifiers, we suggest the
// following assignments:
//
//    Reference Node // an Decl or Clause
//    Ident *Ident   // The identifier in References (optional)
//
// References always refers to the direct element in the scope in which the
// identifier occurs, not the final value, so: *Field, *LetClause, *ForClause,
// etc. In case Ident is defined, it must be the same pointer as the
// referencing identifier. In case it is not defined, the Name of the
// referencing identifier can be used to locate the proper identifier in the
// referenced node.
//
// The Scope field in the original design then loses its function.
//
// Type of reference      Scope          Node
// Let Clause             File/Struct    LetClause
// Alias declaration      File/Struct    Alias (deprecated)
// Illegal Reference      File/Struct
// Value
//   X in a: X=y          Field          Alias
// Fields
//   y in X: y            File/Struct    Expr (y)
//   X in X=x: y          File/Struct    Field
//   X in X=(x): y        File/Struct    Field
//   X in X="\(x)": y     File/Struct    Field
//   X in [X=x]: y        Field          Expr (x)
//   X in X=[x]: y        Field          Field
//
//   V in foo~(K,V): v    File/Struct    Field
//   K in foo~(K,V): v    Field          Expr "foo"
//   V in [x]~(K,V): y    Field          Field
//   K in [x]~(K,V): y    Field          Expr (x)
//   V in (x)~(K,V): y    File/Struct    Field
//   K in (x)~(K,V): y    Field          Expr (x)
//
// for k, v in            ForClause      Ident
// let x = y              LetClause      Ident
//
// Fields inside lambda
//    Label               Field          Expr
//    Value               Field          Field
// Pkg                    nil            ImportSpec

// TODO: allow passing all files in a package and mark nodes as predeclared
// identifiers.

// Resolve resolves all identifiers in a file, populating [ast.Ident.Node] fields.
// Unresolved identifiers are recorded in [ast.File.Unresolved].
// It will not overwrite already resolved identifiers.
// Resolving a file again replaces the list of unresolved identifiers
// rather than appending to it.
func Resolve(f *ast.File, errFn ErrFunc) {
	f.Unresolved = resolve(f, errFn)
}

// ResolveExpr resolves all identifiers in an expression.
// It will not overwrite already resolved values.
func ResolveExpr(e ast.Expr, errFn ErrFunc) {
	resolve(e, errFn)
}

// resolve resolves all identifiers in n, returning those it could not resolve.
func resolve(n ast.Node, errFn ErrFunc) (unresolved []*ast.Ident) {
	s := newRootScope(scope{
		errFn:      errFn,
		identFn:    resolveIdent,
		unresolved: &unresolved,
	})
	ast.Walk(n, s.Before, nil)
	return unresolved
}

// newRootScope returns the outermost scope of a resolution from s, adding the
// state its children share.
func newRootScope(s scope) *scope {
	s.index = make(map[string]entry)
	scopes := append(make([]*scope, 0, 8), &s)
	s.scopes = &scopes
	return &s
}

// A scope maintains the set of named language entities declared
// in the scope and a link to the immediately surrounding (outer)
// scope.
//
// Scopes are created and freed in stack order, and share one index of each
// name's innermost live declaration; each scope records what its own
// declarations replaced there, to restore it when freed. Only the innermost
// live scope looks up or declares names, except for [scope.declareRoot].
type scope struct {
	outer   *scope
	node    ast.Node
	depth   int32 // the number of scopes enclosing this one
	inField bool

	index map[string]entry // shared by all scopes

	// declared and shadowed hold the names this scope declares, the latter
	// with the outer declaration each one shadows.
	declared []string
	shadowed []shadow

	// ambiguous holds the names this scope declares for both a field and a
	// renamed alias, so that a reference by name may mean either.
	ambiguous []string

	// signature is the scope holding the parameters of the function whose
	// signature is being resolved, set on that scope and inherited by those
	// within it, and nil elsewhere. See [scope.reservedParam].
	signature *scope

	identFn func(s *scope, n *ast.Ident) bool
	nameFn  func(name string, e entry)
	errFn   func(p token.Pos, msg string, args ...interface{})
	// conflictFn, if set, renames an alias named like a field in scope,
	// and reports whether it did.
	conflictFn func(alias entry) bool

	// unresolved collects the identifiers which [resolveIdent] cannot
	// resolve. The pointer is shared between the root scope and all its
	// children.
	unresolved *[]*ast.Ident

	// scopes holds the scope at each depth, live or freed for reuse.
	// The pointer is shared between the root scope and all its children.
	scopes *[]*scope
}

type entry struct {
	node  ast.Node
	link  ast.Node   // Alias, LetClause, or Field
	field *ast.Field // Used for LabelAliases

	depth int32 // of the scope declaring the name

	// shadow is one more than the index of the declaration this one shadows
	// in its scope's shadowed list, or zero.
	shadow int32
}

// A shadow is an outer declaration of name which a scope's own replaced.
type shadow struct {
	name  string
	entry entry
}

// declarer returns the scope declaring e, which must be live.
func (s *scope) declarer(e entry) *scope {
	return (*s.scopes)[e.depth]
}

// shadows returns the declaration which e shadows, if any.
func (s *scope) shadows(e entry) (entry, bool) {
	if e.shadow == 0 {
		return entry{}, false
	}
	return s.declarer(e).shadowed[e.shadow-1].entry, true
}

// isAmbiguous reports whether a non-nil s holds name in [scope.ambiguous].
func (s *scope) isAmbiguous(name string) bool {
	return s != nil && slices.Contains(s.ambiguous, name)
}

func (s *scope) allocScope() *scope {
	depth := s.depth + 1
	scopes := *s.scopes
	if int(depth) < len(scopes) {
		scope := scopes[depth]
		if scope.node != nil {
			panic("scope allocated while another one is live at its depth")
		}
		return scope
	}
	scope := &scope{
		index:  s.index,
		depth:  depth,
		scopes: s.scopes,
	}
	*s.scopes = append(scopes, scope)
	return scope
}

func (s *scope) freeScope() {
	if len(s.shadowed) == 0 && len(s.declared) == len(s.index) {
		clear(s.index) // Every live name is declared in s.
	} else {
		for _, name := range s.declared {
			delete(s.index, name)
		}
		for _, sh := range s.shadowed {
			s.index[sh.name] = sh.entry
		}
	}
	// Ensure no pointers remain, which can hold onto memory.
	// We only reuse the capacity of the slices, and keep the shared state.
	*s = scope{
		depth:     s.depth,
		index:     s.index,
		declared:  s.declared[:0],
		shadowed:  s.shadowed[:0],
		ambiguous: s.ambiguous[:0],
		scopes:    s.scopes,
	}
}

// walkHiding walks expr with the declaration of name by n in s hidden, if any.
func (s *scope) walkHiding(name string, n ast.Node, expr ast.Expr) {
	e, ok := s.index[name]
	if !ok || e.depth != s.depth || e.node != n {
		ast.Walk(expr, s.Before, nil)
		return
	}
	if prev, ok := s.shadows(e); ok {
		s.index[name] = prev
	} else {
		delete(s.index, name)
	}
	ast.Walk(expr, s.Before, nil)
	s.index[name] = e
}

// declareRoot declares name in the root scope s while scopes within it are
// live, which requires that none of them declares the name.
func (s *scope) declareRoot(name string, n ast.Node) {
	if _, ok := s.index[name]; ok {
		panic(fmt.Sprintf("%q declared in the root scope while declared in scope", name))
	}
	s.insert(name, n, n, nil)
}

// freeScopesUntil frees all scopes from s up to (but not including) 'ancestor'.
func (s *scope) freeScopesUntil(ancestor *scope) {
	for s != ancestor {
		if s == nil {
			panic("ancestor scope not found")
		}
		next := s.outer
		s.freeScope()
		s = next
	}
}

func newScope(outer *scope, node ast.Node, decls []ast.Decl) *scope {
	s := outer.allocScope()
	s.outer = outer
	s.node = node
	s.inField = false
	s.signature = outer.signature
	s.identFn = outer.identFn
	s.nameFn = outer.nameFn
	s.errFn = outer.errFn
	s.conflictFn = outer.conflictFn
	s.unresolved = outer.unresolved

	// Most declarations are a name each, and shadow none.
	s.declared = slices.Grow(s.declared, len(decls))
	for _, d := range decls {
		switch x := d.(type) {
		case *ast.Field:
			label := x.Label

			if a, ok := x.Label.(*ast.Alias); ok {
				name := a.Ident.Name
				if _, ok := a.Expr.(*ast.ListLit); !ok {
					s.insert(name, x, a, nil)
				}
				if x.Alias != nil {
					// Error: cannot have both old-style label alias and postfix
					// alias
					s.errFn(x.Pos(),
						"field has both label alias and postfix alias")
				}
			}
			if _, isPattern := label.(*ast.ListLit); !isPattern {
				if a := x.Alias; a != nil {
					insertPostfixAliases(s, x, a.Label)
				}
			}

			// TODO(perf): replace labelName with quick tests: this generates an
			// error in many cases.
			name, isIdent, _ := ast.LabelName(label)
			if isIdent {
				v := x.Value
				// Avoid interpreting value aliases at this point.
				if a, ok := v.(*ast.Alias); ok {
					v = a.Expr
				}
				s.insert(name, v, x, nil)
			}
		case *ast.LetClause:
			name, isIdent, _ := ast.LabelName(x.Ident)
			if isIdent {
				s.insert(name, x, x, nil)
			}
		case *ast.Alias:
			name, isIdent, _ := ast.LabelName(x.Ident)
			if isIdent {
				s.insert(name, x, x, nil)
			}
		case *ast.ImportDecl:
			for _, spec := range x.Specs {
				info, _ := ParseImportSpec(spec)
				s.insert(info.Ident, spec, spec, nil)
			}
		}
	}
	return s
}

func newFuncScope(outer *scope, fn *ast.Func) *scope {
	s := outer.allocScope()
	s.outer = outer
	s.node = fn
	s.inField = false
	s.signature = outer.signature
	s.identFn = outer.identFn
	s.nameFn = outer.nameFn
	s.errFn = outer.errFn
	s.conflictFn = outer.conflictFn
	s.unresolved = outer.unresolved

	insertParam := func(name string, p *ast.FuncParam) {
		if name == "" || name == "_" {
			return
		}
		if e, ok := s.index[name]; ok && e.depth == s.depth {
			s.errFn(p.Pos(), "parameter %q redeclared in same scope", name)
			return
		}
		s.insert(name, p, p, nil)
	}
	for _, p := range fn.Params {
		if p == nil {
			continue
		}
		if a := p.Alias; a != nil && a.Field != nil && a.Field.Name != "_" {
			insertParam(a.Field.Name, p)
			continue
		}
		if p.Label != nil {
			name, isIdent, _ := ast.LabelName(p.Label)
			if isIdent {
				insertParam(name, p)
			}
		}
	}
	return s
}

func (s *scope) isLet(n, link ast.Node) bool {
	if _, ok := s.node.(*ast.Field); ok {
		return true
	}
	if _, ok := link.(*ast.PostfixAlias); ok {
		return true
	}
	switch n.(type) {
	case *ast.LetClause, *ast.TryClause, *ast.Alias, *ast.Field:
		return true
	}
	return false
}

func (s *scope) mustBeUnique(n, link ast.Node) bool {
	if _, ok := s.node.(*ast.Field); ok {
		return true
	}
	switch link.(type) {
	case *ast.PostfixAlias, *ast.ForClause:
		return true
	}
	switch n.(type) {
	// TODO: add *ast.ImportSpec when some implementations are moved over to
	// Sanitize.
	case *ast.ImportSpec, *ast.LetClause, *ast.TryClause, *ast.Alias, *ast.Field:
		return true
	}
	return false
}

func (s *scope) insert(name string, n, link ast.Node, f *ast.Field) {
	if name == "" {
		return
	}
	e := entry{node: n, link: link, field: f, depth: s.depth}
	// kept is the entry the name refers to in s: e, or a field
	// taking the place of a renamed alias.
	kept := e
	existing, found := s.index[name]
	// TODO: record both positions.
	if found && name != "_" {
		outer := s.declarer(existing)
		isAlias := s.isLet(n, link)
		conflict := isAlias != outer.isLet(existing.node, existing.link)
		if !conflict && isAlias && outer != s {
			// A field past the shadowed aliases conflicts too.
			_, conflict = s.aliasesPast(existing)
		}
		if conflict {
			alias := e
			if !isAlias {
				alias = existing
			}
			ok := s.conflictFn != nil && s.conflictFn(alias)
			if ok && !isAlias {
				// The aliases the renamed one shadowed conflict too.
				aliases, _ := s.aliasesPast(existing)
				for _, alias := range aliases {
					if ok = s.conflictFn(alias); !ok {
						break
					}
				}
			}
			if !ok {
				s.errFn(n.Pos(), "cannot have both alias and field with name %q in same scope", name)
				return
			}
			// In the same scope, the name refers to the field.
			if outer == s {
				if isAlias {
					kept = existing
				}
				s.ambiguous = append(s.ambiguous, name)
			}
		} else if s.mustBeUnique(n, link) || outer.mustBeUnique(existing.node, existing.link) {
			if outer == s {
				if _, ok := existing.node.(*ast.ImportSpec); ok {
					return
					// TODO:
					// s.errFn(n.Pos(), "conflicting declaration %s\n"+
					// 	"\tprevious declaration at %s",
					// 	name, existing.node.Pos())
				} else {
					s.errFn(n.Pos(), "alias %q redeclared in same scope", name)
				}
				return
			}
			// TODO: Should we disallow shadowing of aliases?
			// This was the case, but it complicates the transition to
			// square brackets. The spec says allow it.
			// s.errFn(n.Pos(), "alias %q already declared in enclosing scope", name)
		}
	}
	// nameFn runs after conflictFn, which may mark e for renaming.
	if s.nameFn != nil {
		s.nameFn(name, e)
	}
	switch {
	case !found:
		s.declared = append(s.declared, name)
	case existing.depth == s.depth:
		// A repeated declaration replaces the one in s.
		kept.shadow = existing.shadow
	default:
		s.shadowed = append(s.shadowed, shadow{name: name, entry: existing})
		kept.shadow = int32(len(s.shadowed))
	}
	s.index[name] = kept
}

// aliasesPast returns the aliases e shadows, innermost first, and whether a
// field is past them.
func (s *scope) aliasesPast(e entry) (aliases []entry, field bool) {
	for {
		var ok bool
		if e, ok = s.shadows(e); !ok {
			return aliases, false
		}
		if !s.declarer(e).isLet(e.node, e.link) {
			return aliases, true
		}
		aliases = append(aliases, e)
	}
}

// resolveScope returns the node of the scope declaring name as node, and its
// entry. The node is a File or StructLit, or the Field declaring a pattern
// constraint's label alias or a value alias.
func (s *scope) resolveScope(name string, node ast.Node) (scope ast.Node, e entry, ok bool) {
	for e, ok := s.index[name]; ok; e, ok = s.shadows(e) {
		if e.node == node {
			return s.declarer(e).node, e, true
		}
	}
	return nil, entry{}, false
}

func (s *scope) lookup(name string) (p *scope, obj ast.Node, node entry) {
	if name == "_" {
		return nil, nil, entry{}
	}
	n, ok := s.index[name]
	if !ok {
		return nil, nil, entry{}
	}
	p = s.declarer(n)
	if _, ok := n.node.(*ast.ImportSpec); ok {
		return p, nil, n
	}
	obj = p.node
	if n.field != nil {
		// Label alias case.
		obj = n.field
	}
	return p, obj, n
}

func insertPostfixAliases(s *scope, x *ast.Field, expr ast.Node) {
	a := x.Alias
	if a == nil {
		return
	}
	hasField := a.Field != nil && a.Field.Name != "_"

	if a.Label == nil {
		// Single form: ~X
		if !hasField {
			s.errFn(a.Pos(),
				"single postfix alias %q field cannot be the blank identifier", a.Field.Name)
		} else {
			s.insert(a.Field.Name, x, a, nil)
		}
		return
	}

	// Double form: ~(X,Y)
	hasLabel := a.Label != nil && a.Label.Name != "_"
	if !hasField && !hasLabel {
		s.errFn(a.Pos(),
			"both label and field in postfix alias cannot be the blank identifier")
		return
	}
	if hasLabel {
		s.insert(a.Label.Name, expr, a, x)
	}
	if hasField {
		s.insert(a.Field.Name, x, a, nil)
	}
}

func (s *scope) Before(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.File:
		s = newScope(s, x, x.Decls)
		defer s.freeScope()
		// Support imports.
		for _, d := range x.Decls {
			ast.Walk(d, s.Before, nil)
		}
		return false

	case *ast.StructLit:
		s = newScope(s, x, x.Elts)
		defer s.freeScope()
		for _, elt := range x.Elts {
			ast.Walk(elt, s.Before, nil)
		}
		return false

	case *ast.Func:
		// Parameter constraints, parameter defaults, and the return type
		// resolve in the enclosing scope; only the body gets a
		// function-parameter scope. A reference to another parameter is
		// reserved for a possible future dependent-parameter feature and is
		// reported as an error rather than silently binding to a like-named
		// field of an enclosing scope.
		if params := x.Parameters(); len(params) > 0 {
			cs := newFuncScope(s, x)
			cs.signature = cs
			for _, p := range params {
				if p == nil {
					continue
				}
				if p.Value != nil {
					ast.Walk(p.Value, cs.Before, nil)
				}
				if p.Default != nil {
					ast.Walk(p.Default, cs.Before, nil)
				}
			}
			if x.Ret != nil {
				ast.Walk(x.Ret, cs.Before, nil)
			}
			cs.freeScope()
		} else if x.Ret != nil {
			ast.Walk(x.Ret, s.Before, nil)
		}
		if x.Body != nil {
			s = newFuncScope(s, x)
			defer s.freeScope()
			ast.Walk(x.Body, s.Before, nil)
		}
		return false

	case *ast.Comprehension:
		outer := s
		s = scopeClauses(s, x.Clauses)
		ast.Walk(x.Value, s.Before, nil)
		// Walk the fallback clause in the OUTER scope, since fallback should not
		// have access to for/let variables from the comprehension clauses,
		// whose scopes are freed first to keep stack order.
		s.freeScopesUntil(outer)
		if x.Fallback != nil {
			ast.Walk(x.Fallback.Body, outer.Before, nil)
		}
		return false

	case *ast.Field:
		var n ast.Node = x.Label
		alias, ok := x.Label.(*ast.Alias)
		if ok {
			n = alias.Expr
		}

		switch label := n.(type) {
		case *ast.ParenExpr:
			ast.Walk(label, s.Before, nil)

		case *ast.Interpolation:
			ast.Walk(label, s.Before, nil)

		case *ast.ListLit:
			if len(label.Elts) != 1 {
				break
			}
			s = newScope(s, x, nil)
			defer s.freeScope()
			if alias != nil {
				if name, _, _ := ast.LabelName(alias.Ident); name != "" {
					s.insert(name, x, alias, nil)
				}
			}

			expr := label.Elts[0]

			if a, ok := expr.(*ast.Alias); ok {
				if x.Alias != nil {
					// Error: cannot have both old-style pattern alias and
					// postfix alias
					s.errFn(x.Pos(),
						"pattern constraint has both label alias and postfix alias")
				}
				expr = a.Expr

				// Add to current scope, instead of the value's, and allow
				// references to bind to these illegally.
				// We need this kind of administration anyway to detect
				// illegal name clashes, and it allows giving better error
				// messages. This puts the burden on clients of this library
				// to detect illegal usage, though.
				s.insert(a.Ident.Name, a.Expr, a, x)
			} else {
				insertPostfixAliases(s, x, expr)
			}

			ast.Walk(expr, nil, func(n ast.Node) {
				if x, ok := n.(*ast.Ident); ok {
					// Declarations are innermost first, as are scopes.
					e, ok := s.index[x.Name]
					for s := s; ok && s != nil && !s.inField; s = s.outer {
						if e.depth != s.depth {
							continue
						}
						// An import is not a field to match against.
						if _, isImport := e.node.(*ast.ImportSpec); !isImport {
							s.errFn(n.Pos(),
								"reference %q in label expression refers to field against which it would be matched", x.Name)
						}
						e, ok = s.shadows(e)
					}
				}
			})
			ast.Walk(expr, s.Before, nil)
		}

		if n := x.Value; n != nil {
			// Handle value aliases.
			if alias, ok := x.Value.(*ast.Alias); ok {
				// TODO: this should move into Before once decl attributes
				// have been fully deprecated and embed attributes are introduced.
				s = newScope(s, x, nil)
				defer s.freeScope()
				s.insert(alias.Ident.Name, alias, x, nil)
				n = alias.Expr
			}
			s.inField = true
			ast.Walk(n, s.Before, nil)
			s.inField = false
		}

		return false

	case *ast.LetClause:
		// Disallow referring to the current LHS name, unless a field took
		// its place; see [scope.conflictFn].
		//
		// Set inField so that the label expression check in pattern constraints
		// does not walk beyond the let clause's value. A let clause's value is
		// a separate context, just like a field value.
		s.inField = true
		s.walkHiding(x.Ident.Name, x, x.Expr)
		s.inField = false
		return false

	case *ast.Alias:
		// Disallow referring to the current LHS name, as for a let clause.
		s.walkHiding(x.Ident.Name, x, x.Expr)
		return false

	case *ast.ImportSpec:
		return false

	case *ast.Attribute:
		// TODO: tokenize attributes, resolve identifiers and store the ones
		// that resolve in a list.

	case *ast.SelectorExpr:
		ast.Walk(x.X, s.Before, nil)
		return false

	case *ast.Ident:
		if s.signature != nil && s.reservedParam(x) {
			return false
		}
		if s.identFn(s, x) {
			return false
		}
	}
	return true
}

// reservedParam reports whether x, inside a function signature, refers to a
// parameter of that or an enclosing function's signature, reporting the
// reference as an error. A closer binding shadows the parameter as usual.
// The check precedes identFn so as to compose with those of Sanitize.
func (s *scope) reservedParam(x *ast.Ident) bool {
	name, ok, _ := ast.LabelName(x)
	if !ok {
		return false
	}
	p, _, node := s.lookup(name)
	if _, isParam := node.node.(*ast.FuncParam); isParam && p.signature == p {
		s.errFn(x.Pos(),
			"cannot refer to parameter %q in a parameter constraint or return type", name)
		return true
	}
	return false
}

func resolveIdent(s *scope, x *ast.Ident) bool {
	name, ok, _ := ast.LabelName(x)
	if !ok {
		// TODO: generate error
		return false
	}
	if _, obj, node := s.lookup(name); node.node != nil {
		switch x.Node {
		case nil:
			x.Node = node.node
			x.Scope = obj

		case node.node:
			x.Scope = obj

		default: // x.Node != node
			scope, _, ok := s.resolveScope(name, x.Node)
			if !ok {
				*s.unresolved = append(*s.unresolved, x)
			}
			x.Scope = scope
		}
	} else {
		*s.unresolved = append(*s.unresolved, x)
	}
	return true
}

func scopeClauses(s *scope, clauses []ast.Clause) *scope {
	for _, c := range clauses {
		switch x := c.(type) {
		case *ast.ForClause:
			ast.Walk(x.Source, s.Before, nil)
			s = newScope(s, x, nil)
			if x.Key != nil {
				s.insert(x.Key.Name, x.Key, x, nil)
			}
			s.insert(x.Value.Name, x.Value, x, nil)

		case *ast.LetClause:
			ast.Walk(x.Expr, s.Before, nil)
			s = newScope(s, x, nil)
			s.insert(x.Ident.Name, x.Ident, x, nil)

		case *ast.TryClause:
			// For the assignment form (try x = expr), handle scope like LetClause.
			if x.Ident != nil {
				ast.Walk(x.Expr, s.Before, nil)
				s = newScope(s, x, nil)
				s.insert(x.Ident.Name, x.Ident, x, nil)
			} else {
				// For the struct form (try { ... }), just walk normally.
				ast.Walk(c, s.Before, nil)
			}

		default:
			ast.Walk(c, s.Before, nil)
		}
	}
	return s
}

// Debugging support
func (s *scope) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scope %p {", s)
	if s != nil && len(s.declared)+len(s.shadowed) > 0 {
		fmt.Fprintln(&b)
		for _, name := range s.declared {
			fmt.Fprintf(&b, "\t%v\n", name)
		}
		for _, sh := range s.shadowed {
			fmt.Fprintf(&b, "\t%v\n", sh.name)
		}
	}
	fmt.Fprintf(&b, "}\n")
	return b.String()
}
