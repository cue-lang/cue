// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt_test

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

func closeAll(ctx *cue.Context, v cue.Value) cue.Value {
	path := cue.MakePath(cue.Def("#x"))
	return ctx.CompileString("#x: _").FillPath(path, v).LookupPath(path)
}

type editConfig struct {
	Edits []cue.Value `json:"edits,omitempty"`
}

// Modifying values can cause a mix of non-finalized nodes as children of
// a finalized parent. Ensure that this does not cause issues.
func TestAPIModifyingValues(t *testing.T) {
	ctx := cuecontext.New()

	v := ctx.CompileString(`
        edits: [{
            type: "replace"
            data!: "^wrong"
        }]
    `)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	closed := closeAll(ctx, ctx.EncodeType(editConfig{}))

	v = v.Unify(closed)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	var cfg editConfig
	if err := v.Decode(&cfg); err != nil {
		t.Fatal(err)
	}

	w := ctx.CompileString(`
        type!: "fill" | "replace"
        data!: _
    `)

	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	x := cfg.Edits[0]
	x = x.Unify(w)

	if err := x.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestValidatorYield checks that the validators of a vertex give the same
// result when one yields, as existsN does while a peer it refers to is being
// computed, as when none does.
//
// A validator named "fail..." fails, "incomplete" gives an incomplete error,
// "struct" fails if given the struct rather than its error, and "pass" passes.
func TestValidatorYield(t *testing.T) {
	type unwind struct{}
	tests := []struct {
		validators []string
		yielder    string // none if empty
		want       string
	}{{
		validators: []string{"pass", "fail"},
		yielder:    "fail",
		want:       "fail",
	}, {
		validators: []string{"pass", "fail"},
		yielder:    "pass",
		want:       "fail",
	}, {
		validators: []string{"failA", "failB"},
		want:       "failA (and 1 more errors)",
	}, {
		validators: []string{"failA", "failB"},
		yielder:    "failB",
		want:       "failA (and 1 more errors)",
	}, {
		validators: []string{"incomplete", "pass", "struct"},
		want:       "struct",
	}, {
		validators: []string{"incomplete", "pass", "struct"},
		yielder:    "pass",
		want:       "struct",
	}, {
		validators: []string{"incomplete", "pass", "struct"},
		yielder:    "struct",
		want:       "struct",
	}}
	for _, test := range tests {
		name := strings.Join(test.validators, ",")
		if test.yielder != "" {
			name += "/" + test.yielder + " yields"
		}
		t.Run(name, func(t *testing.T) {
			r := runtime.New()
			v := &adt.Vertex{}
			v.AddConjunct(adt.MakeRootConjunct(nil, &adt.StructLit{}))
			yielded := false
			for _, name := range test.validators {
				v.AddConjunct(adt.MakeRootConjunct(nil, &adt.ExternalValidator{
					Name: name,
					Validate: func(c *adt.OpContext, x adt.Value) *adt.Bottom {
						if name == test.yielder && !yielded {
							yielded = true
							panic(unwind{})
						}
						switch {
						case name == "incomplete":
							b := c.NewErrf("incomplete")
							b.Code = adt.IncompleteError
							return b
						case name == "struct":
							if b := x.(*adt.Vertex).Bottom(); b != nil {
								return b
							}
							return c.NewErrf("struct")
						case strings.HasPrefix(name, "fail"):
							return c.NewErrf("%s", name)
						}
						return nil
					},
				}))
			}

			c := adt.NewContext(r, v)
			finalize := func() {
				defer func() {
					if e := recover(); e != nil {
						if _, ok := e.(unwind); !ok {
							panic(e)
						}
					}
				}()
				v.Finalize(c)
			}
			finalize()
			if test.yielder != "" {
				if !yielded {
					t.Fatal("no validator yielded")
				}
				finalize()
			}
			got := ""
			if b := v.Bottom(); b != nil {
				got = b.Err.Error()
			}
			if got != test.want {
				t.Errorf("got error %q, want %q", got, test.want)
			}
		})
	}
}
