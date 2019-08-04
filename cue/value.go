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

package cue

import (
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd/v2"
)

type value interface {
	source

	rewrite(*context, rewriteFunc) value

	// evalPartial evaluates a value without choosing default values.
	evalPartial(*context) evaluated

	kind() kind

	// subsumesImpl is only defined for non-reference types.
	// It should only be called by the subsumes function.
	subsumesImpl(*context, value, subsumeMode) bool
}

type evaluated interface {
	value
	binOp(*context, source, op, evaluated) evaluated
	strValue() string
}

type scope interface {
	value
	lookup(*context, label) arc
}

type atter interface {
	// at returns the evaluated and its original value at the given position.
	// If the original could not be found, it returns an error and nil.
	at(*context, int) evaluated
}

type iterAtter interface {
	// at returns the evaluated and its original value at the given position.
	// If the original could not be found, it returns an error and nil.
	iterAt(*context, int) arc
}

// caller must be implemented by any concrete lambdaKind
type caller interface {
	call(ctx *context, src source, args ...evaluated) value
	returnKind() kind
}

func checkKind(ctx *context, x value, want kind) *bottom {
	if b, ok := x.(*bottom); ok {
		return b
	}
	got := x.kind()
	if got&want&concreteKind == bottomKind && want != bottomKind {
		return ctx.mkErr(x, "cannot use value %v (type %s) as %s", x, got, want)
	}
	if !got.isGround() {
		return ctx.mkErr(x, codeIncomplete,
			"non-concrete value %v", got)
	}
	return nil
}

func newDecl(n ast.Decl) baseValue {
	if n == nil {
		panic("empty node")
	}
	return baseValue{n}
}

func newExpr(n ast.Expr) baseValue {
	if n == nil {
		panic("empty node")
	}
	return baseValue{n}
}

func newNode(n ast.Node) baseValue {
	if n == nil {
		panic("empty node")
	}
	return baseValue{n}
}

type source interface {
	// syntax returns the parsed file of the underlying node or a computed
	// node indicating that it is a computed binary expression.
	syntax() ast.Node
	computed() *computedSource
	Pos() token.Pos
	base() baseValue
}

type computedSource struct {
	pos token.Pos
	op  op
	x   value
	y   value
}

func (s *computedSource) Pos() token.Pos {
	return s.pos
}

type posser interface {
	Pos() token.Pos
}

type baseValue struct {
	pos posser
}

func (b baseValue) Pos() token.Pos {
	if b.pos == nil {
		return token.NoPos
	}
	return b.pos.Pos()
}

func (b baseValue) computed() *computedSource {
	switch x := b.pos.(type) {
	case *computedSource:
		return x
	}
	return nil
}

func (b baseValue) syntax() ast.Node {
	switch x := b.pos.(type) {
	case ast.Node:
		return x
	}
	return nil
}

func (b baseValue) base() baseValue {
	return b
}

func (b baseValue) strValue() string { panic("unimplemented") }
func (b baseValue) returnKind() kind { panic("unimplemented") }

// top is the top of the value lattice. It subsumes all possible values.
type top struct{ baseValue }

func (x *top) kind() kind { return topKind }

// basicType represents the root class of any specific type.
type basicType struct {
	baseValue
	k kind
}

func (x *basicType) kind() kind { return x.k | nonGround }

// Literals

type nullLit struct{ baseValue }

func (x *nullLit) kind() kind { return nullKind }

type boolLit struct {
	baseValue
	b bool
}

func (x *boolLit) kind() kind { return boolKind }

func boolTonode(src source, b bool) evaluated {
	return &boolLit{src.base(), b}
}

type bytesLit struct {
	baseValue
	b  []byte
	re *regexp.Regexp // only set if needed
}

func (x *bytesLit) kind() kind       { return bytesKind }
func (x *bytesLit) strValue() string { return string(x.b) }

func (x *bytesLit) iterAt(ctx *context, i int) arc {
	if i >= len(x.b) {
		return arc{}
	}
	v := x.at(ctx, i)
	return arc{v: v, cache: v}
}

func (x *bytesLit) at(ctx *context, i int) evaluated {
	if i < 0 || i >= len(x.b) {
		return ctx.mkErr(x, "index %d out of bounds", i)
	}
	// TODO: this is incorrect.
	n := newNum(x, intKind)
	n.v.SetInt64(int64(x.b[i]))
	return n
}

func (x *bytesLit) len() int { return len(x.b) }

