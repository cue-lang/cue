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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v2"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/subsume"
	"cuelang.org/go/internal/core/validate"
)

// Kind determines the underlying type of a Value.
type Kind = adt.Kind

const BottomKind Kind = 0

const (
	// NullKind indicates a null value.
	NullKind Kind = adt.NullKind

	// BoolKind indicates a boolean value.
	BoolKind = adt.BoolKind

	// IntKind represents an integral number.
	IntKind = adt.IntKind

	// FloatKind represents a decimal float point number that cannot be
	// converted to an integer. The underlying number may still be integral,
	// but resulting from an operation that enforces the float type.
	FloatKind = adt.FloatKind

	// StringKind indicates any kind of string.
	StringKind = adt.StringKind

	// BytesKind is a blob of data.
	BytesKind = adt.BytesKind

	// StructKind is a kev-value map.
	StructKind = adt.StructKind

	// ListKind indicates a list of values.
	ListKind = adt.ListKind

	// _numberKind is used as a implementation detail inside
	// Kind.String to indicate NumberKind.

	// NumberKind represents any kind of number.
	NumberKind = IntKind | FloatKind

	TopKind = adt.TopKind
)

// An structValue represents a JSON object.
//
// TODO: remove
type structValue struct {
	ctx      *context
	v        Value
	obj      *adt.Vertex
	features []adt.Feature
}

// Len reports the number of fields in this struct.
func (o *structValue) Len() int {
	if o.obj == nil {
		return 0
	}
	return len(o.features)
}

// At reports the key and value of the ith field, i < o.Len().
func (o *structValue) At(i int) (key string, v Value) {
	f := o.features[i]
	return o.ctx.LabelStr(f), newChildValue(o, i)
}

func (o *structValue) at(i int) (v *adt.Vertex, isOpt bool) {
	f := o.features[i]
	arc := o.obj.Lookup(f)
	if arc == nil {
		arc = &adt.Vertex{
			Parent: o.v.v,
			Label:  f,
		}
		o.obj.MatchAndInsert(o.ctx.opCtx, arc)
		arc.Finalize(o.ctx.opCtx)
		isOpt = true
	}
	return arc, isOpt
}

// Lookup reports the field for the given key. The returned Value is invalid
// if it does not exist.
func (o *structValue) Lookup(key string) Value {
	f := o.ctx.StrLabel(key)
	i := 0
	len := o.Len()
	for ; i < len; i++ {
		if o.features[i] == f {
			break
		}
	}
	if i == len {
		// TODO: better message.
		ctx := o.ctx
		x := ctx.mkErr(o.obj, codeNotExist, "value %q not found", key)
		return newErrValue(o.v, x)
	}
	return newChildValue(o, i)
}

// MarshalJSON returns a valid JSON encoding or reports an error if any of the
// fields is invalid.
func (o *structValue) marshalJSON() (b []byte, err errors.Error) {
	b = append(b, '{')
	n := o.Len()
	for i := 0; i < n; i++ {
		k, v := o.At(i)
		s, err := json.Marshal(k)
		if err != nil {
			return nil, unwrapJSONError(err)
		}
		b = append(b, s...)
		b = append(b, ':')
		bb, err := json.Marshal(v)
		if err != nil {
			return nil, unwrapJSONError(err)
		}
		b = append(b, bb...)
		if i < n-1 {
			b = append(b, ',')
		}
	}
	b = append(b, '}')
	return b, nil
}

var _ errors.Error = &marshalError{}

type marshalError struct {
	err errors.Error
	b   *bottom
}

func toMarshalErr(v Value, b *bottom) error {
	return &marshalError{v.toErr(b), b}
}

func marshalErrf(v Value, src source, code errCode, msg string, args ...interface{}) error {
	arguments := append([]interface{}{code, msg}, args...)
	b := v.idx.mkErr(src, arguments...)
	return toMarshalErr(v, b)
}

func (e *marshalError) Error() string {
	return fmt.Sprintf("cue: marshal error: %v", e.err)
}

func (e *marshalError) Bottom() *adt.Bottom          { return e.b }
func (e *marshalError) Path() []string               { return e.err.Path() }
func (e *marshalError) Msg() (string, []interface{}) { return e.err.Msg() }
func (e *marshalError) Position() token.Pos          { return e.err.Position() }
func (e *marshalError) InputPositions() []token.Pos {
	return e.err.InputPositions()
}

func unwrapJSONError(err error) errors.Error {
	switch x := err.(type) {
	case *json.MarshalerError:
		return unwrapJSONError(x.Err)
	case *marshalError:
		return x
	case errors.Error:
		return &marshalError{x, nil}
	default:
		return &marshalError{errors.Wrapf(err, token.NoPos, "json error"), nil}
	}
}

// An Iterator iterates over values.
//
type Iterator struct {
	val   Value
	ctx   *context
	arcs  []field
	p     int
	cur   Value
	f     label
	isOpt bool
}

type field struct {
	arc        *adt.Vertex
	isOptional bool
}

// Next advances the iterator to the next value and reports whether there was
// any. It must be called before the first call to Value or Key.
func (i *Iterator) Next() bool {
	if i.p >= len(i.arcs) {
		i.cur = Value{}
		return false
	}
	f := i.arcs[i.p]
	f.arc.Finalize(i.ctx.opCtx)
	i.cur = makeValue(i.val.idx, f.arc)
	i.f = f.arc.Label
	i.isOpt = f.isOptional
	i.p++
	return true
}

// Value returns the current value in the list. It will panic if Next advanced
// past the last entry.
func (i *Iterator) Value() Value {
	return i.cur
}

func (i *Iterator) Feature() adt.Feature {
	return i.f
}

// Label reports the label of the value if i iterates over struct fields and
// "" otherwise.
func (i *Iterator) Label() string {
	if i.f == 0 {
		return ""
	}
	return i.ctx.LabelStr(i.f)
}

// IsHidden reports if a field is hidden from the data model.
func (i *Iterator) IsHidden() bool {
	return i.f.IsHidden()
}

// IsOptional reports if a field is optional.
func (i *Iterator) IsOptional() bool {
	return i.isOpt
}

// IsDefinition reports if a field is a definition.
func (i *Iterator) IsDefinition() bool {
	return i.f.IsDef()
}

// marshalJSON iterates over the list and generates JSON output. HasNext
// will return false after this operation.
func marshalList(l *Iterator) (b []byte, err errors.Error) {
	b = append(b, '[')
	if l.Next() {
		for i := 0; ; i++ {
			x, err := json.Marshal(l.Value())
			if err != nil {
				return nil, unwrapJSONError(err)
			}
			b = append(b, x...)
			if !l.Next() {
				break
			}
			b = append(b, ',')
		}
	}
	b = append(b, ']')
	return b, nil
}

func (v Value) getNum(k kind) (*numLit, errors.Error) {
	v, _ = v.Default()
	ctx := v.ctx()
	if err := v.checkKind(ctx, k); err != nil {
		return nil, v.toErr(err)
	}
	n, _ := v.eval(ctx).(*numLit)
	return n, nil
}

// MantExp breaks x into its mantissa and exponent components and returns the
// exponent. If a non-nil mant argument is provided its value is set to the
// mantissa of x. The components satisfy x == mant × 10**exp. It returns an
// error if v is not a number.
//
// The components are not normalized. For instance, 2.00 is represented mant ==
// 200 and exp == -2. Calling MantExp with a nil argument is an efficient way to
// get the exponent of the receiver.
func (v Value) MantExp(mant *big.Int) (exp int, err error) {
	n, err := v.getNum(numKind)
	if err != nil {
		return 0, err
	}
	if n.X.Form != 0 {
		return 0, ErrInfinite
	}
	if mant != nil {
		mant.Set(&n.X.Coeff)
		if n.X.Negative {
			mant.Neg(mant)
		}
	}
	return int(n.X.Exponent), nil
}

// Decimal is for internal use only. The Decimal type that is returned is
// subject to change.
func (v Value) Decimal() (d *internal.Decimal, err error) {
	n, err := v.getNum(numKind)
	if err != nil {
		return nil, err
	}
	return &n.X, nil
}

