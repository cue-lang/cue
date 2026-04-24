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

package export

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/core/adt"
)

// boundSimplifier collapses BasicType and BoundValue conjuncts into a compact
// int/uint prefix plus the remaining bounds.
type boundSimplifier struct {
	e *exporter

	isInt  bool
	min    *adt.BoundValue
	minNum *adt.Num
	max    *adt.BoundValue
	maxNum *adt.Num
}

func (s *boundSimplifier) add(v adt.Value) (used bool) {
	switch x := v.(type) {
	case *adt.BasicType:
		switch x.K & adt.ScalarKinds {
		case adt.IntKind:
			s.isInt = true
			return true
		}

	case *adt.BoundValue:
		if adt.IsConcrete(x.Value) && x.Kind() == adt.IntKind {
			s.isInt = true
		}
		switch x.Op {
		case adt.GreaterThanOp:
			if n, ok := x.Value.(*adt.Num); ok {
				if s.min == nil || s.minNum.X.Cmp(&n.X) != 1 {
					s.min = x
					s.minNum = n
				}
				return true
			}

		case adt.GreaterEqualOp:
			if n, ok := x.Value.(*adt.Num); ok {
				if s.min == nil || s.minNum.X.Cmp(&n.X) == -1 {
					s.min = x
					s.minNum = n
				}
				return true
			}

		case adt.LessThanOp:
			if n, ok := x.Value.(*adt.Num); ok {
				if s.max == nil || s.maxNum.X.Cmp(&n.X) != -1 {
					s.max = x
					s.maxNum = n
				}
				return true
			}

		case adt.LessEqualOp:
			if n, ok := x.Value.(*adt.Num); ok {
				if s.max == nil || s.maxNum.X.Cmp(&n.X) == 1 {
					s.max = x
					s.maxNum = n
				}
				return true
			}
		}
	}

	return false
}

func (s *boundSimplifier) expr(ctx *adt.OpContext) (e ast.Expr) {
	if s.min == nil || s.max == nil {
		return nil
	}
	if s.isInt {
		if sign := s.minNum.X.Sign(); sign == -1 {
			e = ast.NewIdent("int")
		} else {
			e = ast.NewIdent("uint")
			if sign == 0 && s.min.Op == adt.GreaterEqualOp {
				s.min = nil
			}
		}
	}
	if s.min != nil {
		e = wrapBin(e, s.e.expr(nil, s.min), adt.AndOp)
	}
	if s.max != nil {
		e = wrapBin(e, s.e.expr(nil, s.max), adt.AndOp)
	}
	return e
}

func wrapBin(a, b ast.Expr, op adt.Op) ast.Expr {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return ast.NewBinExpr(op.Token(), a, b)
}
