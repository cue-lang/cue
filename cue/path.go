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

package cue

import (
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"github.com/cockroachdb/apd/v2"
)

// A Selector is a component of a path.
type Selector struct {
	sel selector
}

// String reports the CUE representation of a selector.
func (sel Selector) String() string {
	return sel.sel.String()
}

type selector interface {
	String() string

	feature(ctx adt.Runtime) adt.Feature
	kind() adt.FeatureType
}

// A Path is series of selectors to query a CUE value.
type Path struct {
	path []Selector
}

// MakePath creates a Path from a sequence of selectors.
func MakePath(selectors ...Selector) Path {
	return Path{path: selectors}
}

// ParsePath parses a CUE expression into a Path. Any error resulting from
// this conversion can be obtained by calling Err on the result.
func ParsePath(s string) Path {
	expr, err := parser.ParseExpr("", s)
	if err != nil {
		return MakePath(Selector{pathError{errors.Promote(err, "invalid path")}})
	}

	return Path{path: toSelectors(expr)}
}

// Selectors reports the individual selectors of a path.
func (p Path) Selectors() []Selector {
	return p.path
}

// String reports the CUE representation of p.
func (p Path) String() string {
	if err := p.Err(); err != nil {
		return "_|_"
	}

	b := &strings.Builder{}
	for i, sel := range p.path {
		x := sel.sel
		// TODO: use '.' in all cases, once supported.
		switch {
		case x.kind() == adt.IntLabel:
			b.WriteByte('[')
			b.WriteString(x.String())
			b.WriteByte(']')
			continue
		case i > 0:
			b.WriteByte('.')
		}

		b.WriteString(x.String())
	}
	return b.String()
}

func toSelectors(expr ast.Expr) []Selector {
	switch x := expr.(type) {
	case *ast.Ident:
		return []Selector{identSelector(x)}

	case *ast.IndexExpr:
		a := toSelectors(x.X)
		var sel Selector
		if b, ok := x.Index.(*ast.BasicLit); !ok {
			sel = Selector{pathError{
				errors.Newf(token.NoPos, "non-constant expression %s",
					internal.DebugStr(x.Index))}}
		} else {
			sel = basicLitSelector(b)
		}
		return append(a, sel)

	case *ast.SelectorExpr:
		a := toSelectors(x.X)
		return append(a, identSelector(x.Sel))

	default:
		return []Selector{Selector{pathError{
			errors.Newf(token.NoPos, "invalid label %s ", internal.DebugStr(x)),
		}}}
	}
}

func basicLitSelector(b *ast.BasicLit) Selector {
	switch b.Kind {
	case token.INT:
		var n literal.NumInfo
		if err := literal.ParseNum(b.Value, &n); err != nil {
			return Selector{pathError{
				errors.Newf(token.NoPos, "invalid string index %s", b.Value),
			}}
		}
		var d apd.Decimal
		_ = n.Decimal(&d)
		i, err := d.Int64()
		if err != nil {
			return Selector{pathError{
				errors.Newf(token.NoPos, "integer %s out of range", b.Value),
			}}
		}
		return Index(int(i))

	case token.STRING:
		info, _, _, _ := literal.ParseQuotes(b.Value, b.Value)
		if !info.IsDouble() {
			return Selector{pathError{
				errors.Newf(token.NoPos, "invalid string index %s", b.Value)}}
		}
		s, _ := literal.Unquote(b.Value)
		return Selector{stringSelector(s)}

	default:
		return Selector{pathError{
			errors.Newf(token.NoPos, "invalid literal %s", b.Value),
		}}
	}
}

func identSelector(label ast.Label) Selector {
	switch x := label.(type) {
	case *ast.Ident:
		if isHiddenOrDefinition(x.Name) {
			return Selector{definitionSelector(x.Name)}
		}
		return Selector{stringSelector(x.Name)}

	case *ast.BasicLit:
		return basicLitSelector(x)

	default:
		return Selector{pathError{
			errors.Newf(token.NoPos, "invalid label %s ", internal.DebugStr(x)),
		}}
	}
}

// Err reports errors that occurred when generating the path.
func (p Path) Err() error {
	var errs errors.Error
	for _, x := range p.path {
		if err, ok := x.sel.(pathError); ok {
			errs = errors.Append(errs, err.Error)
		}
	}
	return errs
}

func isHiddenOrDefinition(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "_")
}

// A Def marks a string as a definition label. An # will be added if a string is
// not prefixed with an # or _# already. Hidden labels are qualified by the
// package in which they are looked up.
func Def(s string) Selector {
	if !isHiddenOrDefinition(s) {
		s = "#" + s
	}
	return Selector{definitionSelector(s)}
}

type definitionSelector string

// String returns the CUE representation of the definition.
func (d definitionSelector) String() string {
	return string(d)
}

func (d definitionSelector) kind() adt.FeatureType {
	switch {
	case strings.HasPrefix(string(d), "#"):
		return adt.DefinitionLabel
	case strings.HasPrefix(string(d), "_#"):
		return adt.HiddenDefinitionLabel
	case strings.HasPrefix(string(d), "_"):
		return adt.HiddenLabel
	default:
		panic("invalid definition")
	}
}

func (d definitionSelector) feature(r adt.Runtime) adt.Feature {
	return adt.MakeIdentLabel(r, string(d), "")
}

// A Str is a CUE string label. Definition selectors are defined with Def.
func Str(s string) Selector {
	return Selector{stringSelector(s)}
}

type stringSelector string

func (s stringSelector) String() string {
	str := string(s)
	if isHiddenOrDefinition(str) || !ast.IsValidIdent(str) {
		return literal.Label.Quote(str)
	}
	return str
}

func (s stringSelector) kind() adt.FeatureType { return adt.StringLabel }

func (s stringSelector) feature(r adt.Runtime) adt.Feature {
	return adt.MakeStringLabel(r, string(s))
}

// An Index selects a list element by index.
func Index(x int) Selector {
	f, err := adt.MakeLabel(nil, int64(x), adt.IntLabel)
	if err != nil {
		return Selector{pathError{err}}
	}
	return Selector{indexSelector(f)}
}

type indexSelector adt.Feature

func (s indexSelector) String() string {
	return strconv.Itoa(adt.Feature(s).Index())
}

func (s indexSelector) kind() adt.FeatureType { return adt.IntLabel }

func (s indexSelector) feature(r adt.Runtime) adt.Feature {
	return adt.Feature(s)
}

// TODO: allow import paths to be represented?
//
// // ImportPath defines a lookup at the root of an instance. It must be the first
// // element of a Path.
// func ImportPath(s string) Selector {
// 	return importSelector(s)
// }

// type importSelector string

// func (s importSelector) String() string {
// 	return literal.String.Quote(string(s))
// }

// func (s importSelector) feature(r adt.Runtime) adt.Feature {
// 	return adt.InvalidLabel
// }

// TODO: allow looking up in parent scopes?

// // Parent returns a Selector for looking up in the parent of a current node.
// // Parent selectors may only occur at the start of a Path.
// func Parent() Selector {
// 	return parentSelector{}
// }

// type parentSelector struct{}

// func (p parentSelector) String() string { return "__up" }
// func (p parentSelector) feature(r adt.Runtime) adt.Feature {
// 	return adt.InvalidLabel
// }

type pathError struct {
	errors.Error
}

func (p pathError) String() string        { return p.Error.Error() }
func (p pathError) kind() adt.FeatureType { return 0 }
func (p pathError) feature(r adt.Runtime) adt.Feature {
	return adt.InvalidLabel
}
