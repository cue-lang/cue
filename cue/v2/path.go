// Copyright 2026 The CUE Authors
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
	"fmt"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/core/adt"
	"github.com/cockroachdb/apd/v3"
)

// selKind discriminates the kinds of selector supported by this
// package. This is a deliberately compact subset of the v1 selector
// taxonomy: regular string labels, definitions, hidden labels (produced
// by field enumeration; there is no exported constructor yet), list
// indices, and the AnyString/AnyIndex wildcards.
type selKind uint8

const (
	selInvalid selKind = iota
	selString
	selDef
	selHidden
	selIndex
	selAnyString
	selAnyIndex
)

// A Selector is one path element: a field label, a definition, an index,
// or a pattern.
type Selector struct {
	kind  selKind
	name  string // field or identifier name (selString, selDef, selHidden)
	pkg   string // package scope for hidden labels
	index int    // list index (selIndex)
	err   errors.Error
}

// String returns the CUE syntax for the selector.
func (sel Selector) String() string {
	switch sel.kind {
	case selString:
		if ast.StringLabelNeedsQuoting(sel.name) {
			return literal.Label.Quote(sel.name)
		}
		return sel.name
	case selDef, selHidden:
		return sel.name
	case selIndex:
		return strconv.Itoa(sel.index)
	case selAnyString, selAnyIndex:
		return "[_]"
	}
	return "_|_"
}

// Unquoted returns the unquoted value of a string label. It panics for
// other selector kinds.
func (sel Selector) Unquoted() string {
	if sel.kind != selString {
		panic("Selector.Unquoted invoked on non-string label")
	}
	return sel.name
}

// IsString reports whether sel is a regular (non-definition, non-hidden)
// field label.
func (sel Selector) IsString() bool { return sel.kind == selString }

// IsDefinition reports whether sel is a definition label.
func (sel Selector) IsDefinition() bool { return sel.kind == selDef }

// Index returns the index of the selector. It panics unless sel was
// created by [Index].
func (sel Selector) Index() int {
	if sel.kind != selIndex {
		panic("Selector.Index invoked on non-index selector")
	}
	return sel.index
}

// feature resolves the selector to a runtime label. It is resolved at
// realization time because interning a label requires the runtime.
func (sel Selector) feature(r adt.Runtime) (adt.Feature, errors.Error) {
	switch sel.kind {
	case selString:
		return adt.MakeStringLabel(r, sel.name), nil
	case selDef:
		return adt.MakeIdentLabel(r, sel.name, ""), nil
	case selHidden:
		return adt.MakeIdentLabel(r, sel.name, sel.pkg), nil
	case selIndex:
		return adt.MakeLabel(nil, int64(sel.index), adt.IntLabel)
	case selAnyString:
		return adt.AnyString, nil
	case selAnyIndex:
		return adt.AnyIndex, nil
	}
	err := sel.err
	if err == nil {
		err = errors.Newf(token.NoPos, "invalid selector")
	}
	return adt.InvalidLabel, err
}

// Str constructs a regular field label selector.
func Str(s string) Selector {
	return Selector{kind: selString, name: s}
}

// Def marks a string as a definition label. A # will be added if s is
// not prefixed with a #. It panics if the result is not a valid
// identifier.
func Def(s string) Selector {
	if !internal.IsDef(s) {
		s = "#" + s
	}
	if !ast.IsValidIdent(s) {
		panic(fmt.Sprintf("invalid definition %s", s))
	}
	return Selector{kind: selDef, name: s}
}

// Index selects a list element by index. It returns an invalid selector
// if the index is out of range.
func Index(i int) Selector {
	if _, err := adt.MakeLabel(nil, int64(i), adt.IntLabel); err != nil {
		return Selector{kind: selInvalid, err: err}
	}
	return Selector{kind: selIndex, index: i}
}

var (
	// AnyString is a Selector that can be used to ask for the pattern
	// constraint that applies to any regular string field.
	AnyString = Selector{kind: selAnyString}

	// AnyIndex is a Selector that can be used to ask for the pattern
	// constraint that applies to any list element.
	AnyIndex = Selector{kind: selAnyIndex}
)

// hiddenSel constructs a selector for a hidden label. There is no
// exported constructor for hidden labels yet; they arise only from
// field enumeration.
func hiddenSel(name, pkg string) Selector {
	return Selector{kind: selHidden, name: name, pkg: pkg}
}

// errSel constructs an invalid selector recording err.
func errSel(err errors.Error) Selector {
	return Selector{kind: selInvalid, err: err}
}

// featureToSel maps a runtime label back to a Selector.
func featureToSel(f adt.Feature, r adt.Runtime) Selector {
	switch f.Typ() {
	case adt.StringLabel:
		return Str(f.StringValue(r))
	case adt.IntLabel:
		return Index(f.Index())
	case adt.DefinitionLabel:
		return Def(f.IdentString(r))
	case adt.HiddenLabel, adt.HiddenDefinitionLabel:
		return hiddenSel(f.IdentString(r), f.PkgID(r))
	}
	return errSel(errors.Newf(token.NoPos, "unexpected feature type %v", f.Typ()))
}

// A Path is a sequence of selectors addressing a location within a value.
// The zero value is the empty path.
type Path struct {
	path []Selector
}

