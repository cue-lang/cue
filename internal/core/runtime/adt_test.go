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

package runtime_test

import (
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// TestEmptyNodeInert checks that initializing the shared empty node, as
// returned for a failed lookup or list conversion, does not create a
// nodeContext for it.
func TestEmptyNodeInert(t *testing.T) {
	ctx := adt.NewContext(runtime.New(), &adt.Vertex{})

	// RawList returns the shared empty node for a value which is not a list.
	empty := ctx.RawList(&adt.Top{})
	ctx.Err() // discard the type error
	if empty != ctx.RawList(&adt.Top{}) {
		t.Fatal("RawList did not return a shared node")
	}
	ctx.Err()

	// The empty node is shared, so an earlier test may already have given
	// it a nodeContext. Check that it is finalized, and separately that a
	// fresh finalized vertex does not get a nodeContext either.
	done := &adt.Vertex{}
	done.ForceDone()
	if empty.Status() != done.Status() {
		t.Fatalf("empty node has status %v, want %v", empty.Status(), done.Status())
	}
	for _, v := range []*adt.Vertex{empty, done} {
		before := *ctx.Stats()
		v.Init(ctx)
		after := *ctx.Stats()
		if after.Allocs != before.Allocs || after.Reused != before.Reused {
			t.Fatalf("finalized node got a nodeContext: allocs %d -> %d, reused %d -> %d",
				before.Allocs, after.Allocs, before.Reused, after.Reused)
		}
	}
}

func TestFeatureBool(t *testing.T) {
	r := runtime.New()
	ctx := adt.NewContext(r, &adt.Vertex{})

	makeInt := func(x int64) adt.Feature {
		f, _ := adt.MakeLabel(nil, x, adt.IntLabel)
		return f
	}

	testCases := []struct {
		in           adt.Feature
		isRegular    bool
		isDefinition bool
		isHidden     bool
		isString     bool
		isInt        bool
	}{{
		in:        ctx.StringLabel("foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        ctx.StringLabel("_"),
		isRegular: true,
		isString:  true,
	}, {
		in:        ctx.StringLabel("_#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        ctx.StringLabel("#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "_"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "_#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        adt.MakeStringLabel(r, "#foo"),
		isRegular: true,
		isString:  true,
	}, {
		in:        makeInt(4),
		isRegular: true,
		isInt:     true,
	}, {
		in:        adt.MakeIdentLabel(r, "foo", "main"),
		isRegular: true,
		isString:  true,
	}, {
		in:           adt.MakeIdentLabel(r, "#foo", "main"),
		isDefinition: true,
	}, {
		in:           adt.MakeIdentLabel(r, "_#foo", "main"),
		isDefinition: true,
		isHidden:     true,
	}, {
		in:       adt.MakeIdentLabel(r, "_foo", "main"),
		isHidden: true,
	}}
	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if got := tc.in.IsRegular(); got != tc.isRegular {
				t.Errorf("IsRegular: got %v; want %v", got, tc.isRegular)
			}
			if got := tc.in.IsString(); got != tc.isString {
				t.Errorf("IsString: got %v; want %v", got, tc.isString)
			}
			if got := tc.in.IsInt(); got != tc.isInt {
				t.Errorf("IsInt: got %v; want %v", got, tc.isInt)
			}
			if got := tc.in.IsDef(); got != tc.isDefinition {
				t.Errorf("isDefinition: got %v; want %v", got, tc.isDefinition)
			}
			if got := tc.in.IsHidden(); got != tc.isHidden {
				t.Errorf("IsHidden: got %v; want %v", got, tc.isHidden)
			}
		})
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
