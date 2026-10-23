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

package compile

import (
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/core/adt"
)

// experimentPredeclared resolves identifiers that are predeclared only
// under an experiment the file enables; it returns nil otherwise, so
// that files without the experiment keep today's "reference not found"
// error.
func (c *compiler) experimentPredeclared(n *ast.Ident) adt.Expr {
	switch n.Name {
	case "validator", "__validator":
		if c.experiments.Functions {
			return validatorBuiltin
		}
	}
	return nil
}

func predeclared(n *ast.Ident) adt.Expr {
	// TODO: consider supporting GraphQL-style names:
	// String, Bytes, Boolean, Integer, Number.
	// These names will not conflict with idiomatic camel-case JSON.
	switch n.Name {
	case "string", "__string":
		return &adt.BasicType{Src: n, K: adt.StringKind}
	case "bytes", "__bytes":
		return &adt.BasicType{Src: n, K: adt.BytesKind}
	case "bool", "__bool":
		return &adt.BasicType{Src: n, K: adt.BoolKind}
	case "int", "__int":
		return &adt.BasicType{Src: n, K: adt.IntKind}
	case "float", "__float":
		return &adt.BasicType{Src: n, K: adt.FloatKind}
	case "number", "__number":
		return &adt.BasicType{Src: n, K: adt.NumberKind}

	case "error", "__error":
		return errorBuiltin
	case "len", "__len":
		return lenBuiltin
	case "close", "__close":
		return closeBuiltin
	case "__closeAll":
		return closeAllBuiltin
	case "__reclose":
		return recloseBuiltin
	case "matchIf", "__matchIf":
		return matchIfBuiltin
	case "matchN", "__matchN":
		return matchNBuiltin
	case "and", "__and":
		return andBuiltin
	case "or", "__or":
		return orBuiltin
	case "div", "__div":
		return divBuiltin
	case "mod", "__mod":
		return modBuiltin
	case "quo", "__quo":
		return quoBuiltin
	case "rem", "__rem":
		return remBuiltin

	case "self", "__self":
		// UpCount of 1 gets resolved to relNode(1)
		return &adt.ValueReference{Src: n, UpCount: 1}

	case "__no_sharing":
		return adt.NoShareSentinel

	case "__test_experiment":
		return testExperiment
	}

	if r := LookupRange(n.Name); r != nil {
		return r
	}

	return nil
}

// LookupRange returns a CUE expression for the given predeclared identifier
// representing a range, such as uint8, int128, and float64, or nil if there
// is none. Like other predeclared identifiers, a range may carry a "__" prefix.
func LookupRange(name string) adt.Expr {
	return predefinedRanges()[strings.TrimPrefix(name, "__")]
}

// Predeclared returns the adt expression for a predeclared identifier
// such as "len", "and", or "int", or nil if name is not predeclared.
// It is used to reconstruct references to builtins that were resolved
// at compile time.
func Predeclared(name string) adt.Expr {
	return predeclared(&ast.Ident{Name: name})
}

// predefinedRanges returns the expressions for [BuiltinRanges].
// They are built on first use, as building them is costly at init time.
var predefinedRanges = sync.OnceValue(func() map[string]adt.Expr {
	m := make(map[string]adt.Expr)
	for _, r := range builtinRanges() {
		m[r.Name] = r.expr()
	}
	return m
})
