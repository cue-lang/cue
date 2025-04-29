package genfunc

import (
	_ "embed"
	"reflect"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
)

// simplify applies some simplifications to the given expression to reduce the range
// of syntax we need to handle.
// TODO this should really be something that the core CUE evaluator can do.
func simplify(e ast.Expr) (_r ast.Expr) {
	// Continue simplifying until nothing more happens.
	// TODO this could probably be done without looping.
	for {
		old := e
		e = simplify0(e)
		if reflect.DeepEqual(e, old) {
			return e
		}
	}
}

func simplify0(e ast.Expr) (_r ast.Expr) {
	var structLit *ast.StructLit
	var embed *ast.EmbedDecl
	var binExpr *ast.BinaryExpr
	var unaryExpr *ast.UnaryExpr
	switch {
	case match(e, &structLit) && len(structLit.Elts) == 1 && match(structLit.Elts[0], &embed):
		// { x } -> x
		return simplify0(embed.Expr)
	case match(e, &binExpr) && binExpr.Op == token.AND &&
		isLiteral(binExpr.X) && isLiteral(binExpr.Y):
		if isBottom(binExpr.X) || isBottom(binExpr.Y) {
			// _|_ & lit => _|_
			return &ast.BottomLit{}
		}
		if !equal(binExpr.X, binExpr.Y) {
			// true & false => _|_
			return &ast.BottomLit{}
		}
		// lit & lit => lit
		return binExpr.X
	case match(e, &binExpr) && binExpr.Op == token.AND:
		x, y := simplify0(binExpr.X), simplify0(binExpr.Y)
		if equal(x, y) {
			// x & x => x
			return x
		}
		unifyDisjunct := func(x, y ast.Expr) ast.Expr {
			hasDefault := false
			if match(x, &unaryExpr) && unaryExpr.Op == token.MUL {
				hasDefault = true
				x = unaryExpr.X
			}
			if match(y, &unaryExpr) && unaryExpr.Op == token.MUL {
				hasDefault = true
				y = unaryExpr.X
			}
			e := ast.Expr(&ast.BinaryExpr{Op: token.AND, X: x, Y: y})
			if hasDefault {
				e = &ast.UnaryExpr{Op: token.MUL, X: e}
			}
			return e
		}

		switch {
		case match(y, &binExpr) && binExpr.Op == token.OR:
			// x & (a | b) => (x & a) | (x & b)
			a, b := binExpr.X, binExpr.Y
			return &ast.BinaryExpr{
				Op: token.OR,
				X:  simplify0(unifyDisjunct(x, a)),
				Y:  simplify0(unifyDisjunct(x, b)),
			}
		case match(x, &binExpr) && binExpr.Op == token.OR:
			// (a | b) & y => (a & y) | (b & y)
			a, b := binExpr.X, binExpr.Y
			return &ast.BinaryExpr{Op: token.OR,
				X: simplify0(unifyDisjunct(a, y)),
				Y: simplify0(unifyDisjunct(b, y)),
			}
		case isLiteral(x) && simpleType(y) == literalType(x):
			// "foo" & string => "foo"
			return x
		case isLiteral(y) && simpleType(x) == literalType(y):
			// string & "foo" => "foo"
			return y
		}
		return &ast.BinaryExpr{Op: token.AND, X: x, Y: y}
	case match(e, &unaryExpr):
		return &ast.UnaryExpr{Op: unaryExpr.Op, X: simplify0(unaryExpr.X)}
	case match(e, &binExpr) && binExpr.Op == token.OR:
		switch {
		case isBottom(binExpr.X):
			// _|_ | x => x
			return withoutDefaultMarker(binExpr.Y)
		case isBottom(binExpr.Y):
			// x | _|_ => x
			return withoutDefaultMarker(binExpr.X)
		}
		return &ast.BinaryExpr{
			Op: token.OR,
			X:  simplify0(binExpr.X),
			Y:  simplify0(binExpr.Y),
		}
	}
	return e
}

func withoutDefaultMarker(e ast.Expr) ast.Expr {
	var unaryExpr *ast.UnaryExpr
	if match(e, &unaryExpr) && unaryExpr.Op == token.MUL {
		return unaryExpr.X
	}
	return e
}

func isBottom(e ast.Expr) bool {
	_, ok := withoutDefaultMarker(e).(*ast.BottomLit)
	return ok
}

func simpleType(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.Ident:
		switch x.Name {
		case "string", "int", "number", "bool", "null":
			return x.Name
		}
	}
	return ""
}

func literalType(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.Ident:
		switch x.Name {
		case "true", "false":
			return "bool"
		case "null":
			return "null"
		}
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT,
			token.FLOAT:
			return "number"
		case token.STRING:
			return "string"
		}
	case *ast.BottomLit:
		return "_|_"
	}
	return ""
}

func isLiteral(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident:
		return x.Name == "true" || x.Name == "false" || x.Name == "null"
	case *ast.BasicLit:
		return true
	case *ast.BottomLit:
		return true
	}
	return false
}

func match[T any](x any, xp *T) (ok bool) {
	*xp, ok = x.(T)
	return ok
}

func dump(n ast.Node) string {
	if n == nil {
		return "<nil ast.Node>"
	}
	data, err := format.Node(n)
	if err != nil {
		panic(err)
	}
	return string(data)
}
