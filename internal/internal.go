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

// Package internal exposes some cue internals to other packages.
//
// A better name for this package would be technicaldebt.
package internal

// TODO: refactor packages as to make this package unnecessary.

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// A Decimal is an arbitrary-precision binary-coded decimal number.
//
// Right now Decimal is aliased to apd.Decimal. This may change in the future.
type Decimal = apd.Decimal

// Context wraps apd.Context for CUE's custom logic.
//
// Note that it avoids pointers to make it easier to make copies.
type Context struct {
	apd.Context
}

// WithPrecision mirrors upstream, but returning our type without a pointer.
func (c Context) WithPrecision(p uint32) Context {
	c.Context = *c.Context.WithPrecision(p)
	return c
}

var bigIntTen = apd.NewBigInt(10)

// reduceToIdeal trims the trailing zeros of an exact result d, but stops at
// the ideal exponent rather than removing all of them.
//
// apd pads the result of a division to the full precision of the context,
// which says nothing about the operation: dividing 6.00 by 3 yields
// 2.000000000000000000000000000000000. The General Decimal Arithmetic
// specification instead defines an ideal exponent per operation, and reduces
// an exact result towards it, which is what gives 2.00 here. apd implements
// that rule for addition, subtraction and multiplication, but not for
// division.
//
// The specification reduces an exact result only, so an inexact one keeps
// every digit of the precision, and stops reducing once the coefficient fits
// the precision, so that an exact quotient never exceeds it either.
func (c Context) reduceToIdeal(d *apd.Decimal, res apd.Condition, ideal int32) {
	if d.Form != apd.Finite || res.Inexact() {
		return
	}
	// Reduce clears the sign of a zero, which the specification keeps.
	neg := d.Negative
	d.Reduce(d)
	d.Negative = neg
	// Reduce removes every trailing zero, which may take the exponent past the
	// ideal one; put back the zeros the operation implies, as far as the
	// precision allows.
	n := d.Exponent - ideal
	if c.Precision != 0 {
		n = min(n, int32(c.Precision)-int32(d.NumDigits()))
	}
	if n <= 0 {
		return
	}
	var scale apd.BigInt
	scale.Exp(bigIntTen, apd.NewBigInt(int64(n)), nil)
	d.Coeff.Mul(&d.Coeff, &scale)
	d.Exponent -= n
}

func (c Context) Quo(d, x, y *apd.Decimal) (apd.Condition, error) {
	// The ideal exponent of a quotient is the dividend's less the divisor's.
	// Read it before the call, as d may alias x or y.
	ideal := x.Exponent - y.Exponent
	res, err := c.Context.Quo(d, x, y)
	if err != nil {
		// A failed division leaves d holding no result of its own.
		return res, err
	}
	c.reduceToIdeal(d, res, ideal)
	return res, nil
}

// BaseContext is used as CUE's default context for arbitrary-precision decimals.
var BaseContext = Context{*apd.BaseContext.WithPrecision(34)}

// ExactContext is like [BaseContext] but never rounds, as apd treats a
// precision of zero as unlimited.
//
// Integer arithmetic uses it so that results keep arbitrary precision, which
// doc/ref/spec.md requires: rounding an integer would silently drop its
// low-order digits, and an implementation must report an error rather than
// round when it cannot represent an integer precisely.
var ExactContext = Context{apd.BaseContext}

// EvaluatorVersion is declared here so it can be used everywhere without import cycles,
// but the canonical documentation lives at [cuelang.org/go/cue/cuecontext.EvalVersion].
//
// TODO(mvdan): rename to EvalVersion for consistency with cuecontext.
type EvaluatorVersion int

const (
	// EvalVersionUnset is the zero value, which signals that no evaluator version is provided.
	EvalVersionUnset EvaluatorVersion = 0

	// DefaultVersion is a special value as it selects a version depending on the current
	// value of CUE_EXPERIMENT. It exists separately to [EvalVersionUnset], even though both
	// implement the same version selection logic, so that we can distinguish between
	// a user explicitly asking for the default version versus an entirely unset version.
	DefaultVersion EvaluatorVersion = -1 // TODO(mvdan): rename to EvalDefault for consistency with cuecontext

	// The values below are documented under [cuelang.org/go/cue/cuecontext.EvalVersion].
	// We should never change or delete the values below, as they describe all known past versions
	// which is useful for understanding old debug output.

	EvalV2 EvaluatorVersion = 2
	EvalV3 EvaluatorVersion = 3

	// The current default, stable, and experimental versions.

	StableVersion = EvalV3 // TODO(mvdan): rename to EvalStable for consistency with cuecontext
	DevVersion    = EvalV3 // TODO(mvdan): rename to EvalExperiment for consistency with cuecontext
)

// Package finds the package declaration from the preamble of a file,
// returning it, and its index within the file's Decls.
func Package(f *ast.File) (*ast.Package, int) {
	for i, d := range f.Decls {
		switch d := d.(type) {
		case *ast.CommentGroup:
		case *ast.Attribute:
		case *ast.Package:
			if d.Name == nil { // malformed package declaration
				return nil, -1
			}
			return d, i
		default:
			return nil, -1
		}
	}
	return nil, -1
}

