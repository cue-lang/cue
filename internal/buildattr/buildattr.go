// Package buildattr implements support for interpreting the @if
// build attributes in CUE files.
package buildattr

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

// ShouldBuildFile reports whether a File should be included based on its
// attributes. It uses tagIsSet to determine whether a given attribute
// key should be treated as set.
//
// It also returns the build attribute if one was found.
func ShouldBuildFile(f *ast.File, tagIsSet func(key string) bool) (bool, *ast.Attribute, errors.Error) {
	a, err := getBuildAttr(f)
	if err != nil {
		return false, nil, err
	}
	if a == nil {
		return true, nil, nil
	}

	_, body := a.Split()

	expr, parseErr := parser.ParseExpr("", body)
	if parseErr != nil {
		return false, a, errors.Promote(parseErr, "")
	}

	include, err := shouldInclude(expr, tagIsSet)
	if err != nil {
		return false, a, err
	}
	return include, a, nil
}

func getBuildAttr(f *ast.File) (*ast.Attribute, errors.Error) {
	var a *ast.Attribute
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.Attribute:
			key, _ := x.Split()
			if key != "if" {
				continue
			}
			if a != nil {
				err := errors.Newf(d.Pos(), "multiple @if attributes")
				err = errors.Append(err,
					errors.Newf(a.Pos(), "previous declaration here"))
				return nil, err
			}
			a = x

		case *ast.Package:
			return a, nil
		}
	}
	return a, nil
}

func shouldInclude(expr ast.Expr, tagIsSet func(key string) bool) (bool, errors.Error) {
	switch x := expr.(type) {
	case *ast.Ident:
		return tagIsSet(x.Name), nil

	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND, token.LOR:
			a, err := shouldInclude(x.X, tagIsSet)
			if err != nil {
				return false, err
			}
			b, err := shouldInclude(x.Y, tagIsSet)
			if err != nil {
				return false, err
			}
			if x.Op == token.LAND {
				return a && b, nil
			}
			return a || b, nil

		default:
			return false, errors.Newf(token.NoPos, "invalid operator %v", x.Op)
		}

	case *ast.UnaryExpr:
		if x.Op != token.NOT {
			return false, errors.Newf(token.NoPos, "invalid operator %v", x.Op)
		}
		v, err := shouldInclude(x.X, tagIsSet)
		if err != nil {
			return false, err
		}
		return !v, nil

	default:
		return false, errors.Newf(token.NoPos, "invalid type %T in build attribute", expr)
	}
}