// AppendInt appends the string representation of x in the given base to buf and
// returns the extended buffer, or an error if the underlying number was not
// an integer.
func (v Value) AppendInt(buf []byte, base int) ([]byte, error) {
	i, err := v.Int(nil)
	if err != nil {
		return nil, err
	}
	return i.Append(buf, base), nil
}

// AppendFloat appends to buf the string form of the floating-point number x.
// It returns an error if v is not a number.
func (v Value) AppendFloat(buf []byte, fmt byte, prec int) ([]byte, error) {
	n, err := v.getNum(numKind)
	if err != nil {
		return nil, err
	}
	ctx := apd.BaseContext
	nd := int(apd.NumDigits(&n.X.Coeff)) + int(n.X.Exponent)
	if n.X.Form == apd.Infinite {
		if n.X.Negative {
			buf = append(buf, '-')
		}
		return append(buf, string('∞')...), nil
	}
	if fmt == 'f' && nd > 0 {
		ctx.Precision = uint32(nd + prec)
	} else {
		ctx.Precision = uint32(prec)
	}
	var d apd.Decimal
	ctx.Round(&d, &n.X)
	return d.Append(buf, fmt), nil
}

var (
	// ErrBelow indicates that a value was rounded down in a conversion.
	ErrBelow = errors.New("value was rounded down")

	// ErrAbove indicates that a value was rounded up in a conversion.
	ErrAbove = errors.New("value was rounded up")

	// ErrInfinite indicates that a value is infinite.
	ErrInfinite = errors.New("infinite")
)

// Int converts the underlying integral number to an big.Int. It reports an
// error if the underlying value is not an integer type. If a non-nil *Int
// argument z is provided, Int stores the result in z instead of allocating a
// new Int.
func (v Value) Int(z *big.Int) (*big.Int, error) {
	n, err := v.getNum(intKind)
	if err != nil {
		return nil, err
	}
	if z == nil {
		z = &big.Int{}
	}
	if n.X.Exponent != 0 {
		panic("cue: exponent should always be nil for integer types")
	}
	z.Set(&n.X.Coeff)
	if n.X.Negative {
		z.Neg(z)
	}
	return z, nil
}

// Int64 converts the underlying integral number to int64. It reports an
// error if the underlying value is not an integer type or cannot be represented
// as an int64. The result is (math.MinInt64, ErrAbove) for x < math.MinInt64,
// and (math.MaxInt64, ErrBelow) for x > math.MaxInt64.
func (v Value) Int64() (int64, error) {
	n, err := v.getNum(intKind)
	if err != nil {
		return 0, err
	}
	if !n.X.Coeff.IsInt64() {
		if n.X.Negative {
			return math.MinInt64, ErrAbove
		}
		return math.MaxInt64, ErrBelow
	}
	i := n.X.Coeff.Int64()
	if n.X.Negative {
		i = -i
	}
	return i, nil
}

// Uint64 converts the underlying integral number to uint64. It reports an
// error if the underlying value is not an integer type or cannot be represented
// as a uint64. The result is (0, ErrAbove) for x < 0, and
// (math.MaxUint64, ErrBelow) for x > math.MaxUint64.
func (v Value) Uint64() (uint64, error) {
	n, err := v.getNum(intKind)
	if err != nil {
		return 0, err
	}
	if n.X.Negative {
		return 0, ErrAbove
	}
	if !n.X.Coeff.IsUint64() {
		return math.MaxUint64, ErrBelow
	}
	i := n.X.Coeff.Uint64()
	return i, nil
}

// trimZeros trims 0's for better JSON respresentations.
func trimZeros(s string) string {
	n1 := len(s)
	s2 := strings.TrimRight(s, "0")
	n2 := len(s2)
	if p := strings.IndexByte(s2, '.'); p != -1 {
		if p == n2-1 {
			return s[:len(s2)+1]
		}
		return s2
	}
	if n1-n2 <= 4 {
		return s
	}
	return fmt.Sprint(s2, "e+", n1-n2)
}

var (
	smallestPosFloat64 *apd.Decimal
	smallestNegFloat64 *apd.Decimal
	maxPosFloat64      *apd.Decimal
	maxNegFloat64      *apd.Decimal
)

func init() {
	const (
		// math.SmallestNonzeroFloat64: 1 / 2**(1023 - 1 + 52)
		smallest = "4.940656458412465441765687928682213723651e-324"
		// math.MaxFloat64: 2**1023 * (2**53 - 1) / 2**52
		max = "1.797693134862315708145274237317043567981e+308"
	)
	ctx := apd.BaseContext
	ctx.Precision = 40

	var err error
	smallestPosFloat64, _, err = ctx.NewFromString(smallest)
	if err != nil {
		panic(err)
	}
	smallestNegFloat64, _, err = ctx.NewFromString("-" + smallest)
	if err != nil {
		panic(err)
	}
	maxPosFloat64, _, err = ctx.NewFromString(max)
	if err != nil {
		panic(err)
	}
	maxNegFloat64, _, err = ctx.NewFromString("-" + max)
	if err != nil {
		panic(err)
	}
}

// Float64 returns the float64 value nearest to x. It reports an error if v is
// not a number. If x is too small to be represented by a float64 (|x| <
// math.SmallestNonzeroFloat64), the result is (0, ErrBelow) or (-0, ErrAbove),
// respectively, depending on the sign of x. If x is too large to be represented
// by a float64 (|x| > math.MaxFloat64), the result is (+Inf, ErrAbove) or
// (-Inf, ErrBelow), depending on the sign of x.
func (v Value) Float64() (float64, error) {
	n, err := v.getNum(numKind)
	if err != nil {
		return 0, err
	}
	if n.X.Negative {
		if n.X.Cmp(smallestNegFloat64) == 1 {
			return -0, ErrAbove
		}
		if n.X.Cmp(maxNegFloat64) == -1 {
			return math.Inf(-1), ErrBelow
		}
	} else {
		if n.X.Cmp(smallestPosFloat64) == -1 {
			return 0, ErrBelow
		}
		if n.X.Cmp(maxPosFloat64) == 1 {
			return math.Inf(1), ErrAbove
		}
	}
	f, _ := n.X.Float64()
	return f, nil
}

func (v Value) appendPath(a []string) []string {
	for _, f := range v.v.Path() {
		switch f.Typ() {
		case adt.IntLabel:
			a = append(a, strconv.FormatInt(int64(f.Index()), 10))

		case adt.StringLabel:
			label := v.idx.LabelStr(f)
			if !f.IsDef() && !f.IsHidden() {
				if !ast.IsValidIdent(label) {
					label = literal.String.Quote(label)
				}
			}
			a = append(a, label)
		default:
			a = append(a, f.SelectorString(v.idx.Index))
		}
	}
	return a
}

// Value holds any value, which may be a Boolean, Error, List, Null, Number,
// Struct, or String.
type Value struct {
	idx *index
	v   *adt.Vertex
}

func newErrValue(v Value, b *bottom) Value {
	node := &adt.Vertex{Value: b}
	if v.v != nil {
		node.Label = v.v.Label
		node.Parent = v.v.Parent
	}
	node.UpdateStatus(adt.Finalized)
	node.AddConjunct(adt.MakeRootConjunct(nil, b))
	return makeValue(v.idx, node)
}

func newVertexRoot(ctx *context, x *adt.Vertex) Value {
	if ctx.opCtx != nil {
		// This is indicative of an zero Value. In some cases this is called
		// with an error value.
		x.Finalize(ctx.opCtx)
	} else {
		x.UpdateStatus(adt.Finalized)
	}
	return makeValue(ctx.index, x)
}

func newValueRoot(ctx *context, x value) Value {
	if n, ok := x.(*adt.Vertex); ok {
		return newVertexRoot(ctx, n)
	}
	node := &adt.Vertex{}
	node.AddConjunct(adt.MakeRootConjunct(nil, x))
	return newVertexRoot(ctx, node)
}

func newChildValue(o *structValue, i int) Value {
	arc, _ := o.at(i)
	return makeValue(o.v.idx, arc)
}

