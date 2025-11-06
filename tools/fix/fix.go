// Copyright 2019 CUE Authors
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

// Package fix contains functionality for writing CUE files with legacy
// syntax to newer ones.
//
// Note: the transformations that are supported in this package will change
// over time.
package fix

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueexperiment"
)

type Option func(*options)

type options struct {
	simplify       bool
	exps           []string
	upgradeVersion string
}

// Simplify enables fixes that simplify the code, but are not strictly
// necessary.
func Simplify() Option {
	return func(o *options) { o.simplify = true }
}

// Experiments enables fixes for specific experiments. Depending on the version
// of the module, this may result in adding an @experiment attribute to the
// top of the file for the given experiment.
func Experiments(experiment ...string) Option {
	return func(o *options) {
		// Deduplicate experiments to avoid running the same fix twice
		for _, exp := range experiment {
			if !slices.Contains(o.exps, exp) {
				o.exps = append(o.exps, exp)
			}
		}
	}
}

// UpgradeVersion enables upgrade fixes to the specified language version,
// applying all accepted experiments up to that version.
func UpgradeVersion(version string) Option {
	return func(o *options) {
		o.upgradeVersion = version
	}
}

// File applies fixes to f and returns it. It alters the original f.
func File(f *ast.File, o ...Option) *ast.File {
	f, err := file(f, "", o...)
	// TODO: this File method is public, and its signature was fixed
	// before we started calling Sanitize. Ideally, we want to return
	// this error, but that would require deprecating this File method,
	// and creating a new one, which might happen in due course if we
	// also discover that we need to be a bit more flexible than just
	// accepting a File.
	if err != nil {
		panic(err)
	}
	return f
}

func file(f *ast.File, version string, o ...Option) (*ast.File, errors.Error) {
	existingExps := f.Pos().Experiment()
	if version == "" {
		version = existingExps.LanguageVersion()
	}

	options := options{}
	for _, f := range o {
		f(&options)
	}

	// Handle upgrade version logic
	targetVersion := version
	if options.upgradeVersion != "" {
		targetVersion = options.upgradeVersion

		// Add accepted experiments for the target version to the experiment list
		acceptedExps := cueexperiment.GetUpgradable(version, targetVersion)
		for _, exp := range acceptedExps {
			// Only add if not already in the list and if it would make changes
			if !slices.Contains(options.exps, exp) {
				options.exps = append(options.exps, exp)
			}
		}
	}

	// Handle --exp=all (only valid as a lone argument)
	if slices.Equal(options.exps, []string{"all"}) {
		activeExps := cueexperiment.GetActive(version, targetVersion)
		options.exps = activeExps
	}

	for _, exp := range options.exps {
		if err := cueexperiment.CanApplyFix(exp, version, targetVersion); err != nil {
			return nil, errors.Newf(token.NoPos, "fix: %v", err)
		}
	}

	// TODO: should this error be wrapped better to not panic?
	wantExps, err := cueexperiment.NewFile(targetVersion, options.exps...)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "fix: invalid experiment")
	}

	if wantExps.ExplicitOpen && !existingExps.ExplicitOpen {
		f = fixExperiment(fixExplicitOpen, f, "explicitopen", targetVersion)
	}

	if wantExps.AliasV2 && !existingExps.AliasV2 {
		f = fixExperiment(fixAliasV2, f, "aliasv2", targetVersion)
	}

	// Make sure we use the "after" function, and not the "before",
	// because "before" will stop recursion early which creates
	// problems with nested expressions.
	f = astutil.Apply(f, nil, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.BinaryExpr:
			switch n.Op {
			case token.IDIV, token.IMOD, token.IQUO, token.IREM:
				// Rewrite integer division operations to use builtins.
				ast.SetRelPos(n.X, token.NoSpace)
				c.Replace(&ast.CallExpr{
					// Use the __foo version to prevent accidental shadowing.
					Fun:  ast.NewIdent("__" + n.Op.String()),
					Args: []ast.Expr{n.X, n.Y},
				})

			case token.ADD, token.MUL:
				// The fix here only works when at least one argument is a
				// literal list. It would be better to be able to use CUE
				// to infer type information, and then apply the fix to
				// all places where we infer a list argument.
				x, y := n.X, n.Y
				_, xIsList := x.(*ast.ListLit)
				_, yIsList := y.(*ast.ListLit)
				_, xIsConcat := concatCallArgs(x)
				_, yIsConcat := concatCallArgs(y)

				if n.Op == token.ADD {
					if !(xIsList || xIsConcat || yIsList || yIsConcat) {
						break
					}
					// Rewrite list addition to use list.Concat
					exprs := expandConcats(x, y)
					ast.SetRelPos(x, token.NoSpace)
					c.Replace(ast.NewCall(
						ast.NewSel(&ast.Ident{
							Name: "list",
							Node: ast.NewImport(nil, "list"),
						}, "Concat"), ast.NewList(exprs...)),
					)

				} else {
					if !(xIsList || yIsList) {
						break
					}
					// Rewrite list multiplication to use list.Repeat
					if !xIsList {
						x, y = y, x
					}
					ast.SetRelPos(x, token.NoSpace)
					c.Replace(ast.NewCall(
						ast.NewSel(&ast.Ident{
							Name: "list",
							Node: ast.NewImport(nil, "list"),
						}, "Repeat"), x, y),
					)
				}
			}
		}
		return true
	}).(*ast.File)

	if options.simplify {
		f = simplify(f)
	}

	err = astutil.Sanitize(f)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "fix: sanitize failed")
	}
	return f, nil
}