func (x *bytesLit) slice(ctx *context, lo, hi *numLit) evaluated {
	lox := 0
	hix := len(x.b)
	if lo != nil {
		lox = lo.intValue(ctx)
	}
	if hi != nil {
		hix = hi.intValue(ctx)
	}
	if lox < 0 {
		return ctx.mkErr(x, "invalid slice index %d (must be non-negative)", lox)
	}
	if hix < 0 {
		return ctx.mkErr(x, "invalid slice index %d (must be non-negative)", hix)
	}
	if hix < lox {
		return ctx.mkErr(x, "invalid slice index: %d > %d", lox, hix)
	}
	if len(x.b) < hix {
		return ctx.mkErr(hi, "slice bounds out of range")
	}
	return &bytesLit{x.baseValue, x.b[lox:hix], nil}
}

type stringLit struct {
	baseValue
	str string
	re  *regexp.Regexp // only set if needed

	// TODO: maintain extended grapheme index cache.
}

func (x *stringLit) kind() kind       { return stringKind }
func (x *stringLit) strValue() string { return x.str }

func (x *stringLit) iterAt(ctx *context, i int) arc {
	runes := []rune(x.str)
	if i >= len(runes) {
		return arc{}
	}
	v := x.at(ctx, i)
	return arc{v: v, cache: v}
}

func (x *stringLit) at(ctx *context, i int) evaluated {
	runes := []rune(x.str)
	if i < 0 || i >= len(runes) {
		return ctx.mkErr(x, "index %d out of bounds", i)
	}
	// TODO: this is incorrect.
	return &stringLit{x.baseValue, string(runes[i : i+1]), nil}
}
func (x *stringLit) len() int { return len([]rune(x.str)) }

func (x *stringLit) slice(ctx *context, lo, hi *numLit) evaluated {
	runes := []rune(x.str)
	lox := 0
	hix := len(runes)
	if lo != nil {
		lox = lo.intValue(ctx)
	}
	if hi != nil {
		hix = hi.intValue(ctx)
	}
	if lox < 0 {
		return ctx.mkErr(x, "invalid slice index %d (must be non-negative)", lox)
	}
	if hix < 0 {
		return ctx.mkErr(x, "invalid slice index %d (must be non-negative)", hix)
	}
	if hix < lox {
		return ctx.mkErr(x, "invalid slice index: %d > %d", lox, hix)
	}
	if len(runes) < hix {
		return ctx.mkErr(hi, "slice bounds out of range")
	}
	return &stringLit{x.baseValue, string(runes[lox:hix]), nil}
}

type numBase struct {
	baseValue
	numInfo
}

func newNumBase(n ast.Expr, info numInfo) numBase {
	return numBase{newExpr(n), info}
}

func newNumBin(k kind, a, b *numLit) *numLit {
	n := &numLit{
		numBase: numBase{
			baseValue: a.baseValue,
			numInfo:   unifyNuminfo(a.numInfo, b.numInfo),
		},
	}
	return n
}

type numLit struct {
	numBase
	v apd.Decimal
}

func parseInt(k kind, s string) *numLit {
	n := &ast.BasicLit{
		Kind:  token.INT,
		Value: s,
	}
	num := newNum(newExpr(n), k)
	_, _, err := num.v.SetString(s)
	if err != nil {
		panic(err)
	}
	return num
}

func parseFloat(s string) *numLit {
	n := &ast.BasicLit{
		Kind:  token.FLOAT,
		Value: s,
	}
	num := newNum(newExpr(n), floatKind)
	_, _, err := num.v.SetString(s)
	if err != nil {
		panic(err)
	}
	return num
}

func newNum(src source, k kind) *numLit {
	n := &numLit{numBase: numBase{baseValue: src.base()}}
	n.k = k
	return n
}

var ten = big.NewInt(10)

var one = parseInt(intKind, "1")

func (x *numLit) kind() kind       { return x.k }
func (x *numLit) strValue() string { return x.v.String() }

func (x *numLit) isInt(ctx *context) bool {
	return x.kind()&intKind != 0
}

func (x *numLit) intValue(ctx *context) int {
	v, err := x.v.Int64()
	if err != nil {
		ctx.mkErr(x, "intValue: %v", err)
		return 0
	}
	return int(v)
}

type durationLit struct {
	baseValue
	d time.Duration
}

func (x *durationLit) kind() kind       { return durationKind }
func (x *durationLit) strValue() string { return x.d.String() }

type bound struct {
	baseValue
	op    op   // opNeq, opLss, opLeq, opGeq, or opGtr
	k     kind // mostly used for number kind
	value value
}

func newBound(ctx *context, base baseValue, op op, k kind, v value) evaluated {
	kv := v.kind()
	if kv.isAnyOf(numKind) {
		kv |= numKind
	} else if op == opNeq && kv&atomKind == nullKind {
		kv = typeKinds &^ nullKind
	}
	if op == opMat || op == opNMat {
		v = compileRegexp(ctx, v)
		if isBottom(v) {
			return v.(*bottom)
		}
	}
	return &bound{base, op, unifyType(k&topKind, kv) | nonGround, v}
}

func (x *bound) kind() kind {
	return x.k
}

