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
	"unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd"
)

// Kind determines the underlying type of a Value.
type Kind int

const (
	// BottomKind is the error value.
	BottomKind Kind = 1 << iota

	// NullKind indicates a null value.
	NullKind

	// BoolKind indicates a boolean value.
	BoolKind

	// IntKind represents an integral number.
	IntKind

	// FloatKind represents a decimal float point number that cannot be
	// converted to an integer. The underlying number may still be integral,
	// but resulting from an operation that enforces the float type.
	FloatKind

	// StringKind indicates any kind of string.
	StringKind

	// BytesKind is a blob of data.
	BytesKind

	// StructKind is a kev-value map.
	StructKind

	// ListKind indicates a list of values.
	ListKind

	nextKind

	// NumberKind represents any kind of number.
	NumberKind = IntKind | FloatKind
)

// An structValue represents a JSON object.
//
// TODO: remove
type structValue struct {
	ctx  *context
	path *valueData
	n    *structLit
}

// Len reports the number of fields in this struct.
func (o *structValue) Len() int {
	if o.n == nil {
		return 0
	}
	return len(o.n.arcs)
}

// At reports the key and value of the ith field, i < o.Len().
func (o *structValue) At(i int) (key string, v Value) {
	a := o.n.arcs[i]
	v = newChildValue(o, i)
	return o.ctx.labelStr(a.feature), v
}

// Lookup reports the field for the given key. The returned Value is invalid
// if it does not exist.
func (o *structValue) Lookup(key string) Value {
	f := o.ctx.strLabel(key)
	i := 0
	len := o.Len()
	for ; i < len; i++ {
		if o.n.arcs[i].feature == f {
			break
		}
	}
	if i == len {
		// TODO: better message.
		return newValueRoot(o.ctx, o.ctx.mkErr(o.n, codeNotExist,
			"value %q not found", key))
	}
	// v, _ := o.n.lookup(o.ctx, f)
	// v = o.ctx.manifest(v)
	return newChildValue(o, i)
}

// MarshalJSON returns a valid JSON encoding or reports an error if any of the
// fields is invalid.
func (o *structValue) marshalJSON() (b []byte, err error) {
	b = append(b, '{')
	n := o.Len()
	for i := 0; i < n; i++ {
		k, v := o.At(i)
		s, err := json.Marshal(k)
		if err != nil {
			return nil, unwrapJSONError(k, err)
		}
		b = append(b, s...)
		b = append(b, ':')
		bb, err := json.Marshal(v)
		if err != nil {
			return nil, unwrapJSONError(k, err)
		}
		b = append(b, bb...)
		if i < n-1 {
			b = append(b, ',')
		}
	}
	b = append(b, '}')
	return b, nil
}

type marshalError struct {
	path string
	err  error
	// TODO: maybe also collect the file locations contributing to the
	// failing values?
}

func (e *marshalError) Error() string {
	return fmt.Sprintf("cue: marshal error at path %s: %v", e.path, e.err)
}

func unwrapJSONError(key interface{}, err error) error {
	if je, ok := err.(*json.MarshalerError); ok {
		err = je.Err
	}
	path := make([]string, 0, 2)
	if key != nil {
		path = append(path, fmt.Sprintf("%v", key))
	}
	if me, ok := err.(*marshalError); ok {
		if me.path != "" {
			path = append(path, me.path)
		}
		err = me.err
	}
	return &marshalError{strings.Join(path, "."), err}
}

// An Iterator iterates over values.
//
type Iterator struct {
	val   Value
	ctx   *context
	iter  iterAtter
	len   int
	p     int
	cur   Value
	f     label
	attrs *attributes
}

// Next advances the iterator to the next value and reports whether there was
// any. It must be called before the first call to Value or Key.
func (i *Iterator) Next() bool {
	if i.p >= i.len {
		i.cur = Value{}
		return false
	}
	arc := i.iter.iterAt(i.ctx, i.p)
	i.cur = i.val.makeChild(i.ctx, uint32(i.p), arc)
	i.f = arc.feature
	i.p++
	return true
}

// Value returns the current value in the list. It will panic if Next advanced
// past the last entry.
func (i *Iterator) Value() Value {
	return i.cur
}

// Label reports the label of the value if i iterates over struct fields and
// "" otherwise.
func (i *Iterator) Label() string {
	if i.f == 0 {
		return ""
	}
	return i.ctx.labelStr(i.f)
}

// IsHidden reports if a field is hidden from the data model.
func (i *Iterator) IsHidden() bool {
	return i.f&hidden != 0
}

