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

package cueload

// The @tag injection mechanics in this file are ported from
// cue/load/tags.go, adapted to the cueload configuration surface:
// Config.Tags is a map from tag name to value, and shorthands are
// selected by entries with an empty value.

import (
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/internal"
)

// injectTags finds the @tag attributes in the given files and injects
// the values configured in the loader's Tags and TagVars into the
// tagged fields. It is called once per package, when the package's
// files are parsed.
func (l *Loader) injectTags(files []*ast.File) errors.Error {
	tags, errs := findTags(files)
	if errs != nil {
		return errs
	}
	if len(tags) == 0 {
		return nil
	}
	replacements := make(map[ast.Node]ast.Node)
	for _, t := range tags {
		if val, ok := l.cfg.Tags[t.key]; ok && val != "" {
			x, err := parseTagValue(t.key, val, t.kind)
			if err != nil {
				return err
			}
			t.injectValue(x, replacements)
			continue
		}
		for _, sh := range t.shorthands {
			if val, ok := l.cfg.Tags[sh]; ok && val == "" {
				t.injectValue(ast.NewString(sh), replacements)
				break
			}
		}
	}

	// Inject tag variables for tags that have not been set explicitly.
	for _, t := range tags {
		if t.injected || t.vars == "" {
			continue
		}
		x, err := l.tagVarValue(t.vars)
		if err != nil {
			return err
		}
		if x != nil {
			t.injectValue(x, replacements)
		}
	}

	if len(replacements) == 0 {
		return nil
	}
	// Re-point identifiers that resolved to a replaced node, mirroring
	// the replacement walk in cue/load.Instances.
	for _, f := range files {
		ast.Walk(f, nil, func(n ast.Node) {
			if ident, ok := n.(*ast.Ident); ok {
				if v, ok := replacements[ident.Node]; ok {
					ident.Node = v
				}
			}
		})
	}
	return nil
}

// tagVarValue returns the memoized value of the named tag variable,
// invoking its Func at most once per loader.
func (l *Loader) tagVarValue(name string) (ast.Expr, errors.Error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if x, ok := l.tagVarValues[name]; ok {
		return x, nil
	}
	tv, ok := l.cfg.TagVars[name]
	if !ok {
		return nil, errors.Newf(token.NoPos, "tag variable %q not found", name)
	}
	x, err := tv.Func()
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "error getting tag variable %q", name)
	}
	l.tagVarValues[name] = x
	return x, nil
}

// A tag binds an identifier to a field to allow passing values at load
// time.
//
// A tag is of the form
//
//	@tag(<name>,[type=(string|int|number|bool)][,short=<shorthand>+])
//
// The name is mandatory and type defaults to string. A tag may be
// associated with multiple fields. Tags also allow shorthands: if a
// shorthand bar is declared for a tag with name foo, then a Tags entry
// "bar" with an empty value is identical to a "foo" entry with value
// "bar".
type tag struct {
	key        string
	kind       cue.Kind
	shorthands []string
	vars       string // the var option
	injected   bool

	field *ast.Field
}

func (t *tag) injectValue(x ast.Expr, replacements map[ast.Node]ast.Node) {
	injected := ast.NewBinExpr(token.AND, t.field.Value, x)
	replacements[t.field.Value] = injected
	t.field.Value = injected
	t.injected = true
}

func parseTag(astAttr *ast.Attribute) (*tag, errors.Error) {
	t := &tag{kind: cue.StringKind}

	a := internal.ParseAttr(astAttr)

	t.key, _ = a.String(0)
	if !ast.IsValidIdent(t.key) {
		return t, errors.Newf(a.Pos, "invalid identifier %q", t.key)
	}

	if s, ok, _ := a.Lookup(1, "type"); ok {
		switch s {
		case "string":
		case "int":
			t.kind = cue.IntKind
		case "number":
			t.kind = cue.NumberKind
		case "bool":
			t.kind = cue.BoolKind
		default:
			return t, errors.Newf(a.Pos, "invalid type %q", s)
		}
	}

	if s, ok, _ := a.Lookup(1, "short"); ok {
		for sh := range strings.SplitSeq(s, "|") {
			if !ast.IsValidIdent(sh) {
				return t, errors.Newf(a.Pos, "invalid identifier %q", sh)
			}
			t.shorthands = append(t.shorthands, sh)
		}
	}

	if s, ok, _ := a.Lookup(1, "var"); ok {
		t.vars = s
	}

	return t, nil
}

