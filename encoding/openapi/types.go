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

	"time.Time":                  "dateTime",
	"time.Time ()":               "dateTime",
	`time.Format ("2006-01-02")`: "date",

	// TODO: if a format is more strict (e.g. using zeros instead of nines
	// for fractional seconds), we could still use this as an approximation.
	`time.Format ("2006-01-02T15:04:05.999999999Z07:00")`: "dateTime",

	// TODO:  password.
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
	if s, ok := cueToOpenAPI[string(b)]; ok {
		return s
	}
	s := fmt.Sprint(v)
	return cueToOpenAPI[s]
}

func simplify(b *builder, t *OrderedMap) {
	if b.format == "" {
		return
	}
	switch b.typ {
	case "number", "integer":
		simplifyNumber(t, b.format)
	}
}

func simplifyNumber(t *OrderedMap, format string) string {
	pairs := t.kvs
	k := 0
	for i, kv := range pairs {
		switch kv.Key {
		case "minimum":
			if decimalEqual(minMap[format], kv.Value) {
				continue
			}
		case "maximum":
			if decimalEqual(maxMap[format], kv.Value) {
				continue
			}
		}
		pairs[i] = pairs[k]
		k++
	}
	t.kvs = pairs[:k]
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
