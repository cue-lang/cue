// Copyright 2019 CUE Authors
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

package jsonschema

import (
	"math/big"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// Numeric constraints

func constraintExclusiveMaximum(key string, n cue.Value, s *state) {
	if n.Kind() == cue.BoolKind {
		s.exclusiveMax = true
		return
	}
	s.add(n, numType, &ast.UnaryExpr{Op: token.LSS, X: s.number(n)})
}
func constraintExclusiveMinimum(key string, n cue.Value, s *state) {
	if n.Kind() == cue.BoolKind {
		s.exclusiveMin = true
		return
	}
	s.add(n, numType, &ast.UnaryExpr{Op: token.GTR, X: s.number(n)})
}

func constraintMinimum(key string, n cue.Value, s *state) {
	op := token.GEQ
	if s.exclusiveMin {
		op = token.GTR
	}
	s.add(n, numType, &ast.UnaryExpr{Op: op, X: s.number(n)})
}

func constraintMaximum(key string, n cue.Value, s *state) {
	op := token.LEQ
	if s.exclusiveMax {
		op = token.LSS
	}
	s.add(n, numType, &ast.UnaryExpr{Op: op, X: s.number(n)})
}

func constraintMultipleOf(key string, n cue.Value, s *state) {
	multiple := s.number(n)
	var x big.Int
	_, _ = n.MantExp(&x)
	if x.Cmp(big.NewInt(0)) != 1 {
		s.errf(n, `"multipleOf" value must be > 0; found %s`, n)
	}
	math := s.addImport(n, "math")
	s.add(n, numType, ast.NewCall(ast.NewSel(math, "MultipleOf"), multiple))
}
