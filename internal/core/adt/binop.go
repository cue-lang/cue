// Copyright 2020 CUE Authors
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

package adt

import (
	"bytes"
	"strings"

	"cuelang.org/go/cue/token"
)

var checkConcrete = &ValidateConfig{
	Concrete: true,
	Final:    true,
}

// errOnDiffType is a special value that is used as a source to BinOp to
// indicate that the operation is not supported for the operands of different
// kinds.
var errOnDiffType = &UnaryExpr{}

// BinOp handles all operations except AndOp and OrOp. This includes processing
// unary comparators such as '<4' and '=~"foo"'.
//
// The node argument is the adt node corresponding to the binary expression. It
// is used to determine the source position of the operation, which in turn is
// used to determine the experiment context.
//
// BinOp returns nil if not both left and right are concrete.
func BinOp(c *OpContext, node Node, op Op, left, right Value) Value {
	var p token.Pos
	if node != nil {
		if src := node.Source(); src != nil {
			p = src.Pos()
		}
	}
	leftKind := left.Kind()
	rightKind := right.Kind()

	if err := validateValue(c, left, checkConcrete); err != nil {
		const msg = "invalid left-hand value to '%s' (type %s): %v"
		// TODO: Wrap bottom instead of using NewErrf?
		b := c.NewErrf(msg, op, left.Kind(), err.Err)
		b.Code = err.Code
		return b
	}
	if err := validateValue(c, right, checkConcrete); err != nil {
		const msg = "invalid right-hand value to '%s' (type %s): %v"
		b := c.NewErrf(msg, op, left.Kind(), err.Err)
		b.Code = err.Code
		return b
	}

	if err := CombineErrors(c.src, left, right); err != nil {
		return err
	}

	switch op {
	case EqualOp:
		switch {
		case leftKind&NumberKind != 0 && rightKind&NumberKind != 0:
			return cmpTonode(c, op, c.Num(left, op).X.Cmp(&c.Num(right, op).X))

		case leftKind != rightKind:
			if p.Experiment().StructCmp ||
				// compatibility with !structCmp:
				leftKind == NullKind || rightKind == NullKind {
				return c.newBool(false)
			}

		case leftKind == NullKind:
			return c.newBool(true)

		case leftKind == BoolKind:
			return c.newBool(c.BoolValue(left) == c.BoolValue(right))

		case leftKind == StringKind:
			// normalize?
			return cmpTonode(c, op, strings.Compare(c.StringValue(left), c.StringValue(right)))

		case leftKind == BytesKind:
			return cmpTonode(c, op, bytes.Compare(c.bytesValue(left, op), c.bytesValue(right, op)))

		case leftKind == ListKind:
			return c.newBool(Equal(c, left, right, RegularOnly|IgnoreOptional))

		case !p.Experiment().StructCmp:
		case leftKind == StructKind:
			return c.newBool(Equal(c, left, right, RegularOnly|IgnoreOptional))
		}

	case NotEqualOp:
		switch {
		case leftKind&NumberKind != 0 && rightKind&NumberKind != 0:
			return cmpTonode(c, op, c.Num(left, op).X.Cmp(&c.Num(right, op).X))

		case leftKind != rightKind:
			if p.Experiment().StructCmp ||
				// compatibility with !structCmp:
				leftKind == NullKind ||
				rightKind == NullKind {
				return c.newBool(true)
			}

		case leftKind == NullKind:
			return c.newBool(false)

		case leftKind == BoolKind:
			return c.newBool(c.boolValue(left, op) != c.boolValue(right, op))

		case leftKind == StringKind:
			// normalize?
			return cmpTonode(c, op, strings.Compare(c.StringValue(left), c.StringValue(right)))

		case leftKind == BytesKind:
			return cmpTonode(c, op, bytes.Compare(c.bytesValue(left, op), c.bytesValue(right, op)))

		case leftKind == ListKind:
			return c.newBool(!Equal(c, left, right, RegularOnly|IgnoreOptional))

		case !p.Experiment().StructCmp:
		case leftKind == StructKind:
			return c.newBool(!Equal(c, left, right, RegularOnly|IgnoreOptional))
		}

	case LessThanOp, LessEqualOp, GreaterEqualOp, GreaterThanOp:
		switch {
		case leftKind == StringKind && rightKind == StringKind:
			// normalize?
			return cmpTonode(c, op, strings.Compare(c.stringValue(left, op), c.stringValue(right, op)))

		case leftKind == BytesKind && rightKind == BytesKind:
			return cmpTonode(c, op, bytes.Compare(c.bytesValue(left, op), c.bytesValue(right, op)))

		case leftKind&NumberKind != 0 && rightKind&NumberKind != 0:
			// n := c.newNum(left, right)
			return cmpTonode(c, op, c.Num(left, op).X.Cmp(&c.Num(right, op).X))
		}

	case BoolAndOp:
		return c.newBool(c.boolValue(left, op) && c.boolValue(right, op))

	case BoolOrOp:
		return c.newBool(c.boolValue(left, op) || c.boolValue(right, op))

	case MatchOp:
		// if y.re == nil {
		// 	// This really should not happen, but leave in for safety.
		// 	b, err := Regexp.MatchString(str, x.str)
		// 	if err != nil {
		// 		return c.Errf(Src, "error parsing Regexp: %v", err)
		// 	}
		// 	return boolTonode(Src, b)
		// }
		return c.newBool(c.regexp(right).MatchString(c.stringValue(left, op)))

	case NotMatchOp:
		return c.newBool(!c.regexp(right).MatchString(c.stringValue(left, op)))

	case AddOp:
		switch {
		case leftKind&NumberKind != 0 && rightKind&NumberKind != 0:
			return c.Add(c.Num(left, op), c.Num(right, op))

		case leftKind == StringKind && rightKind == StringKind:
			return c.NewString(c.StringValue(left) + c.StringValue(right))

		case leftKind == BytesKind && rightKind == BytesKind:
			ba := c.bytesValue(left, op)
			bb := c.bytesValue(right, op)
			b := make([]byte, len(ba)+len(bb))
			copy(b, ba)
			copy(b[len(ba):], bb)
			return c.newBytes(b)

		case leftKind == ListKind && rightKind == ListKind:
			return c.NewErrf("Addition of lists is superseded by list.Concat; see https://cuelang.org/e/v0.11-list-arithmetic")
		}

	case SubtractOp:
		return c.Sub(c.Num(left, op), c.Num(right, op))

	case MultiplyOp:
		switch {
		// float
		case leftKind&NumberKind != 0 && rightKind&NumberKind != 0:
			return c.Mul(c.Num(left, op), c.Num(right, op))

		case leftKind == StringKind && rightKind == IntKind:
			const as = "string multiplication"
			return c.NewString(strings.Repeat(c.stringValue(left, as), int(c.uint64(right, as))))

		case leftKind == IntKind && rightKind == StringKind:
			const as = "string multiplication"
			return c.NewString(strings.Repeat(c.stringValue(right, as), int(c.uint64(left, as))))

		case leftKind == BytesKind && rightKind == IntKind:
			const as = "bytes multiplication"
			return c.newBytes(bytes.Repeat(c.bytesValue(left, as), int(c.uint64(right, as))))

		case leftKind == IntKind && rightKind == BytesKind:
			const as = "bytes multiplication"
			return c.newBytes(bytes.Repeat(c.bytesValue(right, as), int(c.uint64(left, as))))

		case leftKind == IntKind && rightKind == ListKind:
			fallthrough
		case leftKind == ListKind && rightKind == IntKind:
			return c.NewErrf("Multiplication of lists is superseded by list.Repeat; see https://cuelang.org/e/v0.11-list-arithmetic")
		}

	case FloatQuotientOp:
		if leftKind&NumberKind != 0 && rightKind&NumberKind != 0 {
			return c.Quo(c.Num(left, op), c.Num(right, op))
		}

	case IntDivideOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			return c.IntDiv(c.Num(left, op), c.Num(right, op))
		}

	case IntModuloOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			return c.IntMod(c.Num(left, op), c.Num(right, op))
		}

	case IntQuotientOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			return c.IntQuo(c.Num(left, op), c.Num(right, op))
		}

	case IntRemainderOp:
		if leftKind&IntKind != 0 && rightKind&IntKind != 0 {
			return c.IntRem(c.Num(left, op), c.Num(right, op))
		}
	}

	return c.NewErrf("invalid operands %s and %s to '%s' (type %s and %s)",
		left, right, op, left.Kind(), right.Kind())
}

func cmpTonode(c *OpContext, op Op, r int) Value {
	result := false
	switch op {
	case LessThanOp:
		result = r == -1
	case LessEqualOp:
		result = r != 1
	case EqualOp, AndOp:
		result = r == 0
	case NotEqualOp:
		result = r != 0
	case GreaterEqualOp:
		result = r != -1
	case GreaterThanOp:
		result = r == 1
	}
	return c.newBool(result)
}
