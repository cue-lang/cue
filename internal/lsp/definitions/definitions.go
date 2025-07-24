package definitions

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

type Definitions struct {
	byFile map[string]resolutions
	root   *scope
}

type resolutions struct {
	positions [][]token.Position
	file      *token.File
}

type scope struct {
	dfns   *Definitions
	parent *scope
	// unprocessed holds the nodes that make up this scope. Once a call
	// to [scope.eval] has returned, unprocessed must never be altered.
	unprocessed []ast.Node
	// keyPositions holds the positions that are considered to define
	// this scope. For example, if a scope represents `a: {}` then
	// keyPositions will hold the location of the `a`. Due to implicit
	// unification, keyPositions may contain several positions.
	keyPositions []token.Position
	// resolvesTo points to the scopes reachable from nodes which are
	// embedded within this scope.
	resolvesTo []*scope
	aliases    []string
	allScopes  []*scope
	fields     map[string]*scope
}

func Analyse(files ...*ast.File) *Definitions {
	dfns := &Definitions{
		byFile: make(map[string]resolutions),
	}
	root := dfns.newScope(nil, token.NoPos)
	dfns.root = root

	for _, file := range files {
		dfns.byFile[file.Filename] = resolutions{
			positions: make([][]token.Position, file.End().Offset()),
			file:      file.Pos().File(),
		}
		root.unprocessed = append(root.unprocessed, file)
		root.keyPositions = append(root.keyPositions, file.Pos().Position())
	}

	root.EvalAll()
	return dfns
}

func (dfns *Definitions) ForFile(file string) (*token.File, [][]token.Position) {
	r, found := dfns.byFile[file]
	if !found {
		return nil, nil
	}
	return r.file, r.positions
}

func (dfns *Definitions) newScope(parent *scope, keyPos token.Pos, unprocessed ...ast.Node) *scope {
	return &scope{
		dfns:         dfns,
		parent:       parent,
		unprocessed:  unprocessed,
		keyPositions: []token.Position{keyPos.Position()},
		fields:       make(map[string]*scope),
	}
}

func (s *scope) newScope(keyPos token.Pos, unprocessed ...ast.Node) *scope {
	r := s.dfns.newScope(s, keyPos, unprocessed...)
	s.allScopes = append(s.allScopes, r)
	return r
}

func (s *scope) Dump(depth int) {
	fmt.Printf("%*sScope %p\n", depth*3, "", s)
	fmt.Printf("%*s Positions %v\n", depth*3, "", s.keyPositions)

	if len(s.aliases) != 0 {
		fmt.Printf("%*s Aliases: %v\n", depth*3, "", s.aliases)
	}

	if len(s.fields) != 0 {
		fmt.Printf("%*s Fields:\n", depth*3, "")
		for name, r := range s.fields {
			fmt.Printf("%*s  %s:\n", depth*3, "", name)
			r.Dump(depth + 1)
		}
	}

	if len(s.allScopes) != 0 {
		fmt.Printf("%*s All scopes:\n", depth*3, "")
		for _, r := range s.allScopes {
			r.Dump(depth + 1)
		}
	}
}

func (s *scope) EvalAll() {
	s.eval()
	for _, r := range s.allScopes {
		r.EvalAll()
	}
}

