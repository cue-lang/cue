package definitions

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/lsp/rangeset"
)

type Definitions struct {
	byFilename map[string]*FileDefinitions
}

func Analyse(files ...*ast.File) *Definitions {
	dfns := &Definitions{
		byFilename: make(map[string]*FileDefinitions),
	}
	root := dfns.newScope(nil, nil)

	for _, file := range files {
		dfns.byFilename[file.Filename] = &FileDefinitions{
			root:        root,
			resolutions: make([][]ast.Node, file.End().Offset()),
			File:        file.Pos().File(),
		}
		root.unprocessed = append(root.unprocessed, file)
	}

	return dfns
}

func (dfns *Definitions) ForFile(filename string) *FileDefinitions {
	return dfns.byFilename[filename]
}

func (dfns *Definitions) newScope(parent *scope, key ast.Node, unprocessed ...ast.Node) *scope {
	s := &scope{
		dfns:        dfns,
		parent:      parent,
		unprocessed: unprocessed,
		fields:      make(map[string]*scope),
	}
	if key != nil {
		s.keys = []ast.Node{key}
		s.addRange(key)
	}
	return s
}

func (dfns *Definitions) addResolution(start token.Pos, length int, targets []ast.Node) {
	startPosition := start.Position()
	filename := startPosition.Filename
	offsets := dfns.byFilename[filename].resolutions
	startOffset := startPosition.Offset
	for i := range length {
		offsets[startOffset+i] = append(offsets[startOffset+i], targets...)
	}
}

type FileDefinitions struct {
	root        *scope
	resolutions [][]ast.Node
	File        *token.File
}

func (fdfns *FileDefinitions) ForOffset(offset int) []ast.Node {
	if offset < 0 || offset >= len(fdfns.resolutions) {
		return nil
	}
	nodes := fdfns.resolutions[offset]
	if nodes != nil {
		return nodes
	}
	fdfns.resolutions[offset] = []ast.Node{}

	filename := fdfns.File.Name()
	root := fdfns.root
	root.eval()
	seen := make(map[*scope]struct{})
	worklist := []*scope{root}
	for len(worklist) > 0 {
		s := worklist[0]
		worklist = worklist[1:]

		if _, found := seen[s]; found {
			continue
		}
		seen[s] = struct{}{}

		for _, s := range s.allScopes {
			s.eval()
			if s.contains(filename, offset) {
				worklist = append(worklist, s)
			}
		}
	}

	return fdfns.resolutions[offset]
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
	keys []ast.Node
	// resolvesTo points to the scopes reachable from nodes which are
	// embedded within this scope.
	resolvesTo []*scope
	aliases    []string
	allScopes  []*scope
	fields     map[string]*scope
	ranges     map[string]*rangeset.RangeSet
}

func (s *scope) addRange(n ast.Node) {
	if s.ranges == nil {
		s.ranges = make(map[string]*rangeset.RangeSet)
	}
	start := n.Pos().Position()
	end := n.End().Position()
	rs, found := s.ranges[start.Filename]
	if !found {
		rs = rangeset.NewRangeSet()
		s.ranges[start.Filename] = rs
	}
	rs.Add(start.Offset, end.Offset)
}

func (s *scope) contains(filename string, offset int) bool {
	rs, found := s.ranges[filename]
	return found && rs.Contains(offset)
}

func (s *scope) newScope(key ast.Node, unprocessed ...ast.Node) *scope {
	r := s.dfns.newScope(s, key, unprocessed...)
	s.allScopes = append(s.allScopes, r)
	return r
}

func (s *scope) dump(depth int) {
	fmt.Printf("%*sScope %p\n", depth*3, "", s)
	fmt.Printf("%*s Ranges %v\n", depth*3, "", s.ranges)

	if len(s.aliases) != 0 {
		fmt.Printf("%*s Aliases: %v\n", depth*3, "", s.aliases)
	}

	if len(s.fields) != 0 {
		fmt.Printf("%*s Fields:\n", depth*3, "")
		for name, r := range s.fields {
			fmt.Printf("%*s  %s:\n", depth*3, "", name)
			r.dump(depth + 1)
		}
	}

	if len(s.allScopes) != 0 {
		fmt.Printf("%*s All scopes:\n", depth*3, "")
		for _, r := range s.allScopes {
			r.dump(depth + 1)
		}
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

		s.addRange(n)

		switch n := n.(type) {
		case *ast.File:
			for _, decl := range n.Decls {
				unprocessed = append(unprocessed, decl)
			}

		case *ast.ImportDecl:
			for _, spec := range n.Specs {
				unprocessed = append(unprocessed, spec)
			}

		case *ast.ImportSpec:
			if n.Name == nil {
				str, err := literal.Unquote(n.Path.Value)
				if err != nil {
					continue
				}
				ip := ast.ParseImportPath(str).Canonical()
				if ip.Qualifier != "" {
					s.ensureField(ip.Qualifier, n)
				}
			} else {
				s.ensureField(n.Name.Name, n)
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
				lhs := s.newScope(nil, n.X)
				rhs := s.newScope(nil, n.Y)
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
				cur := parent.newScope(nil, clause)
				// We need to make sure that the comprehension value
				// (i.e. body) and all subsequent clauses, can be reached
				// by traversing through all clauses. The simplest way to
				// do this is just to include the whole range of n within
				// each descendent.
				cur.addRange(n)
				parent = cur
			}
			if parent != s {
				parent.newScope(nil, n.Value)
			}

		case *ast.IfClause:
			unprocessed = append(unprocessed, n.Condition)

		case *ast.LetClause:
			s.ensureField(n.Ident.Name, n.Ident, n.Expr)

		case *ast.ForClause:
			if n.Key != nil {
				s.ensureField(n.Key.Name, n.Key)
			}
			if n.Value != nil {
				s.ensureField(n.Value.Name, n.Value)
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
				field = s.ensureField(l.Name, l, n.Value)
			case *ast.BasicLit:
				field = s.ensureField(l.Value, l, n.Value)
			default:
				field = s.newScope(l, n.Value)
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
				// 	s.ensureField(alias.Ident.Name, alias.Ident, alias.Expr)
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
						wrapper := s.newScope(nil)
						wrapper.ensureField(alias.Ident.Name, alias.Ident, alias.Expr)
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

func (s *scope) resolve(e ast.Expr) (scopes []*scope) {
	switch e := e.(type) {
	case *ast.Ident:
		resolved := s.resolvePathRoot(e)
		if resolved == nil {
			return nil
		} else {
			s.dfns.addResolution(e.NamePos, len(e.Name), resolved.keys)
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
				s.dfns.addResolution(e.Sel.Pos(), len(name), field.keys)
			}
		}
		if len(scopes) == 0 {
			return nil
		}
		return scopes

	case *ast.IndexExpr:
		return append(s.resolve(e.X), s.resolve(e.Index)...)

	case *ast.StructLit, *ast.ListLit:
		return []*scope{s.newScope(nil, e)}

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

func (s *scope) ensureField(name string, key ast.Node, unprocessed ...ast.Node) *scope {
	field, found := s.fields[name]
	if found {
		field.unprocessed = append(field.unprocessed, unprocessed...)

		if key != nil {
			if !slices.Contains(field.keys, key) {
				field.keys = append(field.keys, key)
			}
		}

	} else {
		field = s.newScope(key, unprocessed...)
		s.fields[name] = field
	}
	return field
}