// MakePath creates a path from a sequence of selectors.
func MakePath(sels ...Selector) Path {
	return Path{path: slices.Clone(sels)}
}

// ParsePath parses a CUE path expression such as a.b[2]."c-d".
// An error is reported via the returned path's Err method.
//
// Unlike with normal CUE expressions, the first element of the path may
// be a string literal. A path may not contain hidden fields.
func ParsePath(s string) Path {
	if s == "" {
		return Path{}
	}
	expr, err := parser.ParseExpr("", s)
	if err != nil {
		return MakePath(errSel(errors.Promote(err, "invalid path")))
	}
	p := Path{path: toSelectors(expr)}
	for _, sel := range p.path {
		if sel.kind == selHidden {
			return MakePath(errSel(errors.Newf(token.NoPos,
				"invalid path: hidden fields not allowed in path %s", s)))
		}
	}
	return p
}

// Selectors returns the individual elements of the path.
func (p Path) Selectors() []Selector {
	return p.path
}

// String returns the CUE syntax for the path.
func (p Path) String() string {
	if err := p.Err(); err != nil {
		return "_|_"
	}
	var b strings.Builder
	for i, sel := range p.path {
		switch {
		case sel.kind == selIndex:
			b.WriteByte('[')
			b.WriteString(sel.String())
			b.WriteByte(']')
			continue
		case i > 0:
			b.WriteByte('.')
		}
		b.WriteString(sel.String())
	}
	return b.String()
}

// Err reports whether the path is valid.
func (p Path) Err() error {
	var errs errors.Error
	for _, sel := range p.path {
		if sel.kind == selInvalid {
			err := sel.err
			if err == nil {
				err = errors.Newf(token.NoPos, "invalid selector")
			}
			errs = errors.Append(errs, err)
		}
	}
	if errs == nil {
		return nil
	}
	return errs
}

func toSelectors(expr ast.Expr) []Selector {
	switch x := expr.(type) {
	case *ast.Ident:
		return []Selector{identSelector(x)}

	case *ast.BasicLit:
		return []Selector{basicLitSelector(x)}

	case *ast.IndexExpr:
		a := toSelectors(x.X)
		var sel Selector
		if b, ok := x.Index.(*ast.BasicLit); !ok {
			sel = errSel(errors.Newf(token.NoPos, "non-constant expression %s",
				astinternal.DebugStr(x.Index)))
		} else {
			sel = basicLitSelector(b)
		}
		return appendSelector(a, sel)

	case *ast.SelectorExpr:
		a := toSelectors(x.X)
		var sel Selector
		switch l := x.Sel.(type) {
		case *ast.Ident:
			sel = identSelector(l)
		case *ast.BasicLit:
			sel = basicLitSelector(l)
		default:
			sel = errSel(errors.Newf(token.NoPos,
				"invalid label %s", astinternal.DebugStr(x.Sel)))
		}
		return appendSelector(a, sel)

	case *ast.ListLit:
		// A list literal can only appear as the first element of a path,
		// where it represents an index, e.g. "[2]" or "[2].foo". This is
		// the inverse of how Path.String formats a leading index selector.
		if len(x.Elts) == 1 {
			if b, ok := x.Elts[0].(*ast.BasicLit); ok {
				return []Selector{basicLitSelector(b)}
			}
		}
	}

	return []Selector{errSel(errors.Newf(token.NoPos,
		"invalid label %s ", astinternal.DebugStr(expr)))}
}

// appendSelector is like append(a, sel), except that it collects errors
// in a one-element slice.
func appendSelector(a []Selector, sel Selector) []Selector {
	isErr := sel.kind == selInvalid
	if len(a) == 1 && a[0].kind == selInvalid {
		if isErr {
			a[0].err = errors.Append(a[0].err, sel.err)
		}
		return a
	}
	if isErr {
		return []Selector{sel}
	}
	return append(a, sel)
}

func identSelector(x *ast.Ident) Selector {
	switch s := x.Name; {
	case strings.HasPrefix(s, "_"):
		return errSel(errors.Newf(token.NoPos,
			"invalid path: hidden label %s not allowed", s))
	case strings.HasPrefix(s, "#"):
		return Selector{kind: selDef, name: s}
	default:
		return Str(s)
	}
}

func basicLitSelector(b *ast.BasicLit) Selector {
	switch b.Kind {
	case token.INT:
		var n literal.NumInfo
		if err := literal.ParseNum(b.Value, &n); err != nil {
			return errSel(errors.Newf(token.NoPos, "invalid string index %s", b.Value))
		}
		var d apd.Decimal
		_ = n.Decimal(&d)
		i, err := d.Int64()
		if err != nil {
			return errSel(errors.Newf(token.NoPos, "integer %s out of range", b.Value))
		}
		return Index(int(i))

	case token.STRING:
		info, _, _, _ := literal.ParseQuotes(b.Value, b.Value)
		if !info.IsDouble() {
			return errSel(errors.Newf(token.NoPos, "invalid string index %s", b.Value))
		}
		s, _ := literal.Unquote(b.Value)
		return Str(s)

	default:
		return errSel(errors.Newf(token.NoPos, "invalid literal %s", b.Value))
	}
}
