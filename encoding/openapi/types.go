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
	"github.com/cockroachdb/apd/v2"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/format"
)

// See https://github.com/OAI/OpenAPI-Specification/blob/master/versions/3.0.0.md#format
var cueToOpenAPI = map[string]string{
	"int32": "int32",
	"int64": "int64",

	"float64": "double",
	"float32": "float",

	"string": "string",
	"bytes":  "binary",

	// TODO: date, date-time, password.
}

func extractFormat(v cue.Value) string {
	switch k := v.IncompleteKind(); {
	case k&cue.NumberKind != 0, k&cue.StringKind != 0, k&cue.BytesKind != 0:
	default:
		return ""
	}
	b, err := format.Node(v.Syntax())
	if err != nil {
		return ""
	}
	return cueToOpenAPI[string(b)]
}

func simplify(b *builder, t *orderedMap) {
	if b.format == "" {
		return
	}
	switch b.typ {
	case "number", "integer":
		simplifyNumber(t, b.format)
	}
}

func simplifyNumber(t *orderedMap, format string) string {
	pairs := *t
	k := 0
	for i, kv := range pairs {
		switch kv.key {
		case "minimum":
			if decimalEqual(minMap[format], kv.value) {
				continue
			}
		case "maximum":
			if decimalEqual(maxMap[format], kv.value) {
				continue
			}
		}
		pairs[i] = pairs[k]
		k++
	}
	*t = pairs[:k]
	return format
}

func decimalEqual(d *decimal, v interface{}) bool {
	if d == nil {
		return false
	}
	b, ok := v.(*decimal)
	if !ok {
		return false
	}
	return d.Cmp(b.Decimal) == 0
}

type decimal struct {
	*apd.Decimal
}

func (d *decimal) MarshalJSON() (b []byte, err error) {
	return d.MarshalText()
}

func mustDecimal(s string) *decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return &decimal{d}
}

var (
	minMap = map[string]*decimal{
		"int32":  mustDecimal("-2147483648"),
		"int64":  mustDecimal("-9223372036854775808"),
		"float":  mustDecimal("-3.40282346638528859811704183484516925440e+38"),
		"double": mustDecimal("-1.797693134862315708145274237317043567981e+308"),
	}
	maxMap = map[string]*decimal{
		"int32":  mustDecimal("2147483647"),
		"int64":  mustDecimal("9223372036854775807"),
		"float":  mustDecimal("+3.40282346638528859811704183484516925440e+38"),
		"double": mustDecimal("+1.797693134862315708145274237317043567981e+308"),
	}
)
