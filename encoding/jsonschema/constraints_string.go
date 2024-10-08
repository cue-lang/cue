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
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// String constraints

func constraintContentEncoding(key string, n cue.Value, s *state) {
	// TODO: only mark as used if it generates something.
	// 7bit, 8bit, binary, quoted-printable and base64.
	// RFC 2054, part 6.1.
	// https://tools.ietf.org/html/rfc2045
	// TODO: at least handle bytes.
}

func constraintContentMediaType(key string, n cue.Value, s *state) {
	// TODO: only mark as used if it generates something.
}

func constraintMaxLength(key string, n cue.Value, s *state) {
	max := s.uint(n)
	strings := s.addImport(n, "strings")
	s.add(n, stringType, ast.NewCall(ast.NewSel(strings, "MaxRunes"), max))
}

func constraintMinLength(key string, n cue.Value, s *state) {
	min := s.uint(n)
	strings := s.addImport(n, "strings")
	s.add(n, stringType, ast.NewCall(ast.NewSel(strings, "MinRunes"), min))
}

func constraintPattern(key string, n cue.Value, s *state) {
	str, ok := s.regexpValue(n)
	if !ok {
		return
	}
	s.add(n, stringType, &ast.UnaryExpr{Op: token.MAT, X: str})
}