// IsOptional reports if a field is optional.
func (i *Iterator) IsOptional() bool {
	return i.cur.path.arc.optional
}

// marshalJSON iterates over the list and generates JSON output. HasNext
// will return false after this operation.
func marshalList(l *Iterator) (b []byte, err error) {
	b = append(b, '[')
	if l.Next() {
		for i := 0; ; i++ {
			x, err := json.Marshal(l.Value())
			if err != nil {
				return nil, unwrapJSONError(i, err)
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

func (v Value) getNum(k kind) (*numLit, error) {
	v, _ = v.Default()
	if err := v.checkKind(v.ctx(), k); err != nil {
		return nil, err
	}
	n, _ := v.path.cache.(*numLit)
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
	if n.v.Form != 0 {
		return 0, ErrInfinite
	}
	if mant != nil {
		mant.Set(&n.v.Coeff)
		if n.v.Negative {
			mant.Neg(mant)
		}
	}
	return int(n.v.Exponent), nil
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
	nd := int(apd.NumDigits(&n.v.Coeff)) + int(n.v.Exponent)
	if n.v.Form == apd.Infinite {
		if n.v.Negative {
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
	ctx.Round(&d, &n.v)
	return d.Append(buf, fmt), nil
}

var (
	// ErrBelow indicates that a value was rounded down in a conversion.
	ErrBelow = errors.New("cue: value was rounded down")

	// ErrAbove indicates that a value was rounded up in a conversion.
	ErrAbove = errors.New("cue: value was rounded up")

	// ErrInfinite indicates that a value is infinite.
	ErrInfinite = errors.New("cue: infinite")
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
	if n.v.Exponent != 0 {
		panic("cue: exponent should always be nil for integer types")
	}
	z.Set(&n.v.Coeff)
	if n.v.Negative {
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
	if !n.v.Coeff.IsInt64() {
		if n.v.Negative {
			return math.MinInt64, ErrAbove
		}
		return math.MaxInt64, ErrBelow
	}
	i := n.v.Coeff.Int64()
	if n.v.Negative {
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
	if n.v.Negative {
		return 0, ErrAbove
	}
	if !n.v.Coeff.IsUint64() {
		return math.MaxUint64, ErrBelow
	}
	i := n.v.Coeff.Uint64()
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
	if n.v.Negative {
		if n.v.Cmp(smallestNegFloat64) == 1 {
			return -0, ErrAbove
		}
		if n.v.Cmp(maxNegFloat64) == -1 {
			return math.Inf(-1), ErrBelow
		}
	} else {
		if n.v.Cmp(smallestPosFloat64) == -1 {
			return 0, ErrBelow
		}
		if n.v.Cmp(maxPosFloat64) == 1 {
			return math.Inf(1), ErrAbove
		}
	}
	f, _ := n.v.Float64()
	return f, nil
}

type valueData struct {
	parent *valueData
	index  uint32
	arc
}

// path returns the path of the value.
func (v *valueData) appendPath(a []string, idx *index) ([]string, kind) {
	var k kind
	if v.parent != nil {
		a, k = v.parent.appendPath(a, idx)
	}
	switch k {
	case listKind:
		a = append(a, strconv.FormatInt(int64(v.index), 10))
	case structKind:
		f := idx.labelStr(v.arc.feature)
		if !isIdent(f) && !isNumber(f) {
			f = quote(f, '"')
		}
		a = append(a, f)
	}
	return a, v.arc.cache.kind()
}

var validIdent = []*unicode.RangeTable{unicode.L, unicode.N}

func isIdent(s string) bool {
	valid := []*unicode.RangeTable{unicode.Letter}
	for _, r := range s {
		if !unicode.In(r, valid...) && r != '_' {
			return false
		}
		valid = validIdent
	}
	return true
}

func isNumber(s string) bool {
	for _, r := range s {
		if r < '0' || '9' < r {
			return false
		}
	}
	return true
}

// Value holds any value, which may be a Boolean, Error, List, Null, Number,
// Struct, or String.
type Value struct {
	idx  *index
	path *valueData
}

func newValueRoot(ctx *context, x value) Value {
	v := x.evalPartial(ctx)
	return Value{ctx.index, &valueData{nil, 0, arc{cache: v, v: x}}}
}

func newChildValue(obj *structValue, i int) Value {
	a := obj.n.arcs[i]
	a.cache = obj.n.at(obj.ctx, i)

	return Value{obj.ctx.index, &valueData{obj.path, uint32(i), a}}
}

func remakeValue(base Value, v value) Value {
	path := *base.path
	path.v = v
	path.cache = v.evalPartial(base.ctx())
	return Value{base.idx, &path}
}

func (v Value) ctx() *context {
	return v.idx.newContext()
}

func (v Value) makeChild(ctx *context, i uint32, a arc) Value {
	return Value{v.idx, &valueData{v.path, i, a}}
}

func (v Value) eval(ctx *context) value {
	if v.path == nil || v.path.cache == nil {
		panic("undefined value")
	}
	return ctx.manifest(v.path.cache)
}

// Eval resolves the references of a value and returns the result.
// This method is not necessary to obtain concrete values.
func (v Value) Eval() Value {
	if v.path == nil {
		return v
	}
	return remakeValue(v, v.path.v.evalPartial(v.ctx()))
}

// Default reports the default value and whether it existed. It returns the
// normal value if there is no default.
func (v Value) Default() (Value, bool) {
	if v.path == nil {
		return v, false
	}
	u := v.path.v.evalPartial(v.ctx())
	x := v.ctx().manifest(u)
	if x != u {
		return remakeValue(v, x), true
	}
	return v, false
}

// Label reports he label used to obtain this value from the enclosing struct.
//
// TODO: get rid of this somehow. Probably by including a FieldInfo struct
// or the like.
func (v Value) Label() (string, bool) {
	if v.path.feature == 0 {
		return "", false
	}
	return v.idx.labelStr(v.path.feature), true
}

// Kind returns the kind of value. It returns BottomKind for atomic values that
// are not concrete. For instance, it will return BottomKind for the bounds
// >=0.
func (v Value) Kind() Kind {
	if v.path == nil {
		return BottomKind
	}
	k := v.path.v.evalPartial(v.ctx()).kind()
	if k.isGround() {
		switch {
		case k.isAnyOf(nullKind):
			return NullKind
		case k.isAnyOf(boolKind):
			return BoolKind
		case k&numKind == (intKind):
			return IntKind
		case k&numKind == (floatKind):
			return FloatKind
		case k.isAnyOf(numKind):
			return NumberKind
		case k.isAnyOf(bytesKind):
			return BytesKind
		case k.isAnyOf(stringKind):
			return StringKind
		case k.isAnyOf(structKind):
			return StructKind
		case k.isAnyOf(listKind):
			return ListKind
		}
	}
	return BottomKind
}

// IncompleteKind returns a mask of all kinds that this value may be.
func (v Value) IncompleteKind() Kind {
	if v.path == nil {
		return BottomKind
	}
	k := v.path.v.evalPartial(v.ctx()).kind()
	vk := BottomKind // Everything is a bottom kind.
	for i := kind(1); i < nonGround; i <<= 1 {
		if k&i != 0 {
			switch i {
			case nullKind:
				vk |= NullKind
			case boolKind:
				vk |= BoolKind
			case intKind:
				vk |= IntKind
			case floatKind:
				vk |= FloatKind
			case stringKind:
				vk |= StringKind
			case bytesKind:
				vk |= BytesKind
			case structKind:
				vk |= StructKind
			case listKind:
				vk |= ListKind
			}
		}
	}
	return vk
}

// MarshalJSON marshalls this value into valid JSON.
func (v Value) MarshalJSON() (b []byte, err error) {
	b, err = v.marshalJSON()
	if err != nil {
		return nil, unwrapJSONError(nil, err)
	}
	return b, nil
}

func (v Value) marshalJSON() (b []byte, err error) {
	v, _ = v.Default()
	if v.path == nil {
		return json.Marshal(nil)
	}
	ctx := v.idx.newContext()
	x := v.eval(ctx)
	// TODO: implement marshalles in value.
	switch k := x.kind(); k {
	case nullKind:
		return json.Marshal(nil)
	case boolKind:
		return json.Marshal(x.(*boolLit).b)
	case intKind, floatKind, numKind:
		return x.(*numLit).v.MarshalText()
	case stringKind:
		return json.Marshal(x.(*stringLit).str)
	case bytesKind:
		return json.Marshal(x.(*bytesLit).b)
	case listKind:
		l := x.(*list)
		i := Iterator{ctx: ctx, val: v, iter: l, len: len(l.elem.arcs)}
		return marshalList(&i)
	case structKind:
		obj, _ := v.structVal(ctx)
		return obj.marshalJSON()
	case bottomKind:
		return nil, x.(*bottom)
	default:
		if k.hasReferences() {
			return nil, v.idx.mkErr(x, "value %q contains unresolved references", debugStr(ctx, x))
		}
		if !k.isGround() {
			return nil, v.idx.mkErr(x, "cannot convert incomplete value %q to JSON", debugStr(ctx, x))
		}
		return nil, v.idx.mkErr(x, "cannot convert value %q of type %T to JSON", debugStr(ctx, x), x)
	}
}

// Syntax converts the possibly partially evaluated value into syntax. This
// can use used to print the value with package format.
func (v Value) Syntax(opts ...Option) ast.Node {
	if v.path == nil || v.path.cache == nil {
		return nil
	}
	ctx := v.ctx()
	o := getOptions(opts)
	if o.raw {
		return export(ctx, v.path.v, o)
	}
	return export(ctx, v.path.cache, o)
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
	if v.path == nil {
		return nil
	}
	return v.path.docs.appendDocs(nil)
}

// Split returns a list of values from which v originated such that
// the unification of all these values equals v and for all returned values
// Source returns a non-nil value.
func (v Value) Split() []Value {
	if v.path == nil {
		return nil
	}
	ctx := v.ctx()
	a := []Value{}
	for _, x := range separate(v.path.v) {
		path := *v.path
		path.cache = x.evalPartial(ctx)
		path.v = x
		a = append(a, Value{v.idx, &path})
	}
	return a
}

func separate(v value) (a []value) {
	c := v.computed()
	if c == nil || c.op != opUnify {
		return []value{v}
	}
	if c.x != nil {
		a = append(a, separate(c.x)...)
	}
	if c.y != nil {
		a = append(a, separate(c.y)...)
	}
	return a
}

// Source returns the original node for this value. The return value may not
// be a syntax.Expr. For instance, a struct kind may be represented by a
// struct literal, a field comprehension, or a file. It returns nil for
// computed nodes. Use Split to get all source values that apply to a field.
func (v Value) Source() ast.Node {
	if v.path == nil {
		return nil
	}
	return v.path.v.syntax()
}

// Err returns the error represented by v or nil v is not an error.
func (v Value) Err() error {
	// TODO(incomplete): change to not return an error for incomplete.
	if err := v.checkKind(v.ctx(), bottomKind); err != nil {
		return err
	}
	return nil
}

// Pos returns position information.
func (v Value) Pos() token.Pos {
	if v.path == nil || v.Source() == nil {
		return token.NoPos
	}
	pos := v.Source().Pos()
	return pos
}

// IsConcrete reports whether the current value is a concrete scalar value,
// not relying on default values, a terminal error, a list, or a struct.
// It does not verify that values of lists or structs are concrete themselves.
// To check whether there is a concrete default, use v.Default().IsConcrete().
func (v Value) IsConcrete() bool {
	if v.path == nil {
		return false // any is neither concrete, not a list or struct.
	}
	x := v.path.v.evalPartial(v.ctx())

	// Errors marked as incomplete are treated as not complete.
	if isIncomplete(x) {
		return false
	}
	// Other errors are considered ground.
	return x.kind().isGround()
}

// IsIncomplete is deprecated.
//
// It indicates that the value cannot be fully evaluated due to
// insufficient information.
func (v Value) IsIncomplete() bool {
	// TODO: remove
	x := v.eval(v.ctx())
	if x.kind().hasReferences() || !x.kind().isGround() {
		return true
	}
	return isIncomplete(x)
}

// IsValid reports whether this value is defined and evaluates to something
// other than an error.
func (v Value) IsValid() bool {
	if v.path == nil || v.path.cache == nil {
		return false
	}
	k := v.eval(v.ctx()).kind()
	return k != bottomKind && !v.IsIncomplete()
}

// Exists reports whether this value existed in the configuration.
func (v Value) Exists() bool {
	if v.path == nil {
		return false
	}
	return exists(v.eval(v.ctx()))
}

func (v Value) checkKind(ctx *context, want kind) *bottom {
	if v.path == nil {
		return errNotExists
	}
	// TODO: use checkKind
	x := v.eval(ctx)
	if b, ok := x.(*bottom); ok {
		return b
	}
	got := x.kind()
	if want != bottomKind {
		if got&want&concreteKind == bottomKind {
			return ctx.mkErr(x, "not of right kind (%v vs %v)", got, want)
		}
		if !got.isGround() {
			return ctx.mkErr(x, codeIncomplete,
				"non-concrete value %v", got)
		}
	}
	return nil
}

func makeInt(v Value, x int64) Value {
	n := &numLit{numBase: numBase{baseValue: v.path.v.base()}}
	n.v.SetInt64(x)
	return remakeValue(v, n)
}

// Len returns the number of items of the underlying value.
// For lists it reports the capacity of the list. For structs it indicates the
// number of fields, for bytes the number of bytes.
func (v Value) Len() Value {
	if v.path != nil {
		switch x := v.path.v.evalPartial(v.ctx()).(type) {
		case *list:
			return remakeValue(v, x.len.evalPartial(v.ctx()))
		case *bytesLit:
			return makeInt(v, int64(x.len()))
		case *stringLit:
			return makeInt(v, int64(x.len()))
		}
	}
	const msg = "len not supported for type %v"
	return remakeValue(v, v.ctx().mkErr(v.path.v, msg, v.Kind()))
}

// Elem returns the value of undefined element types of lists and structs.
func (v Value) Elem() (Value, bool) {
	ctx := v.ctx()
	switch x := v.path.cache.(type) {
	case *structLit:
		if x.template == nil {
			break
		}
		fn, ok := ctx.manifest(x.template).(*lambdaExpr)
		if !ok {
			// TODO: return an error instead.
			break
		}
		// Note, this template does not maintain the relation between the
		// the attribute value and the instance.
		y := fn.call(ctx, x, &basicType{x.baseValue, stringKind})
		return newValueRoot(ctx, y), true
	case *list:
		return newValueRoot(ctx, x.typ), true
	}
	return Value{}, false
}

// List creates an iterator over the values of a list or reports an error if
// v is not a list.
func (v Value) List() (Iterator, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	if err := v.checkKind(ctx, listKind); err != nil {
		return Iterator{ctx: ctx}, err
	}
	l := v.eval(ctx).(*list)
	return Iterator{ctx: ctx, val: v, iter: l, len: len(l.elem.arcs)}, nil
}

// Null reports an error if v is not null.
func (v Value) Null() error {
	v, _ = v.Default()
	if err := v.checkKind(v.ctx(), nullKind); err != nil {
		return err
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
		return false, err
	}
	return v.eval(ctx).(*boolLit).b, nil
}

// String returns the string value if v is a string or an error otherwise.
func (v Value) String() (string, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	if err := v.checkKind(ctx, stringKind); err != nil {
		return "", err
	}
	return v.eval(ctx).(*stringLit).str, nil
}

// Bytes returns a byte slice if v represents a list of bytes or an error
// otherwise.
func (v Value) Bytes() ([]byte, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	switch x := v.eval(ctx).(type) {
	case *bytesLit:
		return append([]byte(nil), x.b...), nil
	case *stringLit:
		return []byte(x.str), nil
	}
	return nil, v.checkKind(ctx, bytesKind|stringKind)
}

// Reader returns a new Reader if v is a string or bytes type and an error
// otherwise.
func (v Value) Reader() (io.Reader, error) {
	v, _ = v.Default()
	ctx := v.ctx()
	switch x := v.eval(ctx).(type) {
	case *bytesLit:
		return bytes.NewReader(x.b), nil
	case *stringLit:
		return strings.NewReader(x.str), nil
	}
	return nil, v.checkKind(ctx, stringKind|bytesKind)
}

// TODO: distinguish between optional, hidden, etc. Probably the best approach
// is to mark options in context and have a single function for creating
// a structVal.

// structVal returns an structVal or an error if v is not a struct.
func (v Value) structVal(ctx *context) (structValue, error) {
	return v.structValOpts(ctx, options{
		omitHidden:   true,
		omitOptional: true,
	})
}

// structVal returns an structVal or an error if v is not a struct.
func (v Value) structValOpts(ctx *context, o options) (structValue, error) {
	v, _ = v.Default()
	if err := v.checkKind(ctx, structKind); err != nil {
		return structValue{}, err
	}
	obj := v.eval(ctx).(*structLit)

	// TODO: This is expansion appropriate?
	obj = obj.expandFields(ctx) // expand comprehensions

	// check if any fields can be omitted
	needFilter := false
	if o.omitHidden || o.omitOptional {
		f := label(0)
		for _, a := range obj.arcs {
			f |= a.feature
			if o.omitOptional && a.optional {
				needFilter = true
			}
		}
		needFilter = needFilter || f&hidden != 0
	}

	if needFilter {
		arcs := make([]arc, len(obj.arcs))
		k := 0
		for _, a := range obj.arcs {
			if a.feature&hidden == 0 && !a.optional {
				arcs[k] = a
				k++
			}
		}
		arcs = arcs[:k]
		obj = &structLit{
			obj.baseValue,
			obj.emit,
			obj.template,
			nil,
			arcs,
			nil,
		}
	}
	return structValue{ctx, v.path, obj}, nil
}

// Fields creates an iterator over v's fields if v is a struct or an error
// otherwise.
func (v Value) Fields(opts ...Option) (Iterator, error) {
	o := options{omitHidden: true, omitOptional: true}
	o.updateOptions(opts)
	ctx := v.ctx()
	obj, err := v.structValOpts(ctx, o)
	if err != nil {
		return Iterator{ctx: ctx}, err
	}
	return Iterator{ctx: ctx, val: v, iter: obj.n, len: len(obj.n.arcs)}, nil
}

// Lookup reports the value starting from v, or an error if the path is not
// found. The empty path returns v itself.
//
// Lookup cannot be used to look up hidden fields.
func (v Value) Lookup(path ...string) Value {
	ctx := v.ctx()
	for _, k := range path {
		obj, err := v.structVal(ctx)
		if err != nil {
			return newValueRoot(ctx, err.(*bottom))
		}
		v = obj.Lookup(k)
	}
	return v
}

// Template returns a function that represents the template definition for a
// struct in a configuration file. It returns nil if v is not a struct kind or
// if there is no template associated with the struct.
//
// The returned function returns the value that would be unified with field
// given its name.
func (v Value) Template() func(label string) Value {
	ctx := v.ctx()
	x, ok := v.path.cache.(*structLit)
	if !ok || x.template == nil {
		return nil
	}
	fn, ok := ctx.manifest(x.template).(*lambdaExpr)
	if !ok {
		return nil
	}
	return func(label string) Value {
		arg := &stringLit{x.baseValue, label}
		y := fn.call(ctx, x, arg)
		return newValueRoot(ctx, y)
	}
}

// Subsumes reports whether w is an instance of v.
//
// Value v and w must be obtained from the same build.
// TODO: remove this requirement.
func (v Value) Subsumes(w Value) bool {
	ctx := v.ctx()
	return subsumes(ctx, v.eval(ctx), w.eval(ctx), subChoose)
}

// Unify reports the greatest lower bound of v and w.
//
// Value v and w must be obtained from the same build.
// TODO: remove this requirement.
func (v Value) Unify(w Value) Value {
	ctx := v.ctx()
	if v.path == nil {
		return w
	}
	if w.path == nil {
		return v
	}
	a := v.path.v
	b := w.path.v
	src := binSrc(token.NoPos, opUnify, a, b)
	val := mkBin(ctx, src.Pos(), opUnify, a, b)
	if err := validate(ctx, val.evalPartial(ctx)); err != nil {
		val = err
	}
	return newValueRoot(ctx, val)
}

// Format prints a debug version of a value.
func (v Value) Format(state fmt.State, verb rune) {
	ctx := v.ctx()
	if v.path == nil {
		fmt.Fprint(state, "<nil>")
		return
	}
	io.WriteString(state, debugStr(ctx, v.path.cache))
}

// Reference returns path from the root of the file referred to by this value
// or no path if this value is not a reference.
func (v Value) Reference() []string {
	// TODO: don't include references to hidden fields.
	if v.path == nil {
		return nil
	}
	sel, ok := v.path.v.(*selectorExpr)
	if !ok {
		return nil
	}
	return mkPath(v.ctx(), v.path, sel, 0)
}

func mkPath(c *context, up *valueData, sel *selectorExpr, d int) (a []string) {
	switch x := sel.x.(type) {
	case *selectorExpr:
		a = mkPath(c, up.parent, x, d+1)
	case *nodeRef:
		// the parent must exist.
		for ; up != nil && up.cache != x.node.(value); up = up.parent {
		}
		a = mkFromRoot(c, up, d+1)
	default:
		panic("should not happend")
	}
	return append(a, c.labelStr(sel.feature))
}

func mkFromRoot(c *context, up *valueData, d int) []string {
	if up == nil || up.parent == nil {
		return make([]string, 0, d)
	}
	a := mkFromRoot(c, up.parent, d+1)
	return append(a, c.labelStr(up.feature))
}

// References reports all references used to evaluate this value. It does not
// report references for sub fields if v is a struct.
//
// Deprecated: can be implemented in terms of Reference and Expr.
func (v Value) References() [][]string {
	ctx := v.ctx()
	pf := pathFinder{up: v.path}
	raw := v.path.v
	if raw == nil {
		return nil
	}
	rewrite(ctx, raw, pf.find)
	return pf.paths
}

type pathFinder struct {
	paths [][]string
	stack []string
	up    *valueData
}

func (p *pathFinder) find(ctx *context, v value) (value, bool) {
	switch x := v.(type) {
	case *selectorExpr:
		i := len(p.stack)
		p.stack = append(p.stack, ctx.labelStr(x.feature))
		rewrite(ctx, x.x, p.find)
		p.stack = p.stack[:i]
		return v, false
	case *nodeRef:
		i := len(p.stack)
		up := p.up
		for ; up != nil && up.cache != x.node.(value); up = up.parent {
		}
		for ; up != nil && up.feature > 0; up = up.parent {
			p.stack = append(p.stack, ctx.labelStr(up.feature))
		}
		path := make([]string, len(p.stack))
		for i, v := range p.stack {
			path[len(path)-1-i] = v
		}
		p.paths = append(p.paths, path)
		p.stack = p.stack[:i]
		return v, false
	case *structLit: // handled in sub fields
		return v, false
	}
	return v, true
}

type options struct {
	concrete     bool // enforce that values are concrete
	raw          bool // show original values
	hasHidden    bool
	omitHidden   bool
	omitOptional bool
	omitAttrs    bool
}

// An Option defines modes of evaluation.
type Option option

type option func(p *options)

// Used in Iter, Validate, Subsume?, Fields() Syntax, Export

// TODO: could also be used for subsumption.

// Concrete ensures that all values are concrete.
//
// For Validate this means it returns an error if this is not the case.
// In other cases a non-concrete value will be replaced with an error.
func Concrete(concrete bool) Option {
	return func(p *options) {
		if concrete {
			p.concrete = true
			if !p.hasHidden {
				p.omitHidden = true
			}
		} else {
			p.raw = true
		}
	}
}

// All indicates that all fields and values should be included in processing
// even if they can be elided or omitted.
func All() Option {
	return func(p *options) {
		p.omitAttrs = false
		p.omitHidden = false
		p.omitOptional = false
	}
}

// Hidden indicates that hidden fields should be included.
//
// Hidden fields may still be included if include is false,
// even if a value is not concrete.
func Hidden(include bool) Option {
	return func(p *options) {
		p.hasHidden = true
		p.omitHidden = !include
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

// Validate reports any errors, recursively. The returned error may be an
// errors.List reporting multiple errors, where the total number of errors
// reported may be less than the actual number.
func (v Value) Validate(opts ...Option) error {
	o := getOptions(opts)
	list := errors.List{}
	v.Walk(func(v Value) bool {
		if err := v.Err(); err != nil {
			if !o.concrete && isIncomplete(v.eval(v.ctx())) {
				return false
			}
			list.Add(err)
			if len(list) > 50 {
				return false // mostly to avoid some hypothetical cycle issue
			}
		}
		if o.concrete {
			if err := isGroundRecursive(v.ctx(), v.eval(v.ctx())); err != nil {
				list.Add(err)
			}
		}
		return true
	}, nil)
	if len(list) > 0 {
		list.Sort()
		// list.RemoveMultiples() // TODO: use RemoveMultiples when it is fixed
		// return list
		return list[0]
	}
	return nil
}

func isGroundRecursive(ctx *context, v value) error {
	switch x := v.(type) {
	case *list:
		for i := 0; i < len(x.elem.arcs); i++ {
			v := ctx.manifest(x.at(ctx, i))
			if err := isGroundRecursive(ctx, v); err != nil {
				return err
			}
		}
	default:
		if !x.kind().isGround() {
			return ctx.mkErr(v, "incomplete value %q", debugStr(ctx, v))
		}
	}
	return nil
}

// Walk descends into all values of v, calling f. If f returns false, Walk
// will not descent further.
func (v Value) Walk(before func(Value) bool, after func(Value)) {
	ctx := v.ctx()
	switch v.Kind() {
	case StructKind:
		if before != nil && !before(v) {
			return
		}
		obj, _ := v.structVal(ctx)
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
	if v.path == nil || v.path.attrs == nil {
		return Attribute{err: errNotExists}
	}
	for _, a := range v.path.attrs.attr {
		if a.key() != key {
			continue
		}
		at := Attribute{}
		if err := parseAttrBody(v.ctx(), nil, a.body(), &at.attr); err != nil {
			return Attribute{err: err.(error)}
		}
		return at
	}
	return Attribute{err: errNotExists}
}

var (
	errNoSuchAttribute = errors.New("entry for key does not exist")
)

// An Attribute contains meta data about a field.
type Attribute struct {
	attr parsedAttr
	err  error
}

// Err returns the error associated with this Attribute or nil if this
// attribute is valid.
func (a *Attribute) Err() error {
	return a.err
}

func (a *Attribute) hasPos(p int) error {
	if a.err != nil {
		return a.err
	}
	if p >= len(a.attr.fields) {
		return fmt.Errorf("field does not exist")
	}
	return nil
}

// String reports the possibly empty string value at the given position or
// an error the attribute is invalid or if the position does not exist.
func (a *Attribute) String(pos int) (string, error) {
	if err := a.hasPos(pos); err != nil {
		return "", err
	}
	return a.attr.fields[pos].text(), nil
}

// Int reports the integer at the given position or an error if the attribute is
// invalid, the position does not exist, or the value at the given position is
// not an integer.
func (a *Attribute) Int(pos int) (int64, error) {
	if err := a.hasPos(pos); err != nil {
		return 0, err
	}
	// TODO: use CUE's literal parser once it exists, allowing any of CUE's
	// number types.
	return strconv.ParseInt(a.attr.fields[pos].text(), 10, 64)
}

// Flag reports whether an entry with the given name exists at position pos or
// onwards or an error if the attribute is invalid or if the first pos-1 entries
// are not defined.
func (a *Attribute) Flag(pos int, key string) (bool, error) {
	if err := a.hasPos(pos - 1); err != nil {
		return false, err
	}
	for _, kv := range a.attr.fields[pos:] {
		if kv.text() == key {
			return true, nil
		}
	}
	return false, nil
}

// Lookup searches for an entry of the form key=value from position pos onwards
// and reports the value if found. It reports an error if the attribute is
// invalid or if the first pos-1 entries are not defined.
func (a *Attribute) Lookup(pos int, key string) (val string, found bool, err error) {
	if err := a.hasPos(pos - 1); err != nil {
		return "", false, err
	}
	for _, kv := range a.attr.fields[pos:] {
		if kv.key() == key {
			return kv.value(), true, nil
		}
	}
	return "", false, nil
}

// Expr reports the operation of the underlying expression and the values it
// operates on. The returned values are appended to the given slice, which may
// be nil.
//
// For unary expressions, it returns the single value of the expression.
//
// For binary expressions it returns first the left and right value, in that
// order. For associative operations however, (for instance '&' and '|'), it may
// return more than two values, where the operation is to be applied in
// sequence.
//
// For selector and index expressions it returns the subject and then the index.
// For selectors, the index is the string value of the identifier. For slice
// expressions, it returns the subject, low value, and high value, in that
// order.
//
// For interpolations it returns a sequence of values to be concatenated, some
// of which will be literal strings and some unevaluated expressions.
//
// A builtin call expression returns the value of the builtin followed by the
// args of the call.
//
// If v is not an expression, Partial append v itself.
// TODO: return v if this is complete? Yes for now
// TODO: add values if a == nil?
func (v Value) Expr() (Op, []Value) {
	if v.path == nil {
		return NoOp, nil
	}
	// TODO: replace appends with []Value{}. For not leave.
	a := []Value{}
	op := NoOp
	switch x := v.path.v.(type) {
	case *binaryExpr:
		a = append(a, remakeValue(v, x.left))
		a = append(a, remakeValue(v, x.right))
		op = opToOp[x.op]
	case *unaryExpr:
		a = append(a, remakeValue(v, x.x))
		op = opToOp[x.op]
	case *bound:
		a = append(a, remakeValue(v, x.value))
		op = opToOp[x.op]
	case *unification:
		// pre-expanded unification
		for _, conjunct := range x.values {
			a = append(a, remakeValue(v, conjunct))
		}
		op = AndOp
	case *disjunction:
		// Filter defaults that are subsumed by another value.
		count := 0
	outer:
		for _, disjunct := range x.values {
			if disjunct.marked {
				for _, n := range x.values {
					if !n.marked && subsumes(v.ctx(), n.val, disjunct.val, 0) {
						continue outer
					}
				}
			}
			count++
			a = append(a, remakeValue(v, disjunct.val))
		}
		if count > 1 {
			op = OrOp
		}
	case *interpolation:
		for _, p := range x.parts {
			a = append(a, remakeValue(v, p))
		}
		op = InterpolationOp
	case *selectorExpr:
		a = append(a, remakeValue(v, x.x))
		a = append(a, remakeValue(v, &stringLit{
			x.baseValue,
			v.ctx().labelStr(x.feature),
		}))
		op = SelectorOp
	case *indexExpr:
		a = append(a, remakeValue(v, x.x))
		a = append(a, remakeValue(v, x.index))
		op = IndexOp
	case *sliceExpr:
		a = append(a, remakeValue(v, x.x))
		a = append(a, remakeValue(v, x.lo))
		a = append(a, remakeValue(v, x.hi))
		op = SliceOp
	case *callExpr:
		a = append(a, remakeValue(v, x.x))
		for _, arg := range x.args {
			a = append(a, remakeValue(v, arg))
		}
		op = CallOp
	default:
		a = append(a, v)
	}
	return op, a
}
