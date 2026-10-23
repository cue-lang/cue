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

package openapi

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

// See https://github.com/OAI/OpenAPI-Specification/blob/main/versions/3.0.0.md#data-types
var cueToOpenAPI = map[string]string{
	"int32": "int32",
	"int64": "int64",

	"float64": "double",
	"float32": "float",

	"bytes": "binary",

	"time.Time()":                "date-time",
	"time.Time":                  "date-time",
	`time.Format ("2006-01-02")`: "date",

	// TODO: if a format is more strict (e.g. using zeros instead of nines
	// for fractional seconds), we could still use this as an approximation.
	`time.Format ("2006-01-02T15:04:05.999999999Z07:00")`: "date-time",

	// TODO:  password.
}

func extractFormat(v cue.Value) string {
	switch k := v.IncompleteKind(); {
	case k&cue.NumberKind != 0, k&cue.StringKind != 0, k&cue.BytesKind != 0:
	default:
		return ""
	}
	var arg string

	if op, a := v.Expr(); op == cue.CallOp {
		v = a[0]
		if len(a) == 2 {
			arg = fmt.Sprintf(" (%v)", a[1].Eval())
		}
	}

	expr := fmt.Sprint(v.Eval(), arg)

	if s, ok := cueToOpenAPI[expr]; ok {
		return s
	}
	s := fmt.Sprint(v)
	return cueToOpenAPI[s]
}

func getDeprecated(v cue.Value) bool {
	// only looking at protobuf attribute for now.
	a := v.Attribute("protobuf")
	r, _ := a.Flag(1, "deprecated")
	return r
}

func simplify(b *builder, t *ast.StructLit) {
	if b.format == "" {
		return
	}
	switch b.typ {
	case "number", "integer":
		simplifyNumber(t, b.format)
	}
}

func simplifyNumber(t *ast.StructLit, format string) string {
	var lo, hi *adt.Num
	for _, r := range compile.BuiltinRanges() {
		if cueToOpenAPI[r.Name] == format {
			lo, hi = r.Lo, r.Hi
			break
		}
	}
	fields := t.Elts
	k := 0
	for i, d := range fields {
		switch label(d) {
		case "minimum":
			if decimalEqual(lo, value(d)) {
				continue
			}
		case "maximum":
			if decimalEqual(hi, value(d)) {
				continue
			}
		}
		fields[k] = fields[i]
		k++
	}
	t.Elts = fields[:k]
	return format
}

func decimalEqual(bound *adt.Num, v ast.Expr) bool {
	if bound == nil {
		return false
	}
	lit, ok := v.(*ast.BasicLit)
	if !ok || (lit.Kind != token.INT && lit.Kind != token.FLOAT) {
		return false
	}
	n := literal.NumInfo{}
	if literal.ParseNum(lit.Value, &n) != nil {
		return false
	}
	var b apd.Decimal
	if n.Decimal(&b) != nil {
		return false
	}
	return bound.X.Cmp(&b) == 0
}