// Dereference reports the value v refers to if v is a reference or v itself
// otherwise.
func Dereference(v Value) Value {
	n := v.v
	if n == nil || len(n.Conjuncts) != 1 {
		return v
	}

	c := n.Conjuncts[0]
	r, _ := c.Expr().(adt.Resolver)
	if r == nil {
		return v
	}

	ctx := v.ctx()
	n, b := ctx.opCtx.Resolve(c.Env, r)
	if b != nil {
		return newErrValue(v, b)
	}
	return makeValue(v.idx, n)
}

// MakeValue converts an adt.Value and given OpContext to a Value. The context
// must be directly or indirectly obtained from the NewRuntime defined in this
// package and it will panic if this is not the case.
//
// For internal use only.
func MakeValue(ctx *adt.OpContext, v adt.Value) Value {
	runtime := ctx.Impl().(*runtime.Runtime)
	index := runtime.Data.(*index)

	return newValueRoot(index.newContext(), v)
}

func makeValue(idx *index, v *adt.Vertex) Value {
	if v.Status() == 0 || v.Value == nil {
		panic(fmt.Sprintf("not properly initialized (state: %v, value: %T)",
			v.Status(), v.Value))
	}
	return Value{idx, v}
}

func remakeValue(base Value, env *adt.Environment, v value) Value {
	// TODO: right now this is necessary because disjunctions do not have
	// populated conjuncts.
	if v, ok := v.(*adt.Vertex); ok && v.Status() >= adt.Partial {
		return Value{base.idx, v}
	}
	n := &adt.Vertex{Label: base.v.Label}
	n.AddConjunct(adt.MakeRootConjunct(env, v))
	n = base.ctx().manifest(n)
	n.Parent = base.v.Parent
	return makeValue(base.idx, n)
}

func remakeFinal(base Value, env *adt.Environment, v adt.Value) Value {
	n := &adt.Vertex{Parent: base.v.Parent, Label: base.v.Label, Value: v}
	n.UpdateStatus(adt.Finalized)
	return makeValue(base.idx, n)
}

func (v Value) ctx() *context {
	return v.idx.newContext()
}

func (v Value) makeChild(ctx *context, i uint32, a arc) Value {
	a.Parent = v.v
	return makeValue(v.idx, a)
}

// Eval resolves the references of a value and returns the result.
// This method is not necessary to obtain concrete values.
func (v Value) Eval() Value {
	if v.v == nil {
		return v
	}
	x := v.v
	// x = eval.FinalizeValue(v.idx.Runtime, v.v)
	// x.Finalize(v.ctx().opCtx)
	x = x.ToDataSingle()
	return makeValue(v.idx, x)
	// return remakeValue(v, nil, ctx.value(x))
}

// Default reports the default value and whether it existed. It returns the
// normal value if there is no default.
func (v Value) Default() (Value, bool) {
	if v.v == nil {
		return v, false
	}

	d := v.v.Default()
	if d == v.v {
		return v, false
	}
	return makeValue(v.idx, d), true

	// d, ok := v.v.Value.(*adt.Disjunction)
	// if !ok {
	// 	return v, false
	// }

	// var w *adt.Vertex

	// switch d.NumDefaults {
	// case 0:
	// 	return v, false

	// case 1:
	// 	w = d.Values[0]

	// default:
	// 	x := *v.v
	// 	x.Value = &adt.Disjunction{
	// 		Src:         d.Src,
	// 		Values:      d.Values[:d.NumDefaults],
	// 		NumDefaults: 0,
	// 	}
	// 	w = &x
	// }

	// w.Conjuncts = nil
	// for _, c := range v.v.Conjuncts {
	// 	// TODO: preserve field information.
	// 	expr, _ := stripNonDefaults(c.Expr())
	// 	w.AddConjunct(adt.MakeConjunct(c.Env, expr))
	// }

	// return makeValue(v.idx, w), true

	// if !stripped {
	// 	return v, false
	// }

	// n := *v.v
	// n.Conjuncts = conjuncts
	// return Value{v.idx, &n}, true

	// isDefault := false
	// for _, c := range v.v.Conjuncts {
	// 	if hasDisjunction(c.Expr()) {
	// 		isDefault = true
	// 		break
	// 	}
	// }

	// if !isDefault {
	// 	return v, false
	// }

	// TODO: record expanded disjunctions in output.
	// - Rename Disjunction to DisjunctionExpr
	// - Introduce Disjuncts with Values.
	// - In Expr introduce Star
	// - Don't pick default by default?

	// Evaluate the value.
	// 	x := eval.FinalizeValue(v.idx.Runtime, v.v)
	// 	if b, _ := x.Value.(*adt.Bottom); b != nil { // && b.IsIncomplete() {
	// 		return v, false
	// 	}
	// 	// Finalize and return here.
	// 	return Value{v.idx, x}, isDefault
}

// TODO: this should go: record preexpanded disjunctions in Vertex.
func hasDisjunction(expr adt.Expr) bool {
	switch x := expr.(type) {
	case *adt.DisjunctionExpr:
		return true
	case *adt.Conjunction:
		for _, v := range x.Values {
			if hasDisjunction(v) {
				return true
			}
		}
	case *adt.BinaryExpr:
		switch x.Op {
		case adt.OrOp:
			return true
		case adt.AndOp:
			return hasDisjunction(x.X) || hasDisjunction(x.Y)
		}
	}
	return false
}

// TODO: this should go: record preexpanded disjunctions in Vertex.
func stripNonDefaults(expr adt.Expr) (r adt.Expr, stripped bool) {
	switch x := expr.(type) {
	case *adt.DisjunctionExpr:
		if !x.HasDefaults {
			return x, false
		}
		d := *x
		d.Values = []adt.Disjunct{}
		for _, v := range x.Values {
			if v.Default {
				d.Values = append(d.Values, v)
			}
		}
		if len(d.Values) == 1 {
			return d.Values[0].Val, true
		}
		return &d, true

	case *adt.BinaryExpr:
		if x.Op != adt.AndOp {
			return x, false
		}
		a, sa := stripNonDefaults(x.X)
		b, sb := stripNonDefaults(x.Y)
		if sa || sb {
			bin := *x
			bin.X = a
			bin.Y = b
			return &bin, true
		}
		return x, false

	default:
		return x, false
	}
}

// Label reports he label used to obtain this value from the enclosing struct.
//
// TODO: get rid of this somehow. Probably by including a FieldInfo struct
// or the like.
func (v Value) Label() (string, bool) {
	if v.v == nil || v.v.Label == 0 {
		return "", false
	}
	return v.idx.LabelStr(v.v.Label), true
}

// Kind returns the kind of value. It returns BottomKind for atomic values that
// are not concrete. For instance, it will return BottomKind for the bounds
// >=0.
func (v Value) Kind() Kind {
	if v.v == nil {
		return BottomKind
	}
	c := v.v.Value
	if !adt.IsConcrete(c) {
		return BottomKind
	}
	if v.IncompleteKind() == adt.ListKind && !v.IsClosed() {
		return BottomKind
	}
	return c.Kind()
}

// IncompleteKind returns a mask of all kinds that this value may be.
func (v Value) IncompleteKind() Kind {
	if v.v == nil {
		return BottomKind
	}
	return v.v.Kind()
}

// MarshalJSON marshalls this value into valid JSON.
func (v Value) MarshalJSON() (b []byte, err error) {
	b, err = v.marshalJSON()
	if err != nil {
		return nil, unwrapJSONError(err)
	}
	return b, nil
}