func mkIntRange(a, b string) evaluated {
	from := newBound(nil, baseValue{}, opGeq, intKind, parseInt(intKind, a))
	to := newBound(nil, baseValue{}, opLeq, intKind, parseInt(intKind, b))
	e := &unification{
		binSrc(token.NoPos, opUnify, from, to),
		[]evaluated{from, to},
	}
	// TODO: make this an integer
	// int := &basicType{k: intKind}
	// e = &unification{
	// 	binSrc(token.NoPos, opUnify, int, e),
	// 	[]evaluated{int, e},
	// }
	return e
}

func mkFloatRange(a, b string) evaluated {
	from := newBound(nil, baseValue{}, opGeq, numKind, parseFloat(a))
	to := newBound(nil, baseValue{}, opLeq, numKind, parseFloat(b))
	e := &unification{
		binSrc(token.NoPos, opUnify, from, to),
		[]evaluated{from, to},
	}
	// TODO: make this an integer
	// int := &basicType{k: intKind}
	// e = &unification{
	// 	binSrc(token.NoPos, opUnify, int, e),
	// 	[]evaluated{int, e},
	// }
	return e
}

var predefinedRanges = map[string]evaluated{
	"rune":  mkIntRange("0", strconv.Itoa(0x10FFFF)),
	"int8":  mkIntRange("-128", "127"),
	"int16": mkIntRange("-32768", "32767"),
	"int32": mkIntRange("-2147483648", "2147483647"),
	"int64": mkIntRange("-9223372036854775808", "9223372036854775807"),
	"int128": mkIntRange(
		"-170141183460469231731687303715884105728",
		"170141183460469231731687303715884105727"),

	// Do not include an alias for "byte", as it would be too easily confused
	// with the builtin "bytes".
	"uint":    newBound(nil, baseValue{}, opGeq, intKind, parseInt(intKind, "0")),
	"uint8":   mkIntRange("0", "255"),
	"uint16":  mkIntRange("0", "65535"),
	"uint32":  mkIntRange("0", "4294967295"),
	"uint64":  mkIntRange("0", "18446744073709551615"),
	"uint128": mkIntRange("0", "340282366920938463463374607431768211455"),

	// 2**127 * (2**24 - 1) / 2**23
	"float32": mkFloatRange(
		"-3.40282346638528859811704183484516925440e+38",
		"+3.40282346638528859811704183484516925440e+38",
	),
	// 2**1023 * (2**53 - 1) / 2**52
	"float64": mkFloatRange(
		"-1.797693134862315708145274237317043567981e+308",
		"+1.797693134862315708145274237317043567981e+308",
	),
}

type interpolation struct {
	baseValue
	k     kind    // string or bytes
	parts []value // odd: strings, even expressions
}

func (x *interpolation) kind() kind { return x.k | nonGround }

type list struct {
	baseValue
	elem *structLit

	typ value

	// TODO: consider removing len. Currently can only be len(a) or >= len(a)
	// and could be replaced with a bool.
	len value
}

// initLit initializes a literal list.
func (x *list) initLit() {
	n := newNum(x, intKind)
	n.v.SetInt64(int64(len(x.elem.arcs)))
	x.len = n
	x.typ = &top{x.baseValue}
}

func (x *list) manifest(ctx *context) evaluated {
	if x.kind().isGround() {
		return x
	}
	// A list is ground if its length is ground, or if the current length
	// meets matches the cap.
	n := newNum(x, intKind)
	n.v.SetInt64(int64(len(x.elem.arcs)))
	if n := binOp(ctx, x, opUnify, n, x.len.evalPartial(ctx)); !isBottom(n) {
		return &list{
			baseValue: x.baseValue,
			elem:      x.elem,
			len:       n,
			typ:       &top{x.baseValue},
		}
	}
	return x
}

func (x *list) kind() kind {
	k := listKind
	if _, ok := x.len.(*numLit); ok {
		return k
	}
	return k | nonGround
}

// at returns the evaluated and original value of position i. List x must
// already have been evaluated. It returns an error and nil if there was an
// issue evaluating the list itself.
func (x *list) at(ctx *context, i int) evaluated {
	arc := x.iterAt(ctx, i)
	if arc.cache == nil {
		return ctx.mkErr(x, "index %d out of bounds", i)
	}
	return arc.cache
}

// iterAt returns the evaluated and original value of position i. List x must
// already have been evaluated. It returns an error and nil if there was an
// issue evaluating the list itself.
func (x *list) iterAt(ctx *context, i int) arc {
	if i < 0 {
		v := ctx.mkErr(x, "index %d out of bounds", i)
		return arc{cache: v}
	}
	if i < len(x.elem.arcs) {
		a := x.elem.iterAt(ctx, i)
		a.feature = 0
		return a
	}
	max := maxNum(x.len.(evaluated))
	if max.kind().isGround() {
		if max.kind()&intKind == bottomKind {
			v := ctx.mkErr(max, "length indicator of list not of type int")
			return arc{cache: v}
		}
		n := max.(*numLit).intValue(ctx)
		if i >= n {
			return arc{}
		}
	}
	return arc{cache: x.typ.(evaluated), v: x.typ}
}

