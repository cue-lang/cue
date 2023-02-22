// Copyright 2023 CUE Authors
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

package updater

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/types"
)

// Updater is used to rewrite an AST by replacing concrete values with
// a given set of other concrete values.
//
// Constraints or types are never replaced.
type Updater struct {
	ctx  *adt.OpContext
	root cue.Value

	// allowed keeps track of fields that may be modified.
	// Only fields that are not part of a constraint may be modified.
	allowed map[*ast.Field]bool
}

// New creates a new Updater for a single ast.File.
// Updates are made in place.
func New(ctx *cue.Context, f *ast.File) (*Updater, error) {
	v := ctx.BuildFile(f)
	if err := v.Err(); err != nil {
		return nil, err
	}
	var tv types.Value
	v.Core(&tv)

	u := &Updater{
		ctx:     adt.NewContext((*runtime.Runtime)(ctx), tv.V),
		root:    v,
		allowed: map[*ast.Field]bool{},
	}

	// Tag fields in the AST that are not part of a constraint.
	// Only these tagged fields are allowed to be modified during
	// updating.
	ast.Walk(f, func(n ast.Node) bool {
		switch f := n.(type) {
		case *ast.Field:
			if f.Optional != token.NoPos {
				return false
			}
			_, _, err := ast.LabelName(f.Label)
			if err != nil {
				return false
			}
			u.allowed[f] = true
		}
		return true
	}, nil)

	return u, nil
}

// Set updates the AST for the given path value pair.
func (u *Updater) Set(path cue.Path, value cue.Value) error {
	expr, ok := value.Syntax().(ast.Expr)
	if !ok {
		// Should never happen.
		return fmt.Errorf("value not an expression")
	}
	return u.injectExpr(path, expr)
}

// injectExpr replaces a concrete expr at the given path with expr or
// adds a new entry for the path-expr pair.
func (u *Updater) injectExpr(path cue.Path, expr ast.Expr) error {
	v := u.root.LookupPath(path)
	if v.Exists() && u.setExpr(v, expr) {
		return nil
	}

	return u.insertAtPath(path, expr)
}

// insertAtPath searches for a struct in which to place a single-line
// field sequence reflecting the path of expr relative to that
// position.
func (u *Updater) insertAtPath(path cue.Path, expr ast.Expr) error {
	for sels := path.Selectors(); len(sels) > 0; {
		last := sels[len(sels)-1]
		if !last.IsString() {
			return fmt.Errorf("selector %s is not a regular string field", last)
		}
		label := sels[len(sels)-1].Unquoted()
		sels = sels[:len(sels)-1]

		// Create an ast.Label and remove quotes if possible.
		var sel ast.Label
		if ast.IsValidIdent(label) {
			sel = ast.NewIdent(label)
		} else {
			sel = ast.NewString(label)
		}

		field := &ast.Field{
			Label: sel,
			Value: expr,
		}

		v := u.root.LookupPath(cue.MakePath(sels...))
		if v.Exists() && u.insertField(v, field) {
			// Start field on new line.
			ast.SetPos(sel, token.Newline.Pos())
			return nil
		}

		// No suitable insertion point found. Wrap in struct and repeat.
		expr = &ast.StructLit{Elts: []ast.Decl{field}}
	}

	// This should never happen as a field can always be inserted in a File.
	return fmt.Errorf("no insertion point for %v", path)
}

// setExpr looks for a conjunct in v that is associated with a field
// that may be replaced with the given value and reports whether it
// was successful.
func (u *Updater) setExpr(v cue.Value, expr ast.Expr) (found bool) {
	var tv types.Value
	v.Core(&tv)

	for _, c := range tv.V.Conjuncts {
		src := c.Source()
		f, ok := src.(*ast.Field)
		if !ok || !u.allowed[f] {
			continue
		}

		// Verify that the conjunct represents a concrete value.
		n := &adt.Vertex{}
		n.AddConjunct(c)
		n.Finalize(u.ctx)

		if n.IsConcrete() {
			// TODO: in case the original value is a struct, consider
			// replacing only the concrete embedded scalar.
			f.Value = expr
			found = true
		}
	}

	return found
}

// insertField looks for a conjunct in v in which to insert the given field and
// returns whether it found such a conjunct.
func (u *Updater) insertField(v cue.Value, field *ast.Field) bool {
	var tv types.Value
	v.Core(&tv)

	for _, c := range tv.V.Conjuncts {
		src := c.Source()
		switch f := src.(type) {
		case *ast.Field:
			s, ok := f.Value.(*ast.StructLit)
			if ok && u.allowed[f] {
				s.Elts = append(s.Elts, field)
				return true
			}

		case *ast.File:
			f.Decls = append(f.Decls, field)
			return true
		}
	}
	return false
}
