// Copyright 2026 CUE Authors
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

package style

import "cuelang.org/go/cue/ast"

// mergeEllipsisDecls applies the [Config.Ellipsis] rule to a body's
// decl slice, mutating it in place. We remove every decl matching
// [isEllipsisDecl] and, if any were removed, append a single fresh
// trailing [*ast.Ellipsis] carrying the comments of every removed
// marker in source order, so no comment is lost. Returns true iff at
// least one marker was removed.
func mergeEllipsisDecls(decls *[]ast.Decl) bool {
	var mergedComments []*ast.CommentGroup
	kept := make([]ast.Decl, 0, len(*decls))
	removed := false
	for _, d := range *decls {
		if isEllipsisDecl(d) {
			removed = true
			mergedComments = append(mergedComments, ast.Comments(d)...)
			continue
		}
		kept = append(kept, d)
	}
	if !removed {
		return false
	}
	trailing := &ast.Ellipsis{}
	ast.SetComments(trailing, mergedComments)
	*decls = append(kept, trailing)
	return true
}

// isEllipsisDecl reports whether d is one of the AST shapes we treat as
// equivalent to a `...`:
//
//   - a literal [*ast.Ellipsis];
//   - an [*ast.Field] whose label is `[string]` or `[_]` (a pattern
//     constraint with a single-Ident pattern) and whose value is the
//     bare `_` identifier.
func isEllipsisDecl(d ast.Decl) bool {
	if e, ok := d.(*ast.Ellipsis); ok {
		return e.Type == nil
	}
	f, ok := d.(*ast.Field)
	if !ok {
		return false
	}
	v, ok := f.Value.(*ast.Ident)
	if !ok || v.Name != "_" {
		return false
	}
	l, ok := f.Label.(*ast.ListLit)
	if !ok || len(l.Elts) != 1 {
		return false
	}
	i, ok := l.Elts[0].(*ast.Ident)
	if !ok {
		return false
	}
	return i.Name == "string" || i.Name == "_"
}