func (x *list) isOpen() bool {
	return !x.len.kind().isGround()
}

// lo and hi must be nil or a ground integer.
func (x *list) slice(ctx *context, lo, hi *numLit) evaluated {
	a := x.elem.arcs
	max := maxNum(x.len).evalPartial(ctx)
	if hi != nil {
		n := hi.intValue(ctx)
		if n < 0 {
			return ctx.mkErr(x, "negative slice index")
		}
		if max.kind().isGround() && !leq(ctx, hi, hi, max) {
			return ctx.mkErr(hi, "slice bounds out of range")
		}
		max = hi
		if n < len(a) {
			a = a[:n]
		}
	}

	if lo != nil {
		n := lo.intValue(ctx)
		if n < 0 {
			return ctx.mkErr(x, "negative slice index")
		}
		if n > 0 && max.kind().isGround() {
			if !leq(ctx, lo, lo, max) {
				max := max.(*numLit).intValue(ctx)
				return ctx.mkErr(x, "invalid slice index: %v > %v", n, max)
			}
			max = binOp(ctx, lo, opSub, max, lo)
		}
		if n < len(a) {
			a = a[n:]
		} else {
			a = []arc{}
		}
	}
	arcs := make([]arc, len(a))
	for i, a := range a {
		arcs[i] = arc{feature: label(i), v: a.v, docs: a.docs}
	}
	s := &structLit{baseValue: x.baseValue, arcs: arcs}
	return &list{baseValue: x.baseValue, elem: s, typ: x.typ, len: max}
}

// An structLit is a single structLit in the configuration tree.
//
// An structLit may have multiple arcs. There may be only one arc per label. Use
// insertRaw to insert arcs to ensure this invariant holds.
type structLit struct {
	baseValue

	// TODO(perf): separate out these infrequent values to save space.
	emit           value // currently only supported at top level.
	template       value
	comprehensions []*fieldComprehension

	// TODO: consider hoisting the template arc to its own value.
	arcs     []arc
	expanded *structLit
}

func newStruct(src source) *structLit {
	return &structLit{baseValue: src.base()}
}

func (x *structLit) kind() kind { return structKind }

type arcs []arc

func (x *structLit) Len() int           { return len(x.arcs) }
func (x *structLit) Less(i, j int) bool { return x.arcs[i].feature < x.arcs[j].feature }
func (x *structLit) Swap(i, j int)      { x.arcs[i], x.arcs[j] = x.arcs[j], x.arcs[i] }

// lookup returns the node for the given label f, if present, or nil otherwise.
func (x *structLit) lookup(ctx *context, f label) arc {
	x = x.expandFields(ctx)
	// Lookup is done by selector or index references. Either this is done on
	// literal nodes or nodes obtained from references. In the later case,
	// noderef will have ensured that the ancestors were evaluated.
	for i, a := range x.arcs {
		if a.feature == f {
			a.cache = x.at(ctx, i)
			return a
		}
	}
	return arc{}
}

func (x *structLit) iterAt(ctx *context, i int) arc {
	x = x.expandFields(ctx)
	if i >= len(x.arcs) {
		return arc{}
	}
	a := x.arcs[i]
	a.cache = x.at(ctx, i) // TODO: return template & v for original?
	return a
}

func (x *structLit) at(ctx *context, i int) evaluated {
	x = x.expandFields(ctx)
	// if x.emit != nil && isBottom(x.emit) {
	// 	return x.emit.(evaluated)
	// }
	// Lookup is done by selector or index references. Either this is done on
	// literal nodes or nodes obtained from references. In the later case,
	// noderef will have ensured that the ancestors were evaluated.
	if v := x.arcs[i].cache; v == nil {

		// cycle detection

		popped := ctx.evalStack
		ctx.evalStack = append(ctx.evalStack, bottom{
			baseValue: x.base(),
			index:     ctx.index,
			code:      codeCycle,
			value:     x.arcs[i].v,
			format:    "cycle detected",
		})
		x.arcs[i].cache = &(ctx.evalStack[len(ctx.evalStack)-1])

		v := x.arcs[i].v.evalPartial(ctx)
		ctx.evalStack = popped

		var doc *ast.Field
		v, doc = x.applyTemplate(ctx, i, v)

		if (len(ctx.evalStack) > 0 && ctx.cycleErr) || cycleError(v) != nil {
			// Don't cache while we're in a evaluation cycle as it will cache
			// partial results. Each field involved in the cycle will have to
			// reevaluated the values from scratch. As the result will be
			// cached after one cycle, it will evaluate the cycle at most twice.
			x.arcs[i].cache = nil
			return v
		}

		// If there as a cycle error, we have by now evaluated a full cycle and
		// it is safe to cache the result.
		ctx.cycleErr = false

		x.arcs[i].cache = v
		if doc != nil {
			x.arcs[i].docs = &docNode{n: doc, left: x.arcs[i].docs}
		}
		if len(ctx.evalStack) == 0 {
			if err := ctx.processDelayedConstraints(); err != nil {
				x.arcs[i].cache = err
			}
		}
	} else if b := cycleError(v); b != nil {
		copy := *b
		return &copy
	}
	return x.arcs[i].cache
}

