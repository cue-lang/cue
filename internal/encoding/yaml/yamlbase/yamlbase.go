// Copyright 2026 CUE Authors
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

// Package yamlbase holds the parts of converting between YAML and CUE
// which do not depend on a YAML implementation, shared by the
// go.yaml.in/yaml/v3 based [cuelang.org/go/internal/encoding/yaml]
// and the goccy/go-yaml based
// [cuelang.org/go/internal/encoding/yaml/goccy].
package yamlbase

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// ComposeFile assembles decoded YAML document expressions into a CUE
// file: the fields of a single mapping document become the file's
// declarations, any other single document is embedded, and a document
// stream becomes an embedded list.
func ComposeFile(filename string, docs []ast.Expr) *ast.File {
	f := &ast.File{Filename: filename}
	switch len(docs) {
	case 0:
	case 1:
		switch x := docs[0].(type) {
		case *ast.StructLit:
			f.Decls = x.Elts
		default:
			f.Decls = []ast.Decl{&ast.EmbedDecl{Expr: x}}
		}
	default:
		f.Decls = []ast.Decl{&ast.EmbedDecl{Expr: &ast.ListLit{Elts: docs}}}
	}
	return f
}

// SpecialFloats maps the plain scalar spellings of infinities and NaN
// to their decoded forms.
var SpecialFloats = map[string]string{
	".inf": "+Inf", ".Inf": "+Inf", ".INF": "+Inf",
	"+.inf": "+Inf", "+.Inf": "+Inf", "+.INF": "+Inf",
	"-.inf": "-Inf", "-.Inf": "-Inf", "-.INF": "-Inf",
	".nan": "NaN", ".NaN": "NaN", ".NAN": "NaN",
}

// IntExpr converts a YAML integer literal to a CUE expression.
// If YAML accepted an invalid integer, we pass it along to ensure
// CUE will fail.
func IntExpr(pos token.Pos, value string) (ast.Expr, error) {
	value = YAML11OctalToCUE(value)
	var info literal.NumInfo
	// We make the assumption that any valid YAML integer literal will be a valid
	// CUE integer literal as well, with the only exception of octal numbers above.
	if err := literal.ParseNum(value, &info); err != nil {
		return nil, fmt.Errorf("cannot decode %q as %s: %v", value, "!!int", err)
	} else if !info.IsInt() {
		return nil, fmt.Errorf("cannot decode %q as %s: not a literal number", value, "!!int")
	}
	return MakeNum(pos, value, token.INT), nil
}

// FloatExpr converts a YAML float literal to a CUE expression. When the
// literal has an explicit or implied !!float tag but looks like an
// integer, it is unified with "number" to record the fact that it was
// represented as a float. Don't unify with float, as `float & 123` is
// invalid, and there's no need to forbid representing the number as an
// integer either.
func FloatExpr(pos token.Pos, value string, explicitTag bool) (ast.Expr, error) {
	// Convert YAML 1.1 octal, like with integers.
	value = YAML11OctalToCUE(value)
	var info literal.NumInfo
	// We make the assumption that any valid YAML float literal will be a valid
	// CUE float literal as well, with the only exception of Inf/NaN and octal above.
	// Note that `!!float 123` is allowed.
	if err := literal.ParseNum(value, &info); err != nil {
		return nil, fmt.Errorf("cannot decode %q as %s: %v", value, "!!float", err)
	}
	if explicitTag && !strings.ContainsAny(value, ".eEiInN") {
		// TODO: number(v) when we have conversions
		//
		// TODO(mvdan): would it be better to do turn `!!float 123` into `123.0`
		// rather than `number & 123`? Note that `float & 123` is an error.
		return &ast.BinaryExpr{
			X:     &ast.Ident{NamePos: pos, Name: "number"},
			OpPos: pos.WithRel(token.Blank),
			Op:    token.AND,
			Y:     MakeNum(pos.WithRel(token.Blank), value, token.INT),
		}, nil
	}
	return MakeNum(pos, value, token.FLOAT), nil
}