// NewComment creates a new CommentGroup from the given text.
// Each line is prefixed with "//" and the last newline is removed.
// Useful for ASTs generated by code other than the CUE parser.
func NewComment(isDoc bool, s string) *ast.CommentGroup {
	if s == "" {
		return nil
	}
	cg := &ast.CommentGroup{Doc: isDoc}
	if !isDoc {
		cg.Line = true
		cg.Position = 10
	}
	addLine := func(text string) {
		cg.List = append(cg.List, &ast.Comment{Text: text})
	}
	// We only wrap a source line whose comment form would exceed wrapTrigger,
	// and then to wrapWidth; this leaves already sensibly wrapped text alone.
	const wrapWidth = 80
	const wrapTrigger = 100
	for line := range strings.Lines(s) {
		words := strings.Fields(line)
		joined := strings.Join(words, " ")
		switch {
		case len(words) == 0:
			addLine("//")
		case utf8.RuneCountInString(joined)+len("// ") <= wrapTrigger:
			addLine("// " + joined)
		default:
			// Too long; greedily wrap the words to wrapWidth.
			count := len("//")
			buf := strings.Builder{}
			buf.WriteString("//")
			for _, w := range words {
				n := utf8.RuneCountInString(w) + 1
				if count+n > wrapWidth && count > len("// ") {
					addLine(buf.String())
					buf.Reset()
					buf.WriteString("//")
					count = len("// ")
				}
				buf.WriteString(" ")
				buf.WriteString(w)
				count += n
			}
			addLine(buf.String())
		}
	}
	if last := len(cg.List) - 1; cg.List[last].Text == "//" {
		cg.List = cg.List[:last]
	}
	return cg
}

func FileComments(f *ast.File) (docs, rest []*ast.CommentGroup) {
	hasPkg := false
	if pkg, _ := Package(f); pkg != nil {
		hasPkg = true
		docs = ast.Comments(pkg)
	}

	for _, c := range ast.Comments(f) {
		if c.Doc {
			docs = append(docs, c)
		} else {
			rest = append(rest, c)
		}
	}

	if !hasPkg && len(docs) == 0 && len(rest) > 0 {
		// use the first file comment group as as doc comment.
		docs, rest = rest[:1], rest[1:]
		docs[0].Doc = true
	}

	return
}

// ToExpr converts a node to an expression. If it is a file, it will return
// it as a struct. If is an expression, it will return it as is. Otherwise
// it panics.
func ToExpr(n ast.Node) ast.Expr {
	switch x := n.(type) {
	case nil:
		return nil

	case ast.Expr:
		return x

	case *ast.File:
		decls := x.Decls[len(x.Preamble()):]
		if len(decls) == 1 {
			if e, ok := decls[0].(*ast.EmbedDecl); ok {
				return e.Expr
			}
		}
		return &ast.StructLit{Elts: decls}

	default:
		panic(fmt.Sprintf("Unsupported node type %T", x))
	}
}

// ToFile converts an expression to a file.
//
// Adjusts the spacing of x when needed.
//
// If preserveStructLit is true and n is a [*ast.StructLit], then n
// will be embedded within the returned [*ast.File] rather than only
// its elements being included in the returned File. This ensures that
// position information of the StructLit's braces is not lost.
func ToFile(n ast.Node, preserveStructLit bool) *ast.File {
	if n == nil {
		return nil
	}
	// TODO(mvdan): SetRelPos modifies the input argument; if it's really needed, make a copy
	switch n := n.(type) {
	case *ast.StructLit:
		if preserveStructLit {
			ast.SetRelPos(n, token.NoSpace)
			return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: n}}}

		} else {
			f := &ast.File{Decls: n.Elts}
			// Ensure that the comments attached to the struct literal are not lost.
			ast.SetComments(f, ast.Comments(n))
			return f
		}
	case ast.Expr:
		ast.SetRelPos(n, token.NoSpace)
		return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: n}}}
	case *ast.File:
		return n
	default:
		panic(fmt.Sprintf("Unsupported node type %T", n))
	}
}

func IsDef(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "_#")
}

func IsHidden(s string) bool {
	return strings.HasPrefix(s, "_")
}

func IsDefOrHidden(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "_")
}

func IsDefinition(label ast.Label) bool {
	switch x := label.(type) {
	case *ast.Alias:
		if ident, ok := x.Expr.(*ast.Ident); ok {
			return IsDef(ident.Name)
		}
	case *ast.Ident:
		return IsDef(x.Name)
	}
	return false
}

func IsRegularField(f *ast.Field) bool {
	var ident *ast.Ident
	switch x := f.Label.(type) {
	case *ast.Alias:
		ident, _ = x.Expr.(*ast.Ident)
	case *ast.Ident:
		ident = x
	}
	if ident == nil {
		return true
	}
	if strings.HasPrefix(ident.Name, "#") || strings.HasPrefix(ident.Name, "_") {
		return false
	}
	return true
}

// BindSelfAlias binds ident to value through a let clause naming the
// predeclared "self", as the postfix alias syntax has no value form. It
// returns the struct literal holding the let clause, and reports whether
// value had to be wrapped in one, which is the case for anything but a
// struct literal itself.
//
// The "self" identifier is marked as predeclared so that
// [cuelang.org/go/cue/ast/astutil.Sanitize] renames it to "__self" if the
// name is shadowed in scope.
func BindSelfAlias(ident *ast.Ident, value ast.Expr) (s *ast.StructLit, let *ast.LetClause, wrapped bool) {
	s, ok := value.(*ast.StructLit)
	if !ok {
		s, wrapped = &ast.StructLit{Elts: []ast.Decl{value}}, true
	}
	let = &ast.LetClause{Ident: ident, Expr: ast.NewPredeclared("self")}
	s.Elts = slices.Insert(s.Elts, 0, ast.Decl(let))
	return s, let, wrapped
}

// GenPath reports the directory in which to store generated files.
func GenPath(root string) string {
	return filepath.Join(root, "cue.mod", "gen")
}