func (s *scope) eval() {
	if s.unprocessed == nil {
		return
	}

	unprocessed := s.unprocessed
	s.unprocessed = nil

	var embeddedResolvable, resolvable []ast.Expr

	for len(unprocessed) != 0 {
		n := unprocessed[0]
		unprocessed = unprocessed[1:]

		switch n := n.(type) {
		case *ast.File:
			for _, decl := range n.Decls {
				unprocessed = append(unprocessed, decl)
			}

		case *ast.StructLit:
			for _, elt := range n.Elts {
				unprocessed = append(unprocessed, elt)
			}

		case *ast.ListLit:
			for i, elt := range n.Elts {
				unprocessed = append(unprocessed, &ast.Field{
					Label:      ast.NewIdent(fmt.Sprint(i)),
					Constraint: token.ILLEGAL,
					TokenPos:   elt.Pos(),
					Token:      token.COLON,
					Value:      elt,
				})
			}

		case *ast.Interpolation:
			resolvable = append(resolvable, n.Elts...)

		case *ast.EmbedDecl:
			unprocessed = append(unprocessed, n.Expr)

		case *ast.ParenExpr:
			unprocessed = append(unprocessed, n.X)

		case *ast.UnaryExpr:
			resolvable = append(resolvable, n.X)

		case *ast.BinaryExpr:
			switch n.Op {
			case token.AND:
				unprocessed = append(unprocessed, n.X, n.Y)
			case token.OR:
				lhs := s.newScope(n.X.Pos(), n.X)
				rhs := s.newScope(n.Y.Pos(), n.Y)
				s.resolvesTo = append(s.resolvesTo, lhs, rhs)
			default:
				resolvable = append(resolvable, n.X, n.Y)
			}

		case *ast.Alias:
			// X=e
			s.aliases = append(s.aliases, n.Ident.Name)
			unprocessed = append(unprocessed, n.Expr)

		case *ast.Ellipsis:
			unprocessed = append(unprocessed, n.Type)

		case *ast.CallExpr:
			resolvable = append(resolvable, n.Args...)

		case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
			embeddedResolvable = append(embeddedResolvable, n.(ast.Expr))

		case *ast.Comprehension:
			parent := s
			for _, clause := range n.Clauses {
				cur := parent.newScope(clause.Pos(), clause)
				parent = cur
			}
			if parent != s {
				parent.newScope(n.Value.Pos(), n.Value)
			}

		case *ast.IfClause:
			unprocessed = append(unprocessed, n.Condition)

		case *ast.LetClause:
			s.ensureField(n.Ident.Name, n.Ident.Pos(), n.Expr)

		case *ast.ForClause:
			if n.Key != nil {
				s.ensureField(n.Key.Name, n.Key.Pos())
			}
			if n.Value != nil {
				s.ensureField(n.Value.Name, n.Value.Pos())
			}
			resolvable = append(resolvable, n.Source)

		case *ast.Field:
			l := n.Label

			alias, isAlias := l.(*ast.Alias)
			if isAlias {
				if expr, ok := alias.Expr.(ast.Label); ok {
					l = expr
				}
			}

			var field *scope
			switch l := l.(type) {
			case *ast.Ident:
				field = s.ensureField(l.Name, l.Pos(), n.Value)
			case *ast.BasicLit:
				field = s.ensureField(l.Value, l.Pos(), n.Value)
			default:
				field = s.newScope(l.Pos(), n.Value)
			}

			if isAlias {
				switch alias.Expr.(type) {
				case *ast.ListLit:
					// X=[e]: field
					// X is only visible within field
					field.aliases = append(field.aliases, alias.Ident.Name)
				case ast.Label:
					// X=ident: field
					// X="basic": field
					// X="\(e)": field
					// X=(e): field
					// X is visible within s
					s.fields[alias.Ident.Name] = field
				}
			}

			switch l := l.(type) {
			case *ast.Interpolation:
				resolvable = append(resolvable, l.Elts...)
			case *ast.ParenExpr:
				// Although the spec supports this, the parser doesn't seem to.
				// if alias, ok := l.X.(*ast.Alias); ok {
				// 	// (X=e): field
				// 	// X is visible within s
				// 	s.ensureField(alias.Ident.Name, alias.Ident.Pos(), alias.Expr)
				// } else {
				resolvable = append(resolvable, l.X)
				// }
			case *ast.ListLit:
				for _, elt := range l.Elts {
					if alias, ok := elt.(*ast.Alias); ok {
						// [X=e]: field
						// X is only visible within field. Given that X
						// refers to the field's key and not the field's
						// value, we can't treat X as an alias for the
						// field, and so we inject a wrapping scope instead:
						wrapper := s.newScope(alias.Ident.Pos())
						wrapper.ensureField(alias.Ident.Name, alias.Ident.Pos(), alias.Expr)
						field.parent = wrapper
					} else {
						resolvable = append(resolvable, elt)
					}
				}
			}
		}
	}

	for _, expr := range embeddedResolvable {
		scopes := s.resolve(expr)
		s.resolvesTo = append(s.resolvesTo, scopes...)
	}
	for _, expr := range resolvable {
		s.allScopes = append(s.allScopes, s.resolve(expr)...)
	}
}

func (dfns *Definitions) addResolution(start token.Pos, length int, targets []token.Position) {
	startPosition := start.Position()
	filename := startPosition.Filename
	offsets := dfns.byFile[filename].positions
	startOffset := startPosition.Offset
	for i := range length {
		offsets[startOffset+i] = append(offsets[startOffset+i], targets...)
	}
}

func (s *scope) resolve(e ast.Expr) (scopes []*scope) {
	switch e := e.(type) {
	case *ast.Ident:
		resolved := s.resolvePathRoot(e)
		if resolved == nil {
			return nil
		} else {
			s.dfns.addResolution(e.NamePos, len(e.Name), resolved.keyPositions)
			return []*scope{resolved}
		}

	case *ast.SelectorExpr:
		resolved := s.resolve(e.X)
		if len(resolved) == 0 {
			return nil
		}
		scopesSet := make(map[*scope]struct{})
		for len(resolved) > 0 {
			r := resolved[0]
			resolved = resolved[1:]
			if _, seen := scopesSet[r]; seen {
				continue
			}
			scopesSet[r] = struct{}{}
			r.eval()
			resolved = append(resolved, r.resolvesTo...)
		}
		name := ""
		switch l := e.Sel.(type) {
		case *ast.Ident:
			name = l.Name
		case *ast.BasicLit:
			name = l.Value
		default:
			return nil
		}

		for r := range scopesSet {
			if field, found := r.fields[name]; found {
				scopes = append(scopes, field)
				s.dfns.addResolution(e.Sel.Pos(), len(name), field.keyPositions)
			}
		}
		if len(scopes) == 0 {
			return nil
		}
		return scopes

	case *ast.IndexExpr:
		return append(s.resolve(e.X), s.resolve(e.Index)...)

	case *ast.StructLit, *ast.ListLit:
		return []*scope{s.newScope(e.Pos(), e)}

	case *ast.ParenExpr:
		return s.resolve(e.X)

	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND, token.OR:
			return append(s.resolve(e.X), s.resolve(e.Y)...)
		}
	}

	return nil
}

func (s *scope) resolvePathRoot(ident *ast.Ident) *scope {
	name := ident.Name
	for ancestor := s; ancestor != nil; ancestor = ancestor.parent {
		if slices.Contains(ancestor.aliases, name) {
			return ancestor
		}
		if field, found := ancestor.fields[name]; found {
			return field
		}
	}
	return nil
}

func (s *scope) ensureField(name string, keyPos token.Pos, unprocessed ...ast.Node) *scope {
	field, found := s.fields[name]
	if found {
		field.unprocessed = append(field.unprocessed, unprocessed...)

		keyPosition := keyPos.Position()
		if !slices.Contains(field.keyPositions, keyPosition) {
			field.keyPositions = append(field.keyPositions, keyPosition)
		}

	} else {
		field = s.newScope(keyPos, unprocessed...)
		s.fields[name] = field
	}
	return field
}

func (s *scope) isAncestorOf(other *scope) bool {
	for other = other.parent; other != nil; other = other.parent {
		if other == s {
			return true
		}
	}
	return false
}
