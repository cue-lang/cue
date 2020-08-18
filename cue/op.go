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
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

// Op indicates the operation at the top of an expression tree of the expression
// use to evaluate a value.
type Op = adt.Op

// Values of Op.
const (
	NoOp = adt.NoOp

	AndOp = adt.AndOp
	OrOp  = adt.OrOp

	SelectorOp = adt.SelectorOp
	IndexOp    = adt.IndexOp
	SliceOp    = adt.SliceOp
	CallOp     = adt.CallOp

	BooleanAndOp = adt.BoolAndOp
	BooleanOrOp  = adt.BoolOrOp

	EqualOp            = adt.EqualOp
	NotOp              = adt.NotOp
	NotEqualOp         = adt.NotEqualOp
	LessThanOp         = adt.LessThanOp
	LessThanEqualOp    = adt.LessEqualOp
	GreaterThanOp      = adt.GreaterThanOp
	GreaterThanEqualOp = adt.GreaterEqualOp

	RegexMatchOp    = adt.MatchOp
	NotRegexMatchOp = adt.NotMatchOp

	AddOp           = adt.AddOp
	SubtractOp      = adt.SubtractOp
	MultiplyOp      = adt.MultiplyOp
	FloatQuotientOp = adt.FloatQuotientOp
	IntQuotientOp   = adt.IntQuotientOp
	IntRemainderOp  = adt.IntRemainderOp
	IntDivideOp     = adt.IntDivideOp
	IntModuloOp     = adt.IntModuloOp

	InterpolationOp = adt.InterpolationOp
)

// isCmp reports whether an op is a comparator.
func (op op) isCmp() bool {
	return opEql <= op && op <= opGeq
}

func (op op) unifyType() (unchecked, ok bool) {
	if op == opUnifyUnchecked {
		return true, true
	}
	return false, op == opUnify
}

type op uint16

const (
	opUnknown op = iota

	opUnify
	opUnifyUnchecked
	opDisjunction

	opLand
	opLor
	opNot

	opEql
	opNeq
	opMat
	opNMat

	opLss
	opGtr
	opLeq
	opGeq

	opAdd
	opSub
	opMul
	opQuo
	opRem

	opIDiv
	opIMod
	opIQuo
	opIRem
)

var opStrings = []string{
	opUnknown: "??",

	opUnify: "&",
	// opUnifyUnchecked is internal only. Syntactically this is
	// represented as embedding.
	opUnifyUnchecked: "&!",
	opDisjunction:    "|",

	opLand: "&&",
	opLor:  "||",
	opNot:  "!",

	opEql:  "==",
	opNeq:  "!=",
	opMat:  "=~",
	opNMat: "!~",

	opLss: "<",
	opGtr: ">",
	opLeq: "<=",
	opGeq: ">=",

	opAdd: "+",
	opSub: "-",
	opMul: "*",
	opQuo: "/",

	opIDiv: "div",
	opIMod: "mod",
	opIQuo: "quo",
	opIRem: "rem",
}

func (op op) String() string { return opStrings[op] }

var tokenMap = map[token.Token]op{
	token.OR:  opDisjunction, // |
	token.AND: opUnify,       // &

	token.ADD: opAdd, // +
	token.SUB: opSub, // -
	token.MUL: opMul, // *
	token.QUO: opQuo, // /

	token.IDIV: opIDiv, // div
	token.IMOD: opIMod, // mod
	token.IQUO: opIQuo, // quo
	token.IREM: opIRem, // rem

	token.LAND: opLand, // &&
	token.LOR:  opLor,  // ||

	token.EQL: opEql, // ==
	token.LSS: opLss, // <
	token.GTR: opGtr, // >
	token.NOT: opNot, // !

	token.NEQ:  opNeq,  // !=
	token.LEQ:  opLeq,  // <=
	token.GEQ:  opGeq,  // >=
	token.MAT:  opMat,  // =~
	token.NMAT: opNMat, // !~
}

var opMap = map[op]token.Token{}

func init() {
	for t, o := range tokenMap {
		opMap[o] = t
	}
}
