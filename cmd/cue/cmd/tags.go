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

package cmd

import (
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cli"
)

func decorateInstances(cmd *Command, tags []string, a []*build.Instance) {
	if len(tags) == 0 {
		return
	}
	exitOnErr(cmd, injectTags(tags, a), true)
}

// A tag binds an identifier to a field to allow passing command-line values.
//
// A tag is of the form
//     @tag(<name>,[type=(string|int|number|bool)][,short=<shorthand>+])
//
// The name is mandatory and type defaults to string. Tags are set using the -t
// option on the command line. -t name=value will parse value for the type
// defined for name and set the field for which this tag was defined to this
// value. A tag may be associated with multiple fields.
//
// Tags also allow shorthands. If a shorthand bar is declared for a tag with
// name foo, then -t bar is identical to -t foo=bar.
//
// It is a deliberate choice to not allow other values to be associated with
// shorthands than the shorthand name itself. Doing so would create a powerful
// mechanism that would assign different values to different fields based on the
// same shorthand, duplicating functionality that is already available in CUE.
type tag struct {
	key        string
	kind       cue.Kind
	shorthands []string

	field *ast.Field
}

func parseTag(pos token.Pos, body string) (t tag, err errors.Error) {
	t.kind = cue.StringKind

	a := internal.ParseAttrBody(pos, body)

	t.key, _ = a.String(0)
	if !ast.IsValidIdent(t.key) {
		return t, errors.Newf(pos, "invalid identifier %q", t.key)
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
			return t, errors.Newf(pos, "invalid type %q", s)
		}
	}

	if s, ok, _ := a.Lookup(1, "short"); ok {
		for _, s := range strings.Split(s, "|") {
			if !ast.IsValidIdent(t.key) {
				return t, errors.Newf(pos, "invalid identifier %q", s)
			}
			t.shorthands = append(t.shorthands, s)
		}
	}

	return t, nil
}

func (t *tag) inject(value string) errors.Error {
	e, err := cli.ParseValue(token.NoPos, t.key, value, t.kind)
	if err != nil {
		return err
	}
	t.field.Value = ast.NewBinExpr(token.AND, t.field.Value, e)
	return nil
}

// findTags defines which fields may be associated with tags.
//
// TODO: should we limit the depth at which tags may occur?
func findTags(b *build.Instance) (tags []tag, errs errors.Error) {
	for _, f := range b.Files {
		ast.Walk(f, func(n ast.Node) bool {
			if b.Err != nil {
				return false
			}

			switch x := n.(type) {
			case *ast.StructLit, *ast.File:
				return true

			case *ast.Field:
				// TODO: allow optional fields?
				_, _, err := ast.LabelName(x.Label)
				if err != nil || x.Optional != token.NoPos {
					return false
				}

				for _, a := range x.Attrs {
					key, body := a.Split()
					if key != "tag" {
						continue
					}
					t, err := parseTag(a.Pos(), body)
					if err != nil {
						errs = errors.Append(errs, err)
						continue
					}
					t.field = x
					tags = append(tags, t)
				}
				return true
			}
			return false
		}, nil)
	}
	return tags, errs
}

func injectTags(tags []string, b []*build.Instance) errors.Error {
	var a []tag
	for _, p := range b {
		x, err := findTags(p)
		if err != nil {
			return err
		}
		a = append(a, x...)
	}

	// Parses command line args
	for _, s := range tags {
		p := strings.Index(s, "=")
		found := false
		if p > 0 { // key-value
			for _, t := range a {
				if t.key == s[:p] {
					found = true
					if err := t.inject(s[p+1:]); err != nil {
						return err
					}
				}
			}
			if !found {
				return errors.Newf(token.NoPos, "no tag for %q", s[:p])
			}
		} else { // shorthand
			for _, t := range a {
				for _, sh := range t.shorthands {
					if sh == s {
						found = true
						if err := t.inject(s); err != nil {
							return err
						}
					}
				}
			}
			if !found {
				return errors.Newf(token.NoPos, "no shorthand for %q", s)
			}
		}
	}
	return nil
}