func expandConcats(exprs ...ast.Expr) (result []ast.Expr) {
	for _, expr := range exprs {
		list, ok := concatCallArgs(expr)
		if ok {
			result = append(result, expandConcats(list.Elts...)...)
		} else {
			result = append(result, expr)
		}
	}
	return result
}

func concatCallArgs(expr ast.Expr) (*ast.ListLit, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	name, ok := sel.X.(*ast.Ident)
	if !ok || name.Name != "list" {
		return nil, false
	}
	name, ok = sel.Sel.(*ast.Ident)
	if !ok || name.Name != "Concat" {
		return nil, false
	}
	if len(call.Args) != 1 {
		return nil, false
	}
	list, ok := call.Args[0].(*ast.ListLit)
	if !ok {
		return nil, false
	}
	return list, true
}

func fixExperiment(fn func(*ast.File) (*ast.File, bool), f *ast.File, exp, version string) *ast.File {
	result, hasChanges := fn(f)

	// If we made any changes, add the @experiment(explicitopen) attribute at the top
	if hasChanges {
		// there must be at least one decl for there to be any changes.
		ast.SetRelPos(result.Decls[0], token.NewSection)

		if !cueexperiment.IsStable(exp, version) {
			expAttr := &ast.Attribute{Text: fmt.Sprintf("@experiment(%s)", exp)}

			// Insert the attribute as the first declaration
			result.Decls = append([]ast.Decl{expAttr}, result.Decls...)
		}
	}

	return result
}