func (x *structLit) expandFields(ctx *context) *structLit {
	if x.expanded != nil {
		return x.expanded
	}
	if x.comprehensions == nil {
		x.expanded = x
		return x
	}

	x.expanded = x

	comprehensions := x.comprehensions
	emit := x.emit
	template := x.template
	newArcs := []arc{}
	optional := false

	for _, c := range comprehensions {
		result := c.clauses.yield(ctx, func(k, v evaluated, opt bool) *bottom {
			optional = opt
			if !k.kind().isAnyOf(stringKind) {
				return ctx.mkErr(k, "key must be of type string")
			}
			if c.isTemplate {
				// TODO: disallow altogether or only when it refers to fields.
				if template == nil {
					template = v
				} else {
					template = mkBin(ctx, c.Pos(), opUnify, template, v)
				}
				return nil
			}
			// TODO(perf): improve big O
			f := ctx.label(k.strValue(), true)
			for i, a := range newArcs {
				if a.feature == f {
					newArcs[i].v = mkBin(ctx, x.Pos(), opUnify, a.v, v)
					return nil
				}
			}
			newArcs = append(newArcs, arc{feature: f, v: v})
			return nil
		})
		switch {
		case result == nil:
		case isBottom(result):
			emit = result
		default:
			panic("should not happen")
		}
	}

	// new arcs may be merged with old ones, but only if the old ones were not
	// referred to in the evaluation of any of the arcs.
	// TODO(perf): improve big O
	arcs := make([]arc, 0, len(x.arcs)+len(newArcs))
	arcs = append(arcs, x.arcs...)
	orig := x
	x = &structLit{x.baseValue, emit, template, nil, arcs, nil}
	x.expanded = x
	orig.expanded = x

outer:
	for _, na := range newArcs {
		f := na.feature
		for i, a := range x.arcs {
			if a.feature == f {
				if x.arcs[i].cache != nil {
					x.arcs[i].cache = ctx.mkErr(na.v, "field %s both generated by and referred to by field comprehension in same struct",
						ctx.labelStr(f))
				} else {
					x.arcs[i].v = mkBin(ctx, x.Pos(), opUnify, a.v, na.v)
					x.arcs[i].optional = x.arcs[i].optional && optional
					x.arcs[i].docs = mergeDocs(na.docs, a.docs)
				}
				continue outer
			}
		}
		x.arcs = append(x.arcs, arc{feature: f, optional: optional, v: na.v})
	}
	sort.Stable(x)
	return x
}

func (x *structLit) applyTemplate(ctx *context, i int, v evaluated) (evaluated, *ast.Field) {
	if x.template != nil {
		fn, err := evalLambda(ctx, x.template)
		if err != nil {
			return err, nil
		}
		name := ctx.labelStr(x.arcs[i].feature)
		arg := &stringLit{x.baseValue, name, nil}
		w := fn.call(ctx, x, arg).evalPartial(ctx)
		v = binOp(ctx, x, opUnify, v, w)

		f, _ := x.template.base().syntax().(*ast.Field)
		return v, f
	}
	return v, nil
}

// A label is a canonicalized feature name.
type label uint32

const hidden label = 0x01 // only set iff identifier starting with $

// An arc holds the label-value pair.
//
// A fully evaluated arc has either a node or a value. An unevaluated arc,
// however, may have both. In this case, the value must ultimately evaluate
// to a node, which will then be merged with the existing one.
type arc struct {
	feature  label
	optional bool
	// TODO: add index to preserve approximate order within a struct and use
	// topological sort to compute new struct order when unifying. This could
	// also be achieved by not sorting labels on features and doing
	// a linear search in fields.

	v     value
	cache evaluated // also used as newValue during unification.
	attrs *attributes
	docs  *docNode
}

type docNode struct {
	n     *ast.Field
	left  *docNode
	right *docNode
}

func (d *docNode) appendDocs(docs []*ast.CommentGroup) []*ast.CommentGroup {
	if d == nil {
		return docs
	}
	docs = d.left.appendDocs(docs)
	if d.n != nil {
		docs = appendDocComments(docs, d.n)
		docs = appendDocComments(docs, d.n.Label)
	}
	docs = d.right.appendDocs(docs)
	return docs
}