// MakeNum returns a CUE number literal of the given kind for val,
// wrapped in a unary minus if val is negative.
func MakeNum(pos token.Pos, val string, kind token.Token) (expr ast.Expr) {
	val, negative := strings.CutPrefix(val, "-")
	expr = &ast.BasicLit{
		ValuePos: pos,
		Kind:     kind,
		Value:    val,
	}
	if negative {
		expr = &ast.UnaryExpr{
			OpPos: pos,
			Op:    token.SUB,
			X:     expr,
		}
	}
	return expr
}

// YAML11OctalToCUE converts a YAML 1.1 octal literal like 0777 or
// -0777 to CUE form. Other values, such as floats with a leading zero
// digit like 01289.5, are returned unchanged.
func YAML11OctalToCUE(value string) string {
	sign, digits := "", value
	if len(digits) > 0 && (digits[0] == '+' || digits[0] == '-') {
		sign, digits = digits[:1], digits[1:]
	}
	rest, ok := strings.CutPrefix(digits, "0")
	if !ok || rest == "" {
		return value
	}
	for _, c := range rest {
		if (c < '0' || c > '7') && c != '_' {
			return value
		}
	}
	return sign + "0o" + rest
}

// IsYAML11NonString reports whether YAML 1.1 decoders read the plain
// scalar str as a value other than a string, such as a boolean, a
// number, or a timestamp, or fail to decode it.
func IsYAML11NonString(str string) bool {
	if legacyStrings[str] {
		return true
	}
	// The regular expressions can only match strings starting with one
	// of these bytes; skip them otherwise.
	return str != "" && strings.IndexByte("-+0123456789:. \t", str[0]) >= 0 &&
		(useQuote().MatchString(str) || rxYAML11Number().MatchString(str))
}

// rxYAML11Number matches the integers and floats of YAML 1.1, using the
// regular expressions of PyYAML, its reference implementation.
var rxYAML11Number = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^(?:` +
		// Integers.
		`[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|` +
		`[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+|` +
		// Floats.
		`[-+]?[0-9][0-9_]*\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9_]+(?:[eE][-+][0-9]+)?|` +
		`[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN)` +
		`)$`)
})

// This regular expression conservatively matches any date, time string,
// or base60 float.
var useQuote = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^[\-+0-9:\. \t]+([-:]|[tT])[\-+0-9:\. \t]+[zZ]?$`)
})

// legacyStrings contains a map of fixed strings with special meaning for any
// type in the YAML Tag registry (https://yaml.org/type/index.html) as used
// in YAML 1.1.
//
// These strings are always double quoted upon export to allow for
// backward compatibility with YAML 1.1 parsers.
var legacyStrings = map[string]bool{
	"y":     true,
	"Y":     true,
	"yes":   true,
	"Yes":   true,
	"YES":   true,
	"n":     true,
	"N":     true,
	"t":     true,
	"T":     true,
	"f":     true,
	"F":     true,
	"no":    true,
	"No":    true,
	"NO":    true,
	"true":  true,
	"True":  true,
	"TRUE":  true,
	"false": true,
	"False": true,
	"FALSE": true,
	"on":    true,
	"On":    true,
	"ON":    true,
	"off":   true,
	"Off":   true,
	"OFF":   true,

	// The default value key, which PyYAML fails to decode.
	"=": true,

	// Non-standard.
	".Nan": true,
}

// MaxAliasNodes bounds the nodes produced by alias expansion. Each alias
// reference expands into a fresh subtree, so wide fan-out can produce a
// tree far larger than the input. Realistic documents use little or no
// aliasing, so this limit is generous.
const MaxAliasNodes = 50_000

// LabelStr returns the name of a field label, or "" if it is neither
// an identifier nor a string.
func LabelStr(l ast.Label) string {
	switch l := l.(type) {
	case *ast.Ident:
		return l.Name
	case *ast.BasicLit:
		s, _ := literal.Unquote(l.Value)
		return s
	}
	return ""
}