func fixExplicitOpen(f *ast.File) (result *ast.File, hasChanges bool) {
	result = astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.EmbedDecl:
			// Check if the embedded expression needs to be "opened" with ellipsis
			switch x := n.Expr.(type) {
			case *ast.PostfixExpr:
				// Already has ellipsis
				return true
			case *ast.BinaryExpr:
				if x.Op != token.AND {
					return true
				}
			case *ast.ListLit, // Lists cannot be opened anyway (atm).
				*ast.StructLit, // Structs are open by default
				*ast.BasicLit,
				*ast.Interpolation,
				*ast.UnaryExpr:

				return true
			default:
				// Needs ellipsis
			}

			// Transform the embedding to use postfix ellipsis
			postfixExpr := &ast.PostfixExpr{
				X:     n.Expr,
				Op:    token.ELLIPSIS,
				OpPos: n.Expr.End(),
			}

			c.Replace(&ast.EmbedDecl{
				Expr: postfixExpr,
			})
			hasChanges = true
		}
		return true
	}, func(c astutil.Cursor) bool {
		if c.Modified() {
			if n, ok := c.Node().(*ast.Field); ok && !internal.IsDefinition(n.Label) {
				ast.SetRelPos(n.Value, token.NoSpace)
				n.Value = ast.NewCall(ast.NewIdent("__reclose"), n.Value)
			}
		}
		return true
	}).(*ast.File)

	return result, hasChanges
}

func fixAliasV2(f *ast.File) (result *ast.File, hasChanges bool) {
	// Run multiple passes until no more changes are made, to handle nested aliases
	for {
		var changed bool
		result, changed = fixAliasV2Pass(f)
		if changed {
			hasChanges = true
			f = result
		} else {
			break
		}
	}
	return result, hasChanges
}

func fixAliasV2Pass(f *ast.File) (result *ast.File, hasChanges bool) {
	result = astutil.Apply(f, func(c astutil.Cursor) bool {
		n, ok := c.Node().(*ast.Field)
		if !ok {
			return true
		}

		// Check if this field has an old-style alias in the label
		if alias, ok := n.Label.(*ast.Alias); ok {
			hasChanges = true

			// Convert old-style alias (X=label) to new postfix alias (label~X)
			// The alias.Expr should be a Label (e.g., Ident)
			n.Alias = &ast.PostfixAlias{
				Field: alias.Ident,
			}
			ast.SetRelPos(alias.Ident, token.NoSpace)

			if label, ok := alias.Expr.(ast.Label); ok {
				// Skip if the label is not a valid Label type
				n.Label = label
				ast.SetRelPos(label, token.NoRelPos)
			}
		}

		if list, ok := n.Label.(*ast.ListLit); ok && len(list.Elts) == 1 {
			if alias, ok := list.Elts[0].(*ast.Alias); ok {
				hasChanges = true
				if n.Alias == nil {
					n.Alias = &ast.PostfixAlias{
						Field: ast.NewIdent("_"),
					}
				}
				n.Alias.Label = alias.Ident
				list.Elts[0] = alias.Expr
				ast.SetRelPos(alias.Ident, token.NoSpace)
				ast.SetRelPos(alias.Expr, token.NoRelPos)
			}
		}

		// Check if this field has an old-style value alias in the value
		// e.g., foo: X={x: X.a} should become foo: {let X = self; x: X.a}
		if alias, ok := n.Value.(*ast.Alias); ok {
			hasChanges = true

			// The value alias binds the alias identifier to self
			// Convert: foo: X={x: X.a, y: 1}
			// To: foo: {let X = self; x: X.a; y: 1}

			// The alias.Expr should be a StructLit
			s, ok := alias.Expr.(*ast.StructLit)
			if !ok {
				// This is possible V=[{a?: V}]. Replace with
				// {let V = self, [{a?: V}]} in that case.
				s = &ast.StructLit{Elts: []ast.Decl{alias.Expr}}
			}

			// Create a let clause: let X = self
			letClause := &ast.LetClause{
				Ident: alias.Ident,
				Expr:  ast.NewIdent("self"),
			}

			// Insert the let clause at the beginning of the struct
			s.Elts = slices.Insert(s.Elts, 0, ast.Decl(letClause))

			n.Value = s

			// Relink referring nodes to prevent rewriting by Sanitize.
			astutil.Apply(s, func(c astutil.Cursor) bool {
				if id, ok := c.Node().(*ast.Ident); ok && id.Node == alias {
					id.Node = letClause
				}
				return true
			}, nil)
		}
		return true
	}, nil).(*ast.File)

	return result, hasChanges
}