func appendDocComments(docs []*ast.CommentGroup, n ast.Node) []*ast.CommentGroup {
	for _, c := range n.Comments() {
		if c.Position == 0 {
			docs = append(docs, c)
		}
	}
	return docs
}

func mergeDocs(a, b *docNode) *docNode {
	if a == b || a == nil {
		return b
	}
	if b == nil {
		return a
	}
	// TODO: filter out duplicates?
	return &docNode{nil, a, b}
}

func (a *arc) val() evaluated {
	return a.cache
}

func (a *arc) setValue(v value) {
	a.v = v
	a.cache = nil
}

// insertValue is used during initialization but never during evaluation.
func (x *structLit) insertValue(ctx *context, f label, optional bool, value value, a *attributes, docs *docNode) {
	for i, p := range x.arcs {
		if f != p.feature {
			continue
		}
		x.arcs[i].v = mkBin(ctx, token.NoPos, opUnify, p.v, value)
		// TODO: should we warn if there is a mixed mode of optional and non
		// optional fields at this point?
		x.arcs[i].optional = x.arcs[i].optional && optional
		x.arcs[i].docs = mergeDocs(x.arcs[i].docs, docs)
		return
	}
	x.arcs = append(x.arcs, arc{f, optional, value, nil, a, docs})
	sort.Stable(x)
}

// A nodeRef is a reference to a node.
type nodeRef struct {
	baseValue
	node scope
}

func (x *nodeRef) kind() kind {
	// TODO(REWORK): no context available
	// n := x.node.deref(nil)
	n := x.node
	return n.kind() | nonGround | referenceKind
}

type selectorExpr struct {
	baseValue
	x       value
	feature label
}

// TODO: could this be narrowed down?
func (x *selectorExpr) kind() kind {
	isRef := x.x.kind() & referenceKind
	return topKind | isRef
}

type indexExpr struct {
	baseValue
	x     value
	index value
}

// TODO: narrow this down when we have list types.
func (x *indexExpr) kind() kind { return topKind | referenceKind }

type sliceExpr struct {
	baseValue
	x  value
	lo value
	hi value
}

// TODO: narrow this down when we have list types.
func (x *sliceExpr) kind() kind { return topKind | referenceKind }

type callExpr struct {
	baseValue
	x    value
	args []value
}

func (x *callExpr) kind() kind {
	// TODO: could this be narrowed down?
	switch c := x.x.(type) {
	case *lambdaExpr:
		return c.returnKind() | nonGround
	case *builtin:
		switch len(x.args) {
		case len(c.Params):
			return c.Result
		case len(c.Params) - 1:
			if len(c.Params) == 0 || c.Result&boolKind == 0 {
				return bottomKind
			}
			return c.Params[0]
		}
	}
	return topKind | referenceKind
}

type customValidator struct {
	baseValue

	args []evaluated // any but the first value
	call *builtin    // function must return a bool
}

func (x *customValidator) kind() kind {
	if len(x.call.Params) == 0 {
		return bottomKind
	}
	return x.call.Params[0] | nonGround
}

type params struct {
	arcs []arc
}

func (x *params) add(f label, v value) {
	if v == nil {
		panic("nil node")
	}
	x.arcs = append(x.arcs, arc{feature: f, v: v})
}

func (x *params) iterAt(ctx *context, i int) (evaluated, value) {
	if i >= len(x.arcs) {
		return nil, nil
	}
	return x.at(ctx, i), x.arcs[i].v
}

// lookup returns the node for the given label f, if present, or nil otherwise.
func (x *params) at(ctx *context, i int) evaluated {
	// Lookup is done by selector or index references. Either this is done on
	// literal nodes or nodes obtained from references. In the later case,
	// noderef will have ensured that the ancestors were evaluated.
	if x.arcs[i].cache == nil {
		x.arcs[i].cache = x.arcs[i].v.evalPartial(ctx)
	}
	return x.arcs[i].cache
}

// lookup returns the node for the given label f, if present, or nil otherwise.
func (x *params) lookup(ctx *context, f label) arc {
	if f == 0 && len(x.arcs) == 1 {
		// A template binding.
		a := x.arcs[0]
		a.cache = x.at(ctx, 0)
		return a
	}
	// Lookup is done by selector or index references. Either this is done on
	// literal nodes or nodes obtained from references. In the later case,
	// noderef will have ensured that the ancestors were evaluated.
	for i, a := range x.arcs {
		if a.feature == f {
			a.cache = x.at(ctx, i)
			return a
		}
	}
	return arc{}
}

type lambdaExpr struct {
	baseValue
	*params
	value value
}

// TODO: could this be narrowed down?
func (x *lambdaExpr) kind() kind       { return lambdaKind }
func (x *lambdaExpr) returnKind() kind { return x.value.kind() }

