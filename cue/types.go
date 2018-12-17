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
	"strings"

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

	// NumberKind represents any kind of number.
	NumberKind

	// StringKind indicates any kind of string.
	StringKind

	// BytesKind is a blob of data.
	BytesKind

	// StructKind is a kev-value map.
	StructKind

	// ListKind indicates a list of values.
	ListKind

	nextKind
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
	for ; i < len(o.n.arcs); i++ {
		if o.n.arcs[i].feature == f {
			break
		}
	}
	if i == len(o.n.arcs) {
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
func (o *structValue) MarshalJSON() (b []byte, err error) {
	b = append(b, '{')
	n := o.Len()
	for i := 0; i < n; i++ {
		k, v := o.At(i)
		s, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b = append(b, s...)
		b = append(b, ':')
		bb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		b = append(b, bb...)
		if i < n-1 {
			b = append(b, ',')
		}
	}
	b = append(b, '}')
	return b, nil
}

// An Iterator iterates over values.
//
type Iterator struct {
	val  Value
	ctx  *context
	iter iterAtter
	len  int
	p    int
	cur  Value
	f    label
}

// Next advances the iterator to the next value and reports whether there was
// any. It must be called before the first call to Value or Key.
func (i *Iterator) Next() bool {
	if i.p >= i.len {
		i.cur = Value{}
		return false
	}
	eval, orig, f := i.iter.iterAt(i.ctx, i.p)
	i.cur = i.val.makeChild(i.ctx, uint32(i.p), f, eval, orig)
	i.f = f
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

// IsHidden reports if a field is hidden from the data model. This may only
// be true if the field was obtained using AllFields.
func (i *Iterator) IsHidden() bool {
	return i.f&hidden != 0
}

// marshalJSON iterates over the list and generates JSON output. HasNext
// will return false after this operation.
func marshalList(l *Iterator) (b []byte, err error) {
	b = append(b, '[')
	if l.Next() {
		for {
			x, err := json.Marshal(l.Value())
			if err != nil {
				return nil, err
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
	if err := v.checkKind(v.ctx(), k); err != nil {
		return nil, err
	}
	n, _ := v.path.v.(*numLit)
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

// IsInt reports whether n is a integral number type.
func (v Value) IsInt() bool {
	_, err := v.getNum(intKind) // TODO: make more efficient.
	return err == nil
}

// IsUint reports whether n is a positive integral number type.
func (v Value) IsUint() bool {
	if !v.IsInt() {
		return false
	}
	n, _ := v.path.v.(*numLit)
	return n.v.Sign() >= 0
}

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
	parent  *valueData
	feature label
	index   uint32
	v       evaluated
	raw     value
}

// Value holds any value, which may be a Boolean, Error, List, Null, Number,
// Struct, or String.
type Value struct {
	idx  *index
	path *valueData
}

func newValueRoot(ctx *context, x value) Value {
	v := x.evalPartial(ctx)
	return Value{ctx.index, &valueData{nil, 0, 0, v, x}}
}

func newChildValue(obj *structValue, i int) Value {
	eval := obj.ctx.manifest(obj.n.at(obj.ctx, i))
	a := obj.n.arcs[i]
	return Value{obj.ctx.index, &valueData{obj.path, a.feature, uint32(i), eval, a.v}}
}

func (v Value) ctx() *context {
	return v.idx.newContext()
}

func (v Value) makeChild(ctx *context, i uint32, f label, eval evaluated, raw value) Value {
	eval = ctx.manifest(eval)
	return Value{v.idx, &valueData{v.path, f, i, eval, raw}}
}

func (v Value) eval(ctx *context) value {
	if v.path == nil || v.path.v == nil {
		panic("undefined value")
	}
	return ctx.manifest(v.path.v)
}

// Default returs v if v.Exists or a value converted from x otherwise.
func (v Value) Default(x interface{}) Value {
	return v
}

// Label reports he label used to obtain this value from the enclosing struct.
//
// TODO: get rid of this somehow. Maybe by passing it to walk
func (v Value) Label() (string, bool) {
	if v.path.feature == 0 {
		return "", false
	}
	return v.idx.labelStr(v.path.feature), true
}

// Kind returns the kind of value. It returns BottomKind for atomic values that
// are not concrete. For instance, it will return BottomKind for the range
// 0..5.
func (v Value) Kind() Kind {
	k := v.eval(v.ctx()).kind()
	if k.isGround() {
		switch {
		case k.isAnyOf(nullKind):
			return NullKind
		case k.isAnyOf(boolKind):
			return BoolKind
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
	k := v.eval(v.ctx()).kind()
	vk := BottomKind // Everything is a bottom kind.
	for i := kind(1); i < nonGround; i <<= 1 {
		if k&i != 0 {
			switch i {
			case nullKind:
				vk |= NullKind
			case boolKind:
				vk |= BoolKind
			case intKind, floatKind:
				vk |= NumberKind
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
		i := Iterator{ctx: ctx, val: v, iter: l, len: len(l.a)}
		return marshalList(&i)
	case structKind:
		obj, _ := v.structVal(ctx)
		return obj.MarshalJSON()
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
func (v Value) Syntax() ast.Expr {
	if v.path == nil || v.path.v == nil {
		return nil
	}
	ctx := v.ctx()
	return export(ctx, v.eval(ctx))
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

// Split returns a list of values from which v originated such that
// the unification of all these values equals v and for all returned values
// Source returns a non-nil value.
func (v Value) Split() []Value {
	if v.path == nil {
		return nil
	}
	ctx := v.ctx()
	a := []Value{}
	for _, x := range separate(v.path.raw) {
		path := *v.path
		path.v = x.evalPartial(ctx)
		path.raw = x
		a = append(a, Value{v.idx, &path})
	}
	return a
}

func separate(v value) (a []value) {
	c := v.computed()
	if c == nil {
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
	return v.path.raw.syntax()
}

// Err returns the error represented by v or nil v is not an error.
func (v Value) Err() error {
	if err := v.checkKind(v.ctx(), bottomKind); err != nil {
		return err
	}
	return nil
}

// Pos returns position information.
func (v Value) Pos() token.Position {
	if v.path == nil || v.Source() == nil {
		return token.Position{}
	}
	pos := v.Source().Pos()
	return v.idx.fset.Position(pos)
}

// IsIncomplete indicates that the value cannot be fully evaluated due to
// insufficient information.
func (v Value) IsIncomplete() bool {
	x := v.eval(v.ctx())
	if x.kind().hasReferences() || !x.kind().isGround() {
		return true
	}
	return isIncomplete(x)
}

// IsValid reports whether this value is defined and evaluates to something
// other than an error.
func (v Value) IsValid() bool {
	if v.path == nil || v.path.v == nil {
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
	if got&want&concreteKind == bottomKind && want != bottomKind {
		return ctx.mkErr(x, "not of right kind (%v vs %v)", got, want)
	}
	if !got.isGround() {
		return ctx.mkErr(x, codeIncomplete,
			"non-concrete value %v when evaluating config file", got)
	}
	return nil
}

// List creates an iterator over the values of a list or reports an error if
// v is not a list.
func (v Value) List() (Iterator, error) {
	ctx := v.ctx()
	if err := v.checkKind(ctx, listKind); err != nil {
		return Iterator{ctx: ctx}, err
	}
	l := v.eval(ctx).(*list)
	return Iterator{ctx: ctx, val: v, iter: l, len: len(l.a)}, nil
}

// Null reports an error if v is not null.
func (v Value) Null() error {
	if err := v.checkKind(v.ctx(), nullKind); err != nil {
		return err
	}
	return nil
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool {
	return v.Null() == nil
}

// Bool returns the bool value of v or false and an error if v is not a boolean.
func (v Value) Bool() (bool, error) {
	ctx := v.ctx()
	if err := v.checkKind(ctx, boolKind); err != nil {
		return false, err
	}
	return v.eval(ctx).(*boolLit).b, nil
}

// String returns the string value if v is a string or an error otherwise.
func (v Value) String() (string, error) {
	ctx := v.ctx()
	if err := v.checkKind(ctx, stringKind); err != nil {
		return "", err
	}
	return v.eval(ctx).(*stringLit).str, nil
}

// Bytes returns a byte slice if v represents a list of bytes or an error
// otherwise.
func (v Value) Bytes() ([]byte, error) {
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
	ctx := v.ctx()
	switch x := v.eval(ctx).(type) {
	case *bytesLit:
		return bytes.NewReader(x.b), nil
	case *stringLit:
		return strings.NewReader(x.str), nil
	}
	return nil, v.checkKind(ctx, stringKind|bytesKind)
}

// structVal returns an structVal or an error if v is not a struct.
func (v Value) structVal(ctx *context) (structValue, error) {
	if err := v.checkKind(ctx, structKind); err != nil {
		return structValue{}, err
	}
	obj := v.eval(ctx).(*structLit)

	obj.expand(ctx) // expand comprehensions

	// check if any labels are hidden
	f := label(0)
	for _, a := range obj.arcs {
		f |= a.feature
	}

	if f&hidden != 0 {
		arcs := make([]arc, len(obj.arcs))
		k := 0
		for _, a := range obj.arcs {
			if a.feature&hidden == 0 {
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
		}

	}
	return structValue{ctx, v.path, obj}, nil
}

func (v Value) structValWithHidden(ctx *context) (structValue, error) {
	if err := v.checkKind(ctx, structKind); err != nil {
		return structValue{}, err
	}
	obj := v.eval(ctx).(*structLit)
	obj.expand(ctx)

	return structValue{ctx, v.path, obj}, nil
}

// Fields creates an iterator over v's fields if v is a struct or an error
// otherwise.
func (v Value) Fields() (Iterator, error) {
	ctx := v.ctx()
	obj, err := v.structVal(ctx)
	if err != nil {
		return Iterator{ctx: ctx}, err
	}
	return Iterator{ctx: ctx, val: v, iter: obj.n, len: len(obj.n.arcs)}, nil
}

// AllFields creates an iterator over all of v's fields, including the hidden
// ones, if v is a struct or an error otherwise.
func (v Value) AllFields() (Iterator, error) {
	ctx := v.ctx()
	obj, err := v.structValWithHidden(ctx)
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
	x, ok := v.path.v.(*structLit)
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
	a := v.eval(ctx)
	b := w.eval(ctx)
	val := ctx.manifest(mkBin(ctx, token.NoPos, opUnify, a, b))
	if err := validate(ctx, val); err != nil {
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
	io.WriteString(state, debugStr(ctx, v.path.v))
}

// References reports all references used to evaluate this value. It does not
// report references for sub fields if v is a struct.
func (v Value) References() [][]string {
	ctx := v.ctx()
	pf := pathFinder{up: v.path}
	raw := v.path.raw
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
		for ; up != nil && up.v != x.node.(value); up = up.parent {
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

// Validate reports any errors, recursively. The returned error may be an
// errors.List reporting multiple errors, where the total number of errors
// reported may be less than the actual number.
func (v Value) Validate() error {
	list := errors.List{}
	v.Walk(func(v Value) bool {
		if err := v.Err(); err != nil {
			list.Add(err)
			if len(list) > 50 {
				return false // mostly to avoid some hypothetical cycle issue
			}
		}
		return true
	}, nil)
	if len(list) > 0 {
		list.Sort()
		// list.RemoveMultiples() // TODO: use RemoveMultiples when it is fixed
		return list
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