func (v Value) marshalJSON() (b []byte, err error) {
	v, _ = v.Default()
	if v.v == nil {
		return json.Marshal(nil)
	}
	ctx := v.idx.newContext()
	x := v.eval(ctx)

	if _, ok := x.(adt.Resolver); ok {
		return nil, marshalErrf(v, x, codeIncomplete, "value %q contains unresolved references", ctx.str(x))
	}
	if !adt.IsConcrete(x) {
		return nil, marshalErrf(v, x, codeIncomplete, "cannot convert incomplete value %q to JSON", ctx.str(x))
	}

	// TODO: implement marshalles in value.
	switch k := x.Kind(); k {
	case nullKind:
		return json.Marshal(nil)
	case boolKind:
		return json.Marshal(x.(*boolLit).B)
	case intKind, floatKind, numKind:
		b, err := x.(*numLit).X.MarshalText()
		b = bytes.TrimLeft(b, "+")
		return b, err
	case stringKind:
		return json.Marshal(x.(*stringLit).Str)
	case bytesKind:
		return json.Marshal(x.(*bytesLit).B)
	case listKind:
		i, _ := v.List()
		return marshalList(&i)
	case structKind:
		obj, err := v.structValData(ctx)
		if err != nil {
			return nil, toMarshalErr(v, err)
		}
		return obj.marshalJSON()
	case bottomKind:
		return nil, toMarshalErr(v, x.(*bottom))
	default:
		return nil, marshalErrf(v, x, 0, "cannot convert value %q of type %T to JSON", ctx.str(x), x)
	}
}

// Syntax converts the possibly partially evaluated value into syntax. This
// can use used to print the value with package format.
func (v Value) Syntax(opts ...Option) ast.Node {
	// TODO: the default should ideally be simplified representation that
	// exactly represents the value. The latter can currently only be
	// ensured with Raw().
	if v.v == nil {
		return nil
	}
	var o options = getOptions(opts)
	// var inst *Instance

	p := export.Profile{
		Simplify:        !o.raw,
		TakeDefaults:    o.final,
		ShowOptional:    !o.omitOptional && !o.concrete,
		ShowDefinitions: !o.omitDefinitions && !o.concrete,
		ShowHidden:      !o.omitHidden && !o.concrete,
		ShowAttributes:  !o.omitAttrs,
		ShowDocs:        o.docs,
	}

	// var expr ast.Expr
	var err error
	var f *ast.File
	if o.concrete || o.final {
		// inst = v.instance()
		var expr ast.Expr
		expr, err = p.Value(v.idx.Runtime, v.v)
		if err != nil {
			return nil
		}

		// This introduces gratuitous unshadowing!
		f, err = astutil.ToFile(expr)
		if err != nil {
			return nil
		}
		// return expr
	} else {
		f, err = p.Def(v.idx.Runtime, v.v)
		if err != nil {
			panic(err)
		}
	}

outer:
	for _, d := range f.Decls {
		switch d.(type) {
		case *ast.Package, *ast.ImportDecl:
			return f
		case *ast.CommentGroup, *ast.Attribute:
		default:
			break outer
		}
	}

	if len(f.Decls) == 1 {
		if e, ok := f.Decls[0].(*ast.EmbedDecl); ok {
			return e.Expr
		}
	}
	return &ast.StructLit{
		Elts: f.Decls,
	}
}

// Decode initializes x with Value v. If x is a struct, it will validate the
// constraints specified in the field tags.
func (v Value) Decode(x interface{}) error {
	// TODO: optimize
	b, err := v.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, x)
}

// // EncodeJSON generates JSON for the given value.
// func (v Value) EncodeJSON(w io.Writer, v Value) error {
// 	return nil
// }

// Doc returns all documentation comments associated with the field from which
// the current value originates.
func (v Value) Doc() []*ast.CommentGroup {
	if v.v == nil {
		return nil
	}
	return export.ExtractDoc(v.v)
}

// Split returns a list of values from which v originated such that
// the unification of all these values equals v and for all returned values.
// It will also split unchecked unifications (embeddings), so unifying the
// split values may fail if actually unified.
// Source returns a non-nil value.
//
// Deprecated: use Expr.
func (v Value) Split() []Value {
	if v.v == nil {
		return nil
	}
	a := []Value{}
	for _, x := range v.v.Conjuncts {
		a = append(a, remakeValue(v, x.Env, x.Expr()))
	}
	return a
}

// Source returns the original node for this value. The return value may not
// be a syntax.Expr. For instance, a struct kind may be represented by a
// struct literal, a field comprehension, or a file. It returns nil for
// computed nodes. Use Split to get all source values that apply to a field.
func (v Value) Source() ast.Node {
	if v.v == nil {
		return nil
	}
	if len(v.v.Conjuncts) == 1 {
		return v.v.Conjuncts[0].Source()
	}
	return v.v.Value.Source()
}

// Err returns the error represented by v or nil v is not an error.
func (v Value) Err() error {
	if err := v.checkKind(v.ctx(), bottomKind); err != nil {
		return v.toErr(err)
	}
	return nil
}

// Pos returns position information.
func (v Value) Pos() token.Pos {
	if v.v == nil || v.Source() == nil {
		return token.NoPos
	}
	pos := v.Source().Pos()
	return pos
}

// TODO: IsFinal: this value can never be changed.

// IsClosed reports whether a list of struct is closed. It reports false when
// when the value is not a list or struct.
func (v Value) IsClosed() bool {
	if v.v == nil {
		return false
	}
	return v.v.IsClosed(v.ctx().opCtx)
}

// IsConcrete reports whether the current value is a concrete scalar value
// (not relying on default values), a terminal error, a list, or a struct.
// It does not verify that values of lists or structs are concrete themselves.
// To check whether there is a concrete default, use v.Default().IsConcrete().
func (v Value) IsConcrete() bool {
	if v.v == nil {
		return false // any is neither concrete, not a list or struct.
	}
	if b, ok := v.v.Value.(*adt.Bottom); ok {
		return !b.IsIncomplete()
	}
	if !adt.IsConcrete(v.v) {
		return false
	}
	if v.IncompleteKind() == adt.ListKind && !v.IsClosed() {
		return false
	}
	return true
}

// // Deprecated: IsIncomplete
// //
// // It indicates that the value cannot be fully evaluated due to
// // insufficient information.
// func (v Value) IsIncomplete() bool {
// 	panic("deprecated")
// }

// Exists reports whether this value existed in the configuration.
func (v Value) Exists() bool {
	if v.v == nil {
		return false
	}
	return exists(v.v.Value)
}

func (v Value) checkKind(ctx *context, want kind) *bottom {
	if v.v == nil {
		return errNotExists
	}
	// TODO: use checkKind
	x := v.eval(ctx)
	if b, ok := x.(*bottom); ok {
		return b
	}
	k := x.Kind()
	if want != bottomKind {
		if k&want == bottomKind {
			return ctx.mkErr(x, "cannot use value %v (type %s) as %s",
				ctx.opCtx.Str(x), k, want)
		}
		if !adt.IsConcrete(x) {
			return ctx.mkErr(x, codeIncomplete, "non-concrete value %v", k)
		}
	}
	return nil
}

func makeInt(v Value, x int64) Value {
	n := &adt.Num{K: intKind}
	n.X.SetInt64(int64(x))
	return remakeFinal(v, nil, n)
}

// Len returns the number of items of the underlying value.
// For lists it reports the capacity of the list. For structs it indicates the
// number of fields, for bytes the number of bytes.
func (v Value) Len() Value {
	if v.v != nil {
		switch x := v.eval(v.ctx()).(type) {
		case *adt.Vertex:
			if x.IsList() {
				ctx := v.ctx()
				n := &adt.Num{K: intKind}
				n.X.SetInt64(int64(len(x.Elems())))
				if x.IsClosed(ctx.opCtx) {
					return remakeFinal(v, nil, n)
				}
				// Note: this HAS to be a Conjunction value and cannot be
				// an adt.BinaryExpr, as the expressions would be considered
				// to be self-contained and unresolvable when evaluated
				// (can never become concrete).
				c := &adt.Conjunction{Values: []adt.Value{
					&adt.BasicType{K: adt.IntKind},
					&adt.BoundValue{Op: adt.GreaterEqualOp, Value: n},
				}}
				return remakeFinal(v, nil, c)

			}
		case *bytesLit:
			return makeInt(v, int64(len(x.B)))
		case *stringLit:
			return makeInt(v, int64(len([]rune(x.Str))))
		}
	}
	const msg = "len not supported for type %v"
	return remakeValue(v, nil, v.ctx().mkErr(v.v, msg, v.Kind()))

}