// call calls and evaluates a  lambda expression. It is assumed that x may be
// destroyed, either because it is copied as a result of a reference or because
// it is invoked as a literal.
func (x *lambdaExpr) call(ctx *context, p source, args ...evaluated) value {
	// fully evaluated.
	if len(x.params.arcs) != len(args) {
		return ctx.mkErr(p, x, "number of arguments does not match (%d vs %d)",
			len(x.params.arcs), len(args))
	}

	// force parameter substitution. It is important that the result stands on
	// its own and does not depend on its input parameters.
	arcs := make(arcs, len(x.arcs))
	for i, a := range x.arcs {
		v := binOp(ctx, p, opUnify, a.v.evalPartial(ctx), args[i])
		if isBottom(v) {
			return v
		}
		arcs[i] = arc{feature: a.feature, v: v, cache: v}
	}
	lambda := &lambdaExpr{x.baseValue, &params{arcs}, nil}
	defer ctx.pushForwards(x, lambda).popForwards()
	obj := ctx.copy(x.value)
	return obj
}

// Operations

type unaryExpr struct {
	baseValue
	op op
	x  value
}

func (x *unaryExpr) kind() kind { return x.x.kind() }

func compileRegexp(ctx *context, v value) value {
	var err error
	switch x := v.(type) {
	case *stringLit:
		if x.re == nil {
			x.re, err = regexp.Compile(x.str)
			if err != nil {
				return ctx.mkErr(v, "could not compile regular expression %q: %v", x.str, err)
			}
		}
	case *bytesLit:
		if x.re == nil {
			x.re, err = regexp.Compile(string(x.b))
			if err != nil {
				return ctx.mkErr(v, "could not compile regular expression %q: %v", x.b, err)
			}
		}
	}
	return v
}

type binaryExpr struct {
	baseValue
	op    op
	left  value
	right value
}

func mkBin(ctx *context, pos token.Pos, op op, left, right value) value {
	if left == nil || right == nil {
		panic("operands may not be nil")
	}
	if op == opUnify {
		if left == right {
			return left
		}
		if _, ok := left.(*top); ok {
			return right
		}
		if _, ok := right.(*top); ok {
			return left
		}
		// TODO(perf): consider adding a subsumption filter.
		// if subsumes(ctx, left, right) {
		// 	return right
		// }
		// if subsumes(ctx, right, left) {
		// 	return left
		// }
	}
	bin := &binaryExpr{binSrc(pos, op, left, right), op, left, right}
	return updateBin(ctx, bin)
}

func updateBin(ctx *context, bin *binaryExpr) value {
	switch bin.op {
	case opMat, opNMat:
		bin.right = compileRegexp(ctx, bin.right)
		if isBottom(bin.right) {
			return bin.right
		}
	}
	return bin
}

func (x *binaryExpr) kind() kind {
	// TODO: cache results
	kind, _, _ := matchBinOpKind(x.op, x.left.kind(), x.right.kind())
	return kind | nonGround
}

// unification collects evaluated values that are not mutually exclusive
// but cannot be represented as a single value. It allows doing the bookkeeping
// on accumulating conjunctions, simplifying them along the way, until they do
// resolve into a single value.
type unification struct {
	baseValue
	values []evaluated
}

func (x *unification) kind() kind {
	k := topKind
	for _, v := range x.values {
		k &= v.kind()
	}
	return k | nonGround
}

type disjunction struct {
	baseValue

	values []dValue

	hasDefaults bool // also true if it had elminated defaults.

	// bind is the node that a successful disjunction will bind to. This
	// allows other arcs to point to this node before the disjunction is
	// completed. For early failure, this node can be set to the glb of all
	// disjunctions. Otherwise top will suffice.
	// bind node
}

type dValue struct {
	val    value
	marked bool
}

func (x *disjunction) kind() kind {
	k := kind(0)
	for _, v := range x.values {
		k |= v.val.kind()
	}
	if k != bottomKind {
		k |= nonGround
	}
	return k
}

func (x *disjunction) Pos() token.Pos { return x.values[0].val.Pos() }

// add add a value to the disjunction. It is assumed not to be a disjunction.
func (x *disjunction) add(ctx *context, v value, marked bool) {
	x.values = append(x.values, dValue{v, marked})
}

