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

package load

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/buildattr"
	"cuelang.org/go/internal/cli"
)

type tagger struct {
	cfg *Config
	// tagMap holds true for all the tags in cfg.Tags that
	// are not associated with a value.
	tagMap map[string]bool
	// tags keeps a record of all the @tag attibutes found in files.
	tags         []*tag // tags found in files
	replacements map[ast.Node]ast.Node

	// mu guards the usedTags map.
	mu sync.Mutex
	// usedTags keeps a record of all the tag attributes found in files.
	usedTags map[string]bool
}

func newTagger(c *Config) *tagger {
	tagMap := map[string]bool{}
	for _, t := range c.Tags {
		if !strings.ContainsRune(t, '=') {
			tagMap[t] = true
		}
	}
	return &tagger{
		cfg:      c,
		tagMap:   tagMap,
		usedTags: make(map[string]bool),
	}
}

// tagIsSet reports whether the tag with the given key
// is enabled. It also updates t.usedTags to
// reflect that the tag has been seen.
func (tg *tagger) tagIsSet(key string) bool {
	tg.mu.Lock()
	tg.usedTags[key] = true
	tg.mu.Unlock()
	return tg.tagMap[key]
}

// A TagVar represents an injection variable.
type TagVar struct {
	// Func returns an ast for a tag variable. It is only called once
	// per evaluation of a configuration.
	Func func() (ast.Expr, error)

	// Description documents this TagVar.
	Description string
}

// DefaultTagVars creates a new map with a set of supported injection variables.
func DefaultTagVars() map[string]TagVar {
	return map[string]TagVar{
		"now": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(time.Now().UTC().Format(time.RFC3339Nano)), nil
			},
		},
		"os": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(runtime.GOOS), nil
			},
		},
		"arch": {
			Func: func() (ast.Expr, error) {
				return ast.NewString(runtime.GOARCH), nil
			},
		},
		"cwd": {
			Func: func() (ast.Expr, error) {
				return varToString(os.Getwd())
			},
		},
		"username": {
			Func: func() (ast.Expr, error) {
				u, err := user.Current()
				if err != nil {
					return nil, err
				}
				return ast.NewString(u.Username), nil
			},
		},
		"hostname": {
			Func: func() (ast.Expr, error) {
				return varToString(os.Hostname())
			},
		},
		"rand": {
			Func: func() (ast.Expr, error) {
				var b [16]byte
				rand.Read(b[:])
				var hx [34]byte
				hx[0] = '0'
				hx[1] = 'x'
				hex.Encode(hx[2:], b[:])
				return ast.NewLit(token.INT, string(hx[:])), nil
			},
		},
	}
}

func varToString(s string, err error) (ast.Expr, error) {
	if err != nil {
		return nil, err
	}
	return ast.NewString(s), nil
}

// A tag binds an identifier to a field to allow passing command-line values.
//
// A tag is of the form
//
//	@tag(<name>,[type=(string|int|number|bool)][,short=<shorthand>+])
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
	key            string
	kind           cue.Kind
	shorthands     []string
	vars           string // -T flag
	hasReplacement bool

	field *ast.Field
}

func parseTag(pos token.Pos, body string) (t *tag, err errors.Error) {
	t = &tag{}
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
		for s := range strings.SplitSeq(s, "|") {
			if !ast.IsValidIdent(t.key) {
				return t, errors.Newf(pos, "invalid identifier %q", s)
			}
			t.shorthands = append(t.shorthands, s)
		}
	}

	if s, ok, _ := a.Lookup(1, "var"); ok {
		t.vars = s
	}

	return t, nil
}

func (t *tag) inject(value string, tg *tagger) errors.Error {
	e, err := cli.ParseValue(token.NoPos, t.key, value, t.kind)
	t.injectValue(e, tg)
	return err
}

func (t *tag) injectValue(x ast.Expr, tg *tagger) {
	injected := ast.NewBinExpr(token.AND, t.field.Value, x)
	if tg.replacements == nil {
		tg.replacements = make(map[ast.Node]ast.Node)
	}
	tg.replacements[t.field.Value] = injected
	t.field.Value = injected
	t.hasReplacement = true
}

// findTags defines which fields may be associated with tags.
//
// TODO: should we limit the depth at which tags may occur?
func findTags(b *build.Instance) (tags []*tag, errs errors.Error) {
	findInvalidTags := func(x ast.Node, msg string) {
		ast.Walk(x, nil, func(n ast.Node) {
			if f, ok := n.(*ast.Field); ok {
				for _, a := range f.Attrs {
					if key, _ := a.Split(); key == "tag" {
						errs = errors.Append(errs, errors.Newf(a.Pos(), "%s", msg))
						// TODO: add position of x.
					}
				}
			}
		})
	}
	for _, f := range b.Files {
		ast.Walk(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ListLit:
				findInvalidTags(n, "@tag not allowed within lists")
				return false

			case *ast.Comprehension:
				findInvalidTags(n, "@tag not allowed within comprehension")
				return false

			case *ast.Field:
				// TODO: allow optional fields?
				_, _, err := ast.LabelName(x.Label)
				if err != nil || x.Constraint != token.ILLEGAL {
					findInvalidTags(n, "@tag not allowed within field constraint")
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
			}
			return true
		}, nil)
	}
	return tags, errs
}

func (tg *tagger) injectTags(tags []string) errors.Error {
	// Parses command line args
	for _, s := range tags {
		name, val, ok := strings.Cut(s, "=")
		found := tg.usedTags[s]
		if ok { // key-value
			for _, t := range tg.tags {
				if t.key == name {
					found = true
					if err := t.inject(val, tg); err != nil {
						return err
					}
				}
			}
			if !found {
				return errors.Newf(token.NoPos, "no tag for %q", name)
			}
		} else { // shorthand
			for _, t := range tg.tags {
				for _, sh := range t.shorthands {
					if sh == s {
						found = true
						if err := t.inject(s, tg); err != nil {
							return err
						}
					}
				}
			}
			if !found {
				return errors.Newf(token.NoPos, "tag %q not used in any file", s)
			}
		}
	}

	if tg.cfg.TagVars != nil {
		vars := map[string]ast.Expr{}

		// Inject tag variables if the tag wasn't already set.
		for _, t := range tg.tags {
			if t.hasReplacement || t.vars == "" {
				continue
			}
			x, ok := vars[t.vars]
			if !ok {
				tv, ok := tg.cfg.TagVars[t.vars]
				if !ok {
					return errors.Newf(token.NoPos,
						"tag variable '%s' not found", t.vars)
				}
				tag, err := tv.Func()
				if err != nil {
					return errors.Wrapf(err, token.NoPos,
						"error getting tag variable '%s'", t.vars)
				}
				x = tag
				vars[t.vars] = tag
			}
			if x != nil {
				t.injectValue(x, tg)
			}
		}
	}
	return nil
}

func shouldBuildFile(f *ast.File, tagIsSet func(key string) bool) errors.Error {
	ok, attr, err := buildattr.ShouldBuildFile(f, tagIsSet)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if key, body := attr.Split(); key == "if" {
		return excludeError{errors.Newf(attr.Pos(), "@if(%s) did not match", body)}
	} else {
		return excludeError{errors.Newf(attr.Pos(), "@ignore() attribute found")}
	}
}
