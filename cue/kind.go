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
	"fmt"

	"cuelang.org/go/cue/token"
)

func unifyType(a, b kind) kind {
	const mask = topKind
	isRef := (a &^ mask) | (b &^ mask)
	return isRef | (a & b)
}

type kind uint16

const (
	unknownKind kind = (1 << iota)
	nullKind
	boolKind
	intKind
	floatKind
	stringKind
	bytesKind
	durationKind
	listKind
	structKind

	lambdaKind
	// customKind

	// nonGround means that a value is not specific enough to be emitted.
	// Structs and lists are indicated as ground even when their values are not.
	nonGround

	// TODO: distinguish beteween nonGround and disjunctions?

	// a referenceKind is typically top and nonGround, but is indicated with an
	// additional bit. If reference is set and nonGround is not, it is possible
	// to move the reference to an assertion clause.
	referenceKind

	atomKind     = (listKind - 1) &^ unknownKind
	concreteKind = (lambdaKind - 1) &^ unknownKind

	// doneKind indicates a value can not further develop on its own (i.e. not a
	// reference). If doneKind is not set, but the result is ground, it
	// typically possible to hoist the reference out of a unification operation.

	// For rational numbers, typically both intKind and floatKind are set,
	// unless the range is restricted by a root type.
	numKind = intKind | floatKind

	comparableKind = (listKind - 1) &^ unknownKind
	stringableKind = scalarKinds | stringKind
	topKind        = (referenceKind - 1) // all kinds, but not references
	typeKinds      = (nonGround - 1) &^ unknownKind
	okKinds        = typeKinds &^ bottomKind
	fixedKinds     = okKinds &^ (structKind | lambdaKind)
	scalarKinds    = numKind | durationKind

	bottomKind = 0
)

func isTop(v value) bool {
	return v.kind() == topKind
}

// isDone means that the value will not evaluate further.
func (k kind) isDone() bool        { return k&referenceKind == bottomKind }
func (k kind) hasReferences() bool { return k&referenceKind != bottomKind }
func (k kind) isGround() bool      { return k&nonGround == bottomKind }
func (k kind) isAtom() bool        { return k.isGround() && k&atomKind != bottomKind }
func (k kind) isAnyOf(of kind) bool {
	return k&of != bottomKind
}
func (k kind) stringable() bool {
	return k.isGround() && k&stringKind|scalarKinds != bottomKind
}

func (k kind) String() string {
	str := ""
	if k&topKind == topKind {
		str = "_"
		goto finalize
	}
	for i := kind(1); i < referenceKind; i <<= 1 {
		t := ""
		switch k & i {
		case bottomKind:
			continue
		case nullKind:
			t = "null"
		case boolKind:
			t = "bool"
		case intKind:
			if k&floatKind != 0 {
				t = "number"
			} else {
				t = "int"
			}
		case floatKind:
			if k&intKind != 0 {
				continue
			}
			t = "float"
		case stringKind:
			t = "string"
		case bytesKind:
			t = "bytes"
		case durationKind:
			t = "duration"
		case listKind:
			t = "list"
		case structKind:
			t = "struct"
		case lambdaKind:
			t = "lambda"
		case nonGround, referenceKind:
			continue
		default:
			t = fmt.Sprintf("<unknown> %x", int(i))
		}
		if str != "" {
			str += "|"
		}
		str += t
	}
finalize:
	if str == "" {
		return "_|_"
	}
	if k&nonGround != 0 {
		str = "(" + str + ")*"
	}
	if k&referenceKind != 0 {
		str = "&" + str
	}
	return str
}

type kindInfo struct {
	kind
	subsumes   []*kindInfo
	subsumedBy []*kindInfo
	unary      []token.Token
	binary     []token.Token
}

var toKindInfo = map[kind]*kindInfo{}

// matchBinOpKind returns the result kind of applying the given op to operands with
// the given kinds. The operation is disallowed if the return value is bottomKind. If
// the second return value is true, the operands should be swapped before evaluation.
//
// Evaluating binary expressions uses this to
// - fail incompatible operations early, even if the concrete types are
//   not known,
// - check the result type of unification,
//
// Secondary goals:
// - keep type compatibility mapped at a central place
// - reduce the amount op type switching.
// - simplifies testing
func matchBinOpKind(op op, a, b kind) (k kind, swap bool) {
	if op == opDisjunction {
		return a | b, false
	}
	u := unifyType(a, b)
	valBits := u & typeKinds
	catBits := u &^ typeKinds
	aGround := a&nonGround == 0
	bGround := b&nonGround == 0
	a = a & typeKinds
	b = b & typeKinds
	if valBits == bottomKind {
		k := nullKind
		switch op {
		case opEql, opNeq:
			fallthrough
		case opUnify:
			if a&nullKind != 0 {
				return k, false
			}
			if b&nullKind != 0 {
				return k, true
			}
			return bottomKind, false
		}
		if op == opMul {
			if a.isAnyOf(listKind|stringKind|bytesKind) && b.isAnyOf(intKind) {
				return a | catBits, false
			}
			if b.isAnyOf(listKind|stringKind|bytesKind) && a.isAnyOf(intKind) {
				return b | catBits, true
			}
		}
		// non-overlapping types
		if a&scalarKinds == 0 || b&scalarKinds == 0 {
			return bottomKind, false
		}
		// a and b have different numeric types.
		switch {
		case b.isAnyOf(durationKind):
			// a must be a numeric, non-duration type.
			if op == opMul {
				return durationKind | catBits, true
			}
		case a.isAnyOf(durationKind):
			if opIn(op, opMul, opQuo, opRem) {
				return durationKind | catBits, false
			}
		case op.isCmp():
			return boolKind, false
		}
		return bottomKind, false
	}
	switch {
	case a&nonGround == 0 && b&nonGround == 0:
		// both ground values: nothing to do

	case op != opUnify && op != opLand && op != opLor && op != opNeq:

	default:
		swap = aGround && !bGround
	}
	// a and b have overlapping types.
	switch op {
	case opUnify:
		// Increase likelihood of unification succeeding on first try.
		return u, swap

	case opLand, opLor:
		if u.isAnyOf(boolKind) {
			return boolKind | catBits, swap
		}
	case opEql, opNeq, opMat, opNMat:
		if u.isAnyOf(fixedKinds) {
			return boolKind | catBits, false
		}
	case opLss, opLeq, opGeq, opGtr:
		if u.isAnyOf(fixedKinds) {
			return boolKind | catBits, false
		}
	case opAdd:
		if u.isAnyOf(atomKind) {
			return u&(atomKind) | catBits, false
			// u&(durationKind|intKind)
		}
	case opSub:
		if u.isAnyOf(scalarKinds) {
			return u&scalarKinds | catBits, false
		}
	case opRem:
		if u.isAnyOf(floatKind) {
			return floatKind | catBits, false
		}
	case opQuo:
		if u.isAnyOf(floatKind) {
			return floatKind | catBits, false
		}
	case opIRem, opIMod:
		if u.isAnyOf(intKind) {
			return u&(intKind) | catBits, false
		}
	case opIQuo, opIDiv:
		if u.isAnyOf(intKind) {
			return intKind | catBits, false
		}
	case opMul:
		if u.isAnyOf(numKind) {
			return u&numKind | catBits, false
		}
	default:
		panic("unimplemented")
	}
	return bottomKind, false
}