// normalize removes redundant element from unification.
// x must already have been evaluated.
func (x *disjunction) normalize(ctx *context, src source) mVal {
	leq := func(ctx *context, lt, gt dValue) bool {
		if isBottom(lt.val) {
			return true
		}
		return (!lt.marked || gt.marked) && subsumes(ctx, gt.val, lt.val, 0)
	}
	k := 0

	// manifesting values should be disabled for recursive evaluation as
	// these values may still be bound to another value later on, for instance
	// when the result of this value is unified with another value.
	noManifest := ctx.noManifest
	ctx.noManifest = true
	hasMarked := false
	var markedErr *bottom
outer:
	for i, v := range x.values {
		// TODO: this is pre-evaluation is quite aggressive. Verify whether
		// this does not trigger structural cycles. If so, this can check for
		// bottom and the validation can be delayed to as late as picking
		// defaults. The drawback of this approach is that printed intermediate
		// results will not look great.
		if err := validate(ctx, v.val); err != nil {
			if v.marked {
				markedErr = err
			}
			continue
		}
		if v.marked {
			hasMarked = true
		}
		for j, w := range x.values {
			if i == j {
				continue
			}
			if leq(ctx, v, w) && (!leq(ctx, w, v) || j < i) {
				// strictly subsumed, or equal and and the equal element was
				// processed earlier.
				continue outer
			}
		}
		// If there was a three-way equality, an element w, where w == v could
		// already have been added.
		for j := 0; j < k; j++ {
			if leq(ctx, v, x.values[j]) {
				continue outer
			}
		}
		x.values[k] = v
		k++
	}
	if !hasMarked && markedErr != nil && (k > 1 || !x.values[0].val.kind().isGround()) {
		x.values[k] = dValue{&bottom{}, true}
		k++
	}
	ctx.noManifest = noManifest

	switch k {
	case 0:
		// Empty disjunction. All elements must be errors.
		// Take the first error as an example.
		err := x.values[0].val
		if !isBottom(err) {
			// TODO: use format instead of debugStr.
			err = ctx.mkErr(src, ctx.str(err))
		}
		return mVal{ctx.mkErr(src, "empty disjunction: %v", err), false}
	case 1:
		v := x.values[0]
		return mVal{v.val.(evaluated), v.marked}
	}
	x.values = x.values[:k]
	return mVal{x, false}
}

type listComprehension struct {
	baseValue
	clauses yielder
}

func (x *listComprehension) kind() kind {
	return listKind | nonGround | referenceKind
}

type fieldComprehension struct {
	baseValue
	clauses    yielder
	isTemplate bool
}

func (x *fieldComprehension) kind() kind {
	return topKind | nonGround
}

type yieldFunc func(k, v evaluated, optional bool) *bottom

type yielder interface {
	value
	yield(*context, yieldFunc) evaluated
}

type yield struct {
	baseValue
	opt   bool
	key   value
	value value
}

func (x *yield) kind() kind { return topKind | referenceKind }

func (x *yield) yield(ctx *context, fn yieldFunc) evaluated {
	var k evaluated
	if x.key != nil {
		k = ctx.manifest(x.key)
		if isBottom(k) {
			return k
		}
	} else {
		k = &top{}
	}
	v := x.value.evalPartial(ctx)
	if isBottom(v) {
		return v
	}
	if err := fn(k, v, x.opt); err != nil {
		return err
	}
	return nil
}

type guard struct { // rename to guard
	baseValue
	condition value
	value     yielder
}

func (x *guard) kind() kind { return topKind | referenceKind }

func (x *guard) yield(ctx *context, fn yieldFunc) evaluated {
	filter := ctx.manifest(x.condition)
	if isBottom(filter) {
		return filter
	}
	if err := checkKind(ctx, filter, boolKind); err != nil {
		return err
	}
	if filter.(*boolLit).b {
		if err := x.value.yield(ctx, fn); err != nil {
			return err
		}
	}
	return nil
}

type feed struct {
	baseValue
	source value
	fn     *lambdaExpr
}

func (x *feed) kind() kind { return topKind | referenceKind }

func (x *feed) yield(ctx *context, yfn yieldFunc) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "feed", x))
	}
	source := ctx.manifest(x.source)
	fn := x.fn // no need to evaluate eval

	switch src := source.(type) {
	case *structLit:
		src = src.expandFields(ctx)
		for i, a := range src.arcs {
			key := &stringLit{
				x.baseValue,
				ctx.labelStr(a.feature),
				nil,
			}
			val := src.at(ctx, i)
			v := fn.call(ctx, x, key, val)
			if isBottom(v) {
				return v.evalPartial(ctx)
			}
			if err := v.(yielder).yield(ctx, yfn); err != nil {
				return err
			}
		}
		return nil

	case *list:
		for i := range src.elem.arcs {
			idx := newNum(x, intKind)
			idx.v.SetInt64(int64(i))
			v := fn.call(ctx, x, idx, src.at(ctx, i))
			if isBottom(v) {
				return v.evalPartial(ctx)
			}
			if err := v.(yielder).yield(ctx, yfn); err != nil {
				return err
			}
		}
		return nil

	default:
		if isBottom(source) {
			return source
		}
		if k := source.kind(); k&(structKind|listKind) == bottomKind {
			return ctx.mkErr(x, x.source, "feed source must be list or struct, found %s", k)
		}
		return ctx.mkErr(x, "feed source not fully evaluated to struct or list")
	}
}