// Elem returns the value of undefined element types of lists and structs.
func (v Value) Elem() (Value, bool) {
	if v.v == nil {
		return Value{}, false
	}
	ctx := v.ctx().opCtx
	x := &adt.Vertex{
		Parent: v.v,
		Label:  0,
	}
	v.v.Finalize(ctx)
	v.v.MatchAndInsert(ctx, x)
	if len(x.Conjuncts) == 0 {
		return Value{}, false
	}
	x.Finalize(ctx)
	return makeValue(v.idx, x), true
}

// // BulkOptionals returns all bulk optional fields as key-value pairs.
// // See also Elem and Template.
// func (v Value) BulkOptionals() [][2]Value {
// 	x, ok := v.path.cache.(*structLit)
// 	if !ok {
// 		return nil
// 	}
// 	return v.appendBulk(nil, x.optionals)
// }

// func (v Value) appendBulk(a [][2]Value, x *optionals) [][2]Value {
// 	if x == nil {
// 		return a
// 	}
// 	a = v.appendBulk(a, x.left)
// 	a = v.appendBulk(a, x.right)
// 	for _, set := range x.fields {
// 		if set.key != nil {
// 			ctx := v.ctx()
// 			fn, ok := ctx.manifest(set.value).(*lambdaExpr)
// 			if !ok {
// 				// create error
// 				continue
// 			}
// 			x := fn.call(ctx, set.value, &basicType{K: stringKind})

// 			a = append(a, [2]Value{v.makeElem(set.key), v.makeElem(x)})
// 		}
// 	}
// 	return a
// }

// List creates an iterator over the values of a list or reports an error if
// v is not a list.
func (v Value) List() (Iterator, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	if err := v.checkKind(ctx, listKind); err != nil {
		return Iterator{ctx: ctx}, v.toErr(err)
	}
	arcs := []field{}
	for _, a := range v.v.Elems() {
		if a.Label.IsInt() {
			arcs = append(arcs, field{arc: a})
		}
	}
	return Iterator{ctx: ctx, val: v, arcs: arcs}, nil
}

// Null reports an error if v is not null.
func (v Value) Null() error {
	v, _ = v.Default()
	if err := v.checkKind(v.ctx(), nullKind); err != nil {
		return v.toErr(err)
	}
	return nil
}

// // IsNull reports whether v is null.
// func (v Value) IsNull() bool {
// 	return v.Null() == nil
// }

// Bool returns the bool value of v or false and an error if v is not a boolean.
func (v Value) Bool() (bool, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	if err := v.checkKind(ctx, boolKind); err != nil {
		return false, v.toErr(err)
	}
	return v.eval(ctx).(*boolLit).B, nil
}

// String returns the string value if v is a string or an error otherwise.
func (v Value) String() (string, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	if err := v.checkKind(ctx, stringKind); err != nil {
		return "", v.toErr(err)
	}
	return v.eval(ctx).(*stringLit).Str, nil
}

// Bytes returns a byte slice if v represents a list of bytes or an error
// otherwise.
func (v Value) Bytes() ([]byte, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	switch x := v.eval(ctx).(type) {
	case *bytesLit:
		return append([]byte(nil), x.B...), nil
	case *stringLit:
		return []byte(x.Str), nil
	}
	return nil, v.toErr(v.checkKind(ctx, bytesKind|stringKind))
}

// Reader returns a new Reader if v is a string or bytes type and an error
// otherwise.
func (v Value) Reader() (io.Reader, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	switch x := v.eval(ctx).(type) {
	case *bytesLit:
		return bytes.NewReader(x.B), nil
	case *stringLit:
		return strings.NewReader(x.Str), nil
	}
	return nil, v.toErr(v.checkKind(ctx, stringKind|bytesKind))
}

// TODO: distinguish between optional, hidden, etc. Probably the best approach
// is to mark options in context and have a single function for creating
// a structVal.

// structVal returns an structVal or an error if v is not a struct.
func (v Value) structValData(ctx *context) (structValue, *bottom) {
	return v.structValOpts(ctx, options{
		omitHidden:      true,
		omitDefinitions: true,
		omitOptional:    true,
	})
}

func (v Value) structValFull(ctx *context) (structValue, *bottom) {
	return v.structValOpts(ctx, options{})
}

// structVal returns an structVal or an error if v is not a struct.
func (v Value) structValOpts(ctx *context, o options) (structValue, *bottom) {
	v, _ = v.Default()

	obj, err := v.getStruct()
	if err != nil {
		return structValue{}, err
	}

	features := export.VertexFeatures(obj)

	k := 0
	for _, f := range features {
		if f.IsDef() && (o.omitDefinitions || o.concrete) {
			continue
		}
		if f.IsHidden() && o.omitHidden {
			continue
		}
		if arc := obj.Lookup(f); arc == nil {
			if o.omitOptional {
				continue
			}
			// ensure it really exists.
			v := adt.Vertex{
				Parent: obj,
				Label:  f,
			}
			obj.MatchAndInsert(ctx.opCtx, &v)
			if len(v.Conjuncts) == 0 {
				continue
			}
		}
		features[k] = f
		k++
	}
	features = features[:k]
	return structValue{ctx, v, obj, features}, nil
}

// Struct returns the underlying struct of a value or an error if the value
// is not a struct.
func (v Value) Struct() (*Struct, error) {
	ctx := v.ctx()
	obj, err := v.structValOpts(ctx, options{})
	if err != nil {
		return nil, v.toErr(err)
	}
	return &Struct{obj}, nil
}

func (v Value) getStruct() (*structLit, *bottom) {
	ctx := v.ctx()
	if err := v.checkKind(ctx, structKind); err != nil {
		if !err.ChildError {
			return nil, err
		}
	}
	return v.v, nil
}

// Struct represents a CUE struct value.
type Struct struct {
	structValue
}

// FieldInfo contains information about a struct field.
type FieldInfo struct {
	Selector string
	Name     string // Deprecated: use Selector
	Pos      int
	Value    Value

	IsDefinition bool
	IsOptional   bool
	IsHidden     bool
}

func (s *Struct) Len() int {
	return s.structValue.Len()
}

// field reports information about the ith field, i < o.Len().
func (s *Struct) Field(i int) FieldInfo {
	a, opt := s.at(i)
	ctx := s.v.ctx()

	v := makeValue(s.v.idx, a)
	name := ctx.LabelStr(a.Label)
	str := a.Label.SelectorString(ctx.opCtx)
	return FieldInfo{str, name, i, v, a.Label.IsDef(), opt, a.Label.IsHidden()}
}

// FieldByName looks up a field for the given name. If isIdent is true, it will
// look up a definition or hidden field (starting with `_` or `_#`). Otherwise
// it interprets name as an arbitrary string for a regular field.
func (s *Struct) FieldByName(name string, isIdent bool) (FieldInfo, error) {
	f := s.v.ctx().Label(name, isIdent)
	for i, a := range s.features {
		if a == f {
			return s.Field(i), nil
		}
	}
	return FieldInfo{}, errNotFound
}

// Fields creates an iterator over the Struct's fields.
func (s *Struct) Fields(opts ...Option) *Iterator {
	iter, _ := s.v.Fields(opts...)
	return iter
}

// Fields creates an iterator over v's fields if v is a struct or an error
// otherwise.
func (v Value) Fields(opts ...Option) (*Iterator, error) {
	o := options{omitDefinitions: true, omitHidden: true, omitOptional: true}
	o.updateOptions(opts)
	ctx := v.ctx()
	obj, err := v.structValOpts(ctx, o)
	if err != nil {
		return &Iterator{ctx: ctx}, v.toErr(err)
	}

	arcs := []field{}
	for i := range obj.features {
		arc, isOpt := obj.at(i)
		arcs = append(arcs, field{arc: arc, isOptional: isOpt})
	}
	return &Iterator{ctx: ctx, val: v, arcs: arcs}, nil
}

// Lookup reports the value at a path starting from v. The empty path returns v
// itself. Use LookupDef for definitions or LookupField for any kind of field.
//
// The Exists() method can be used to verify if the returned value existed.
// Lookup cannot be used to look up hidden or optional fields or definitions.
func (v Value) Lookup(path ...string) Value {
	ctx := v.ctx()
	for _, k := range path {
		// TODO(eval) TODO(error): always search in full data and change error
		// message if a field is found but is of the incorrect type.
		obj, err := v.structValData(ctx)
		if err != nil {
			// TODO: return a Value at the same location and a new error?
			return newErrValue(v, err)
		}
		v = obj.Lookup(k)
	}
	return v
}