// findTags defines which fields may be associated with tags.
func findTags(files []*ast.File) (tags []*tag, errs errors.Error) {
	findInvalidTags := func(x ast.Node, msg string) {
		ast.Walk(x, nil, func(n ast.Node) {
			if f, ok := n.(*ast.Field); ok {
				for _, a := range f.Attrs {
					if key, _ := a.Split(); key == "tag" {
						errs = errors.Append(errs, errors.Newf(a.Pos(), "%s", msg))
					}
				}
			}
		})
	}
	for _, f := range files {
		ast.Walk(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ListLit:
				findInvalidTags(n, "@tag not allowed within lists")
				return false

			case *ast.Comprehension:
				findInvalidTags(n, "@tag not allowed within comprehension")
				return false

			case *ast.Field:
				_, _, err := ast.LabelName(x.Label)
				if err != nil || x.Constraint != token.ILLEGAL {
					findInvalidTags(n, "@tag not allowed within field constraint")
					return false
				}

				for _, a := range x.Attrs {
					if a.Name() != "tag" {
						continue
					}
					t, err := parseTag(a)
					if err != nil {
						errs = errors.Append(errs, err)
						continue
					}
					t.field = x
					tags = append(tags, t)
				}
			}
			return true
		}, nil)
	}
	return tags, errs
}

// parseTagValue parses a tag value string according to the tag's kind,
// mirroring the value grammar of cue/load (see internal/cli.ParseValue).
func parseTagValue(name, str string, k cue.Kind) (ast.Expr, errors.Error) {
	var expr ast.Expr
	var x ast.Expr
	var errs errors.Error

	if k&cue.NumberKind != 0 {
		var info literal.NumInfo
		if err := literal.ParseNum(str, &info); err != nil {
			// Note that the wrapped err already mentions str.
			errs = errors.Wrapf(err, token.NoPos,
				"invalid number for injection tag %q", name)
		} else if info.IsInt() {
			expr = ast.NewLit(token.INT, str)
		} else if k&cue.FloatKind == 0 {
			errs = errors.Newf(token.NoPos,
				"invalid int %q for injection tag %q", str, name)
		} else {
			expr = ast.NewLit(token.FLOAT, str)
		}
	}

	if k&cue.BoolKind != 0 {
		str = strings.TrimSpace(str)
		b, ok := boolValues[str]
		if !ok {
			errs = errors.Append(errs, errors.Newf(token.NoPos,
				"invalid boolean %q for injection tag %q", str, name))
		} else if expr != nil || k&cue.StringKind != 0 {
			bl := ast.NewBool(b)
			if expr != nil {
				expr = &ast.BinaryExpr{Op: token.OR, X: expr, Y: bl}
			} else {
				expr = bl
			}
		} else {
			x = ast.NewBool(b)
		}
	}

	if k&cue.StringKind != 0 {
		if expr != nil {
			expr = &ast.BinaryExpr{Op: token.OR, X: expr, Y: ast.NewString(str)}
		} else {
			x = ast.NewString(str)
		}
	}

	switch {
	case expr != nil:
		return expr, nil
	case x != nil:
		return x, nil
	case errs == nil:
		return nil, errors.Newf(token.NoPos,
			"invalid type for injection tag %q", name)
	}
	return nil, errs
}

var boolValues = map[string]bool{
	"1":     true,
	"0":     false,
	"t":     true,
	"f":     false,
	"T":     true,
	"F":     false,
	"true":  true,
	"false": false,
	"TRUE":  true,
	"FALSE": false,
	"True":  true,
	"False": false,
}