// LookupDef reports the definition with the given name within struct v. The
// Exists method of the returned value will report false if the definition did
// not exist. The Err method reports if any error occurred during evaluation.
func (v Value) LookupDef(name string) Value {
	ctx := v.ctx()
	o, err := v.structValFull(ctx)
	if err != nil {
		return newErrValue(v, err)
	}

	f := v.ctx().Label(name, true)
	for i, a := range o.features {
		if a == f {
			if f.IsHidden() || !f.IsDef() { // optional not possible for now
				break
			}
			return newChildValue(&o, i)
		}
	}
	if !strings.HasPrefix(name, "#") {
		alt := v.LookupDef("#" + name)
		// Use the original error message if this resulted in an error as well.
		if alt.Err() == nil {
			return alt
		}
	}
	return newErrValue(v, ctx.mkErr(v.v, "definition %q not found", name))
}

var errNotFound = errors.Newf(token.NoPos, "field not found")

// FieldByName looks up a field for the given name. If isIdent is true, it will
// look up a definition or hidden field (starting with `_` or `_#`). Otherwise
// it interprets name as an arbitrary string for a regular field.
func (v Value) FieldByName(name string, isIdent bool) (f FieldInfo, err error) {
	s, err := v.Struct()
	if err != nil {
		return f, err
	}
	return s.FieldByName(name, isIdent)
}

// LookupField reports information about a field of v.
//
// Deprecated: this API does not work with new-style definitions. Use FieldByName.
func (v Value) LookupField(name string) (FieldInfo, error) {
	s, err := v.Struct()
	if err != nil {
		// TODO: return a Value at the same location and a new error?
		return FieldInfo{}, err
	}
	f, err := s.FieldByName(name, true)
	if err != nil {
		return f, err
	}
	if f.IsHidden {
		return f, errNotFound
	}
	return f, err
}

// TODO: expose this API?
//
// // EvalExpr evaluates an expression within the scope of v, which must be
// // a struct.
// //
// // Expressions may refer to builtin packages if they can be uniquely identified.
// func (v Value) EvalExpr(expr ast.Expr) Value {
// 	ctx := v.ctx()
// 	result := evalExpr(ctx, v.eval(ctx), expr)
// 	return newValueRoot(ctx, result)
// }

// Fill creates a new value by unifying v with the value of x at the given path.
//
// Values may be any Go value that can be converted to CUE, an ast.Expr or
// a Value. In the latter case, it will panic if the Value is not from the same
// Runtime.
//
// Any reference in v referring to the value at the given path will resolve
// to x in the newly created value. The resulting value is not validated.
func (v Value) Fill(x interface{}, path ...string) Value {
	if v.v == nil {
		return v
	}
	ctx := v.ctx()
	for i := len(path) - 1; i >= 0; i-- {
		x = map[string]interface{}{path[i]: x}
	}
	var value = convert.GoValueToValue(ctx.opCtx, x, true)
	n, _ := value.(*adt.Vertex)
	if n == nil {
		n = &adt.Vertex{Label: v.v.Label}
		n.AddConjunct(adt.MakeRootConjunct(nil, value))
	} else {
		n.Label = v.v.Label
	}
	n.Finalize(ctx.opCtx)
	n.Parent = v.v.Parent
	w := makeValue(v.idx, n)
	return v.Unify(w)
}

// Template returns a function that represents the template definition for a
// struct in a configuration file. It returns nil if v is not a struct kind or
// if there is no template associated with the struct.
//
// The returned function returns the value that would be unified with field
// given its name.
func (v Value) Template() func(label string) Value {
	// TODO: rename to optional.
	if v.v == nil {
		return nil
	}

	types := v.v.OptionalTypes()
	if types&(adt.HasAdditional|adt.HasPattern) == 0 {
		return nil
	}

	parent := v.v
	ctx := v.ctx().opCtx
	return func(label string) Value {
		f := ctx.StringLabel(label)
		arc := &adt.Vertex{Parent: parent, Label: f}
		v.v.MatchAndInsert(ctx, arc)
		if len(arc.Conjuncts) == 0 {
			return Value{}
		}
		arc.Finalize(ctx)
		return makeValue(v.idx, arc)
	}
}

// Subsume reports nil when w is an instance of v or an error otherwise.
//
// Without options, the entire value is considered for assumption, which means
// Subsume tests whether  v is a backwards compatible (newer) API version of w.
// Use the Final() to indicate that the subsumed value is data, and that
//
// Use the Final option to check subsumption if a w is known to be final,
// and should assumed to be closed.
//
// Options are currently ignored and the function will panic if any are passed.
//
// Value v and w must be obtained from the same build.
// TODO: remove this requirement.
func (v Value) Subsume(w Value, opts ...Option) error {
	o := getOptions(opts)
	p := subsume.CUE
	switch {
	case o.final && o.ignoreClosedness:
		p = subsume.FinalOpen
	case o.final:
		p = subsume.Final
	case o.ignoreClosedness:
		p = subsume.API
	}
	p.Defaults = true
	ctx := v.ctx().opCtx
	return p.Value(ctx, v.v, w.v)
}

// Deprecated: use Subsume.
//
// Subsumes reports whether w is an instance of v.
//
// Without options, Subsumes checks whether v is a backwards compatbile schema
// of w.
//
// By default, Subsumes tests whether two values are compatible
// Value v and w must be obtained from the same build.
// TODO: remove this requirement.
func (v Value) Subsumes(w Value) bool {
	p := subsume.Profile{Defaults: true}
	return p.Check(v.ctx().opCtx, v.v, w.v)
}

// Unify reports the greatest lower bound of v and w.
//
// Value v and w must be obtained from the same build.
// TODO: remove this requirement.
func (v Value) Unify(w Value) Value {
	// ctx := v.ctx()
	if v.v == nil {
		return w
	}
	if w.v == nil {
		return v
	}
	n := &adt.Vertex{Label: v.v.Label}
	n.AddConjunct(adt.MakeRootConjunct(nil, v.v))
	n.AddConjunct(adt.MakeRootConjunct(nil, w.v))

	ctx := v.idx.newContext()
	n.Finalize(ctx.opCtx)

	// We do not set the parent until here as we don't want to "inherit" the
	// closedness setting from v.
	n.Parent = v.v.Parent
	return makeValue(v.idx, n)
}

// UnifyAccept is as v.Unify(w), but will disregard any field that is allowed
// in the Value accept.
func (v Value) UnifyAccept(w Value, accept Value) Value {
	if v.v == nil {
		return w
	}
	if w.v == nil {
		return v
	}
	if accept.v == nil {
		panic("accept must exist")
	}

	n := &adt.Vertex{Parent: v.v.Parent, Label: v.v.Label}
	n.AddConjunct(adt.MakeRootConjunct(nil, v.v))
	n.AddConjunct(adt.MakeRootConjunct(nil, w.v))

	e := eval.New(v.idx.Runtime)
	ctx := e.NewContext(n)
	e.UnifyAccept(ctx, n, adt.Finalized, accept.v.Closed) // check for nil.

	// ctx := v.idx.newContext()
	n.Closed = accept.v.Closed
	n.Finalize(ctx)
	return makeValue(v.idx, n)
}

// Equals reports whether two values are equal, ignoring optional fields.
// The result is undefined for incomplete values.
func (v Value) Equals(other Value) bool {
	if v.v == nil || other.v == nil {
		return false
	}
	return eval.Equal(v.ctx().opCtx, v.v, other.v)

}

// Format prints a debug version of a value.
func (v Value) Format(state fmt.State, verb rune) {
	ctx := v.ctx()
	if v.v == nil {
		fmt.Fprint(state, "<nil>")
		return
	}
	switch {
	case state.Flag('#'):
		_, _ = io.WriteString(state, ctx.str(v.v))
	case state.Flag('+'):
		_, _ = io.WriteString(state, ctx.opCtx.Str(v.v))
	default:
		n, _ := export.Raw.Expr(v.idx.Runtime, v.v)
		b, _ := format.Node(n)
		_, _ = state.Write(b)
	}
}

func (v Value) instance() *Instance {
	if v.v == nil {
		return nil
	}
	return v.ctx().getImportFromNode(v.v)
}

// Reference returns the instance and path referred to by this value such that
// inst.Lookup(path) resolves to the same value, or no path if this value is not
// a reference. If a reference contains index selection (foo[bar]), it will
// only return a reference if the index resolves to a concrete value.
func (v Value) Reference() (inst *Instance, path []string) {
	// TODO: don't include references to hidden fields.
	if v.v == nil || len(v.v.Conjuncts) != 1 {
		return nil, nil
	}
	ctx := v.ctx()
	c := v.v.Conjuncts[0]

	return reference(ctx, c.Env, c.Expr())
}

func reference(c *context, env *adt.Environment, r adt.Expr) (inst *Instance, path []string) {
	ctx := c.opCtx
	defer ctx.PopState(ctx.PushState(env, r.Source()))

	switch x := r.(type) {
	case *adt.FieldReference:
		env := ctx.Env(x.UpCount)
		inst, path = mkPath(c, nil, env.Vertex)
		path = append(path, x.Label.SelectorString(c.Index))

	case *adt.LabelReference:
		env := ctx.Env(x.UpCount)
		return mkPath(c, nil, env.Vertex)

	case *adt.DynamicReference:
		env := ctx.Env(x.UpCount)
		inst, path = mkPath(c, nil, env.Vertex)
		v, _ := ctx.Evaluate(env, x.Label)
		str := ctx.StringValue(v)
		path = append(path, str)

	case *adt.ImportReference:
		imp := x.ImportPath.StringValue(ctx)
		inst = c.index.getImportFromPath(imp)

	case *adt.SelectorExpr:
		inst, path = reference(c, env, x.X)
		path = append(path, x.Sel.SelectorString(ctx))

	case *adt.IndexExpr:
		inst, path = reference(c, env, x.X)
		v, _ := ctx.Evaluate(env, x.Index)
		str := ctx.StringValue(v)
		path = append(path, str)
	}
	if inst == nil {
		return nil, nil
	}
	return inst, path
}

func mkPath(ctx *context, a []string, v *adt.Vertex) (inst *Instance, path []string) {
	if v.Parent == nil {
		return ctx.index.getImportFromNode(v), a
	}
	inst, path = mkPath(ctx, a, v.Parent)
	path = append(path, v.Label.SelectorString(ctx.opCtx))
	return inst, path
}

// // References reports all references used to evaluate this value. It does not
// // report references for sub fields if v is a struct.
// //
// // Deprecated: can be implemented in terms of Reference and Expr.
// func (v Value) References() [][]string {
// 	panic("deprecated")
// }

type options struct {
	concrete          bool // enforce that values are concrete
	raw               bool // show original values
	hasHidden         bool
	omitHidden        bool
	omitDefinitions   bool
	omitOptional      bool
	omitAttrs         bool
	resolveReferences bool
	final             bool
	ignoreClosedness  bool // used for comparing APIs
	docs              bool
	disallowCycles    bool // implied by concrete
}

// An Option defines modes of evaluation.
type Option option

type option func(p *options)

// Final indicates a value is final. It implicitly closes all structs and lists
// in a value and selects defaults.
func Final() Option {
	return func(o *options) {
		o.final = true
		o.omitDefinitions = true
		o.omitOptional = true
		o.omitHidden = true
	}
}

// Schema specifies the input is a Schema. Used by Subsume.
func Schema() Option {
	return func(o *options) {
		o.ignoreClosedness = true
	}
}

// Concrete ensures that all values are concrete.
//
// For Validate this means it returns an error if this is not the case.
// In other cases a non-concrete value will be replaced with an error.
func Concrete(concrete bool) Option {
	return func(p *options) {
		if concrete {
			p.concrete = true
			p.final = true
			if !p.hasHidden {
				p.omitHidden = true
				p.omitDefinitions = true
			}
		}
	}
}

// DisallowCycles forces validation in the precense of cycles, even if
// non-concrete values are allowed. This is implied by Concrete(true).
func DisallowCycles(disallow bool) Option {
	return func(p *options) { p.disallowCycles = disallow }
}

// ResolveReferences forces the evaluation of references when outputting.
// This implies the input cannot have cycles.
func ResolveReferences(resolve bool) Option {
	return func(p *options) { p.resolveReferences = resolve }
}

// Raw tells Syntax to generate the value as is without any simplifications.
func Raw() Option {
	return func(p *options) { p.raw = true }
}

// All indicates that all fields and values should be included in processing
// even if they can be elided or omitted.
func All() Option {
	return func(p *options) {
		p.omitAttrs = false
		p.omitHidden = false
		p.omitDefinitions = false
		p.omitOptional = false
	}
}

// Docs indicates whether docs should be included.
func Docs(include bool) Option {
	return func(p *options) { p.docs = true }
}

// Definitions indicates whether definitions should be included.
//
// Definitions may still be included for certain functions if they are referred
// to by other other values.
func Definitions(include bool) Option {
	return func(p *options) {
		p.hasHidden = true
		p.omitDefinitions = !include
	}
}

// Hidden indicates that definitions and hidden fields should be included.
//
// Deprecated: Hidden fields are deprecated.
func Hidden(include bool) Option {
	return func(p *options) {
		p.hasHidden = true
		p.omitHidden = !include
		p.omitDefinitions = !include
	}
}

// Optional indicates that optional fields should be included.
func Optional(include bool) Option {
	return func(p *options) { p.omitOptional = !include }
}

// Attributes indicates that attributes should be included.
func Attributes(include bool) Option {
	return func(p *options) { p.omitAttrs = !include }
}

func getOptions(opts []Option) (o options) {
	o.updateOptions(opts)
	return
}

func (o *options) updateOptions(opts []Option) {
	for _, fn := range opts {
		fn(o)
	}
}

// Validate reports any errors, recursively. The returned error may represent
// more than one error, retrievable with errors.Errors, if more than one
// exists.
func (v Value) Validate(opts ...Option) error {
	o := options{}
	o.updateOptions(opts)

	cfg := &validate.Config{
		Concrete:       o.concrete,
		DisallowCycles: o.disallowCycles,
		AllErrors:      true,
	}

	b := validate.Validate(v.ctx().opCtx, v.v, cfg)
	if b != nil {
		return b.Err
	}
	return nil
}

// Walk descends into all values of v, calling f. If f returns false, Walk
// will not descent further. It only visits values that are part of the data
// model, so this excludes optional fields, hidden fields, and definitions.
func (v Value) Walk(before func(Value) bool, after func(Value)) {
	ctx := v.ctx()
	switch v.Kind() {
	case StructKind:
		if before != nil && !before(v) {
			return
		}
		obj, _ := v.structValData(ctx)
		for i := 0; i < obj.Len(); i++ {
			_, v := obj.At(i)
			v.Walk(before, after)
		}
	case ListKind:
		if before != nil && !before(v) {
			return
		}
		list, _ := v.List()
		for list.Next() {
			list.Value().Walk(before, after)
		}
	default:
		if before != nil {
			before(v)
		}
	}
	if after != nil {
		after(v)
	}
}

// Attribute returns the attribute data for the given key.
// The returned attribute will return an error for any of its methods if there
// is no attribute for the requested key.
func (v Value) Attribute(key string) Attribute {
	// look up the attributes
	if v.v == nil {
		return Attribute{internal.NewNonExisting(key)}
	}
	// look up the attributes
	for _, a := range export.ExtractFieldAttrs(v.v.Conjuncts) {
		k, body := a.Split()
		if key != k {
			continue
		}
		return Attribute{internal.ParseAttrBody(token.NoPos, body)}
	}

	return Attribute{internal.NewNonExisting(key)}
}

// An Attribute contains meta data about a field.
type Attribute struct {
	attr internal.Attr
}

// Err returns the error associated with this Attribute or nil if this
// attribute is valid.
func (a *Attribute) Err() error {
	return a.attr.Err
}

// String reports the possibly empty string value at the given position or
// an error the attribute is invalid or if the position does not exist.
func (a *Attribute) String(pos int) (string, error) {
	return a.attr.String(pos)
}

// Int reports the integer at the given position or an error if the attribute is
// invalid, the position does not exist, or the value at the given position is
// not an integer.
func (a *Attribute) Int(pos int) (int64, error) {
	return a.attr.Int(pos)
}

// Flag reports whether an entry with the given name exists at position pos or
// onwards or an error if the attribute is invalid or if the first pos-1 entries
// are not defined.
func (a *Attribute) Flag(pos int, key string) (bool, error) {
	return a.attr.Flag(pos, key)
}

// Lookup searches for an entry of the form key=value from position pos onwards
// and reports the value if found. It reports an error if the attribute is
// invalid or if the first pos-1 entries are not defined.
func (a *Attribute) Lookup(pos int, key string) (val string, found bool, err error) {
	return a.attr.Lookup(pos, key)
}

// Expr reports the operation of the underlying expression and the values it
// operates on.
//
// For unary expressions, it returns the single value of the expression.
//
// For binary expressions it returns first the left and right value, in that
// order. For associative operations however, (for instance '&' and '|'), it may
// return more than two values, where the operation is to be applied in
// sequence.
//
// For selector and index expressions it returns the subject and then the index.
// For selectors, the index is the string value of the identifier.
//
// For interpolations it returns a sequence of values to be concatenated, some
// of which will be literal strings and some unevaluated expressions.
//
// A builtin call expression returns the value of the builtin followed by the
// args of the call.
func (v Value) Expr() (Op, []Value) {
	// TODO: return v if this is complete? Yes for now
	if v.v == nil {
		return NoOp, nil
	}

	var expr adt.Expr
	var env *adt.Environment

	if v.v.IsData() {
		switch x := v.v.Value.(type) {
		case *adt.ListMarker, *adt.StructMarker:
			expr = v.v
		default:
			expr = x
		}

	} else {
		switch len(v.v.Conjuncts) {
		case 0:
			if v.v.Value == nil {
				return NoOp, []Value{makeValue(v.idx, v.v)}
			}
			switch x := v.v.Value.(type) {
			case *adt.ListMarker, *adt.StructMarker:
				expr = v.v
			default:
				expr = x
			}

		case 1:
			// the default case, processed below.
			c := v.v.Conjuncts[0]
			env = c.Env
			expr = c.Expr()
			if w, ok := expr.(*adt.Vertex); ok {
				return Value{v.idx, w}.Expr()
			}

		default:
			a := []Value{}
			ctx := v.ctx().opCtx
			for _, c := range v.v.Conjuncts {
				// Keep parent here. TODO: do we need remove the requirement
				// from other conjuncts?
				n := &adt.Vertex{
					Parent: v.v.Parent,
					Label:  v.v.Label,
				}
				n.AddConjunct(c)
				n.Finalize(ctx)
				a = append(a, makeValue(v.idx, n))
			}
			return adt.AndOp, a
		}
	}

	// TODO: replace appends with []Value{}. For not leave.
	a := []Value{}
	op := NoOp
	switch x := expr.(type) {
	case *binaryExpr:
		a = append(a, remakeValue(v, env, x.X))
		a = append(a, remakeValue(v, env, x.Y))
		op = x.Op
	case *unaryExpr:
		a = append(a, remakeValue(v, env, x.X))
		op = x.Op
	case *boundExpr:
		a = append(a, remakeValue(v, env, x.Expr))
		op = x.Op
	case *boundValue:
		a = append(a, remakeValue(v, env, x.Value))
		op = x.Op
	case *adt.Conjunction:
		// pre-expanded unification
		for _, conjunct := range x.Values {
			a = append(a, remakeValue(v, env, conjunct))
		}
		op = AndOp
	case *adt.Disjunction:
		count := 0
	outer:
		for i, disjunct := range x.Values {
			if i < x.NumDefaults {
				for _, n := range x.Values[x.NumDefaults:] {
					if subsume.Value(v.ctx().opCtx, n, disjunct) == nil {
						continue outer
					}
				}
			}
			count++
			a = append(a, remakeValue(v, env, disjunct))
		}
		if count > 1 {
			op = OrOp
		}

	case *adt.DisjunctionExpr:
		// Filter defaults that are subsumed by another value.
		count := 0
	outerExpr:
		for _, disjunct := range x.Values {
			if disjunct.Default {
				for _, n := range x.Values {
					a := adt.Vertex{
						Label:  v.v.Label,
						Closed: v.v.Closed,
					}
					b := a
					a.AddConjunct(adt.MakeRootConjunct(env, n.Val))
					b.AddConjunct(adt.MakeRootConjunct(env, disjunct.Val))

					e := eval.New(v.idx.Runtime)
					ctx := e.NewContext(nil)
					e.UnifyAccept(ctx, &a, adt.Finalized, v.v.Closed)
					e.UnifyAccept(ctx, &b, adt.Finalized, v.v.Closed)
					a.Parent = v.v.Parent
					if !n.Default && subsume.Value(ctx, &a, &b) == nil {
						continue outerExpr
					}
				}
			}
			count++
			a = append(a, remakeValue(v, env, disjunct.Val))
		}
		if count > 1 {
			op = adt.OrOp
		}

	case *interpolation:
		for _, p := range x.Parts {
			a = append(a, remakeValue(v, env, p))
		}
		op = InterpolationOp

	case *adt.FieldReference:
		// TODO: allow hard link
		ctx := v.ctx().opCtx
		f := ctx.PushState(env, x.Src)
		env := ctx.Env(x.UpCount)
		a = append(a, remakeValue(v, nil, &adt.NodeLink{Node: env.Vertex}))
		a = append(a, remakeValue(v, nil, ctx.NewString(x.Label.SelectorString(ctx))))
		_ = ctx.PopState(f)
		op = SelectorOp

	case *selectorExpr:
		a = append(a, remakeValue(v, env, x.X))
		// A string selector is quoted.
		a = append(a, remakeValue(v, env, &adt.String{
			Str: x.Sel.SelectorString(v.idx.Index),
		}))
		op = SelectorOp

	case *indexExpr:
		a = append(a, remakeValue(v, env, x.X))
		a = append(a, remakeValue(v, env, x.Index))
		op = IndexOp
	case *sliceExpr:
		a = append(a, remakeValue(v, env, x.X))
		a = append(a, remakeValue(v, env, x.Lo))
		a = append(a, remakeValue(v, env, x.Hi))
		op = SliceOp
	case *callExpr:
		a = append(a, remakeValue(v, env, x.Fun))
		for _, arg := range x.Args {
			a = append(a, remakeValue(v, env, arg))
		}
		op = CallOp
	case *customValidator:
		a = append(a, remakeValue(v, env, x.Builtin))
		for _, arg := range x.Args {
			a = append(a, remakeValue(v, env, arg))
		}
		op = CallOp

	case *adt.StructLit:
		// Simulate old embeddings.
		envEmbed := &adt.Environment{
			Up:     env,
			Vertex: v.v,
		}
		fields := []adt.Decl{}
		ctx := v.ctx().opCtx
		for _, d := range x.Decls {
			switch x := d.(type) {
			case adt.Expr:
				// embedding
				n := &adt.Vertex{Label: v.v.Label}
				c := adt.MakeRootConjunct(envEmbed, x)
				n.AddConjunct(c)
				n.Finalize(ctx)
				n.Parent = v.v.Parent
				a = append(a, makeValue(v.idx, n))

			default:
				fields = append(fields, d)
			}
		}
		if len(a) == 0 {
			a = append(a, v)
			break
		}

		if len(fields) > 0 {
			n := &adt.Vertex{
				Label: v.v.Label,
			}
			c := adt.MakeRootConjunct(env, &adt.StructLit{
				Decls: fields,
			})
			n.AddConjunct(c)
			n.Finalize(ctx)
			n.Parent = v.v.Parent
			a = append(a, makeValue(v.idx, n))
		}

		op = adt.AndOp

	default:
		a = append(a, v)
	}
	return op, a
}
