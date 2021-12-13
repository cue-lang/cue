// Copyright 2018 The CUE Authors
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

// Package trim removes fields that may be inferred from another mixed in value
// that "dominates" it. For instance, a value that is merged in from a
// definition is considered to dominate a value from a regular struct that
// mixes in this definition. Values derived from constraints and comprehensions
// can also dominate other fields.
//
// A value A is considered to be implied by a value B if A subsumes the default
// value of B. For instance, if a definition defines a field `a: *1 | int` and
// mixed in with a struct that defines a field `a: 1 | 2`, then the latter can
// be removed because a definition field dominates a regular field and because
// the latter subsumes the default value of the former.
//
//
// Examples:
//
// 	light: [string]: {
// 		room:          string
// 		brightnessOff: *0.0 | >=0 & <=100.0
// 		brightnessOn:  *100.0 | >=0 & <=100.0
// 	}
//
// 	light: ceiling50: {
// 		room:          "MasterBedroom"
// 		brightnessOff: 0.0    // this line
// 		brightnessOn:  100.0  // and this line will be removed
// 	}
//
// Results in:
//
// 	// Unmodified: light: [string]: { ... }
//
// 	light: ceiling50: {
// 		room: "MasterBedroom"
// 	}
//
package trim

import (
	"io"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/subsume"
	"cuelang.org/go/internal/core/walk"
	"cuelang.org/go/internal/value"
)

// Config configures trim options.
type Config struct {
	Trace bool
}

// Files trims fields in the given files that can be implied from other fields,
// as can be derived from the evaluated values in inst.
// Trimming is done on a best-effort basis and only when the removed field
// is clearly implied by another field, rather than equal sibling fields.
func Files(files []*ast.File, inst cue.InstanceOrValue, cfg *Config) error {
	r, v := value.ToInternal(inst.Value())

	t := &trimmer{
		Config:  *cfg,
		ctx:     adt.NewContext(r, v),
		remove:  map[ast.Node]bool{},
		exclude: map[ast.Node]bool{},
		debug:   Debug,
		w:       os.Stderr,
	}

	// Mark certain expressions as off limits.
	// TODO: We could alternatively ensure that comprehensions unconditionally
	// resolve.
	visitor := &walk.Visitor{
		Before: func(n adt.Node) bool {
			switch x := n.(type) {
			case *adt.StructLit:
				// Structs with comprehensions may never be removed.
				for _, d := range x.Decls {
					switch d.(type) {
					case *adt.Comprehension:
						t.markKeep(x)
					}
				}
			}
			return true
		},
	}
	for _, c := range v.Conjuncts {
		visitor.Elem(c.Elem())
	}

	d, _, _, pickedDefault := t.addDominators(nil, v, false)
	t.findSubordinates(d, v, pickedDefault)

	// Remove subordinate values from files.
	for _, f := range files {
		astutil.Apply(f, func(c astutil.Cursor) bool {
			if f, ok := c.Node().(*ast.Field); ok && t.remove[f.Value] && !t.exclude[f.Value] {
				c.Delete()
			}
			return true
		}, nil)
		if err := astutil.Sanitize(f); err != nil {
			return err
		}
	}

	return nil
}

type trimmer struct {
	Config

	ctx     *adt.OpContext
	remove  map[ast.Node]bool
	exclude map[ast.Node]bool

	debug  bool
	indent int
	w      io.Writer
}

var Debug bool = false

func (t *trimmer) markRemove(c adt.Conjunct) {
	if src := c.Elem().Source(); src != nil {
		t.remove[src] = true
		if t.debug {
			t.logf("removing %s", debug.NodeString(t.ctx, c.Elem(), nil))
		}
	}
}

func (t *trimmer) markKeep(x adt.Expr) {
	if src := x.Source(); src != nil {
		t.exclude[src] = true
		if t.debug {
			t.logf("keeping")
		}
	}
}

const dominatorNode = adt.ComprehensionSpan | adt.DefinitionSpan | adt.ConstraintSpan

// isDominator reports whether a node can remove other nodes.
func isDominator(c adt.Conjunct) (ok, mayRemove bool) {
	if !c.CloseInfo.IsInOneOf(dominatorNode) {
		return false, false
	}
	switch c.Field().(type) {
	case *adt.OptionalField: // bulk constraints handled elsewhere.
		return true, false
	}
	return true, true
}

// Removable reports whether a non-dominator conjunct can be removed. This is
// not the case if it has pattern constraints that could turn into dominator
// nodes.
func removable(c adt.Conjunct, v *adt.Vertex) bool {
	return c.CloseInfo.FieldTypes&(adt.HasAdditional|adt.HasPattern) == 0
}

// Roots of constraints are not allowed to strip conjuncts by
// themselves as it will eliminate the reason for the trigger.
func (t *trimmer) allowRemove(v *adt.Vertex) bool {
	for _, c := range v.Conjuncts {
		_, allowRemove := isDominator(c)
		loc := c.CloseInfo.Location() != c.Elem()
		isSpan := c.CloseInfo.RootSpanType() != adt.ConstraintSpan
		if allowRemove && (loc || isSpan) {
			return true
		}
	}
	return false
}

// A parent may be removed if there is not a `no` and there is at least one
// `yes`. A `yes` is proves that there is at least one node that is not a
// dominator node and that we are not removing nodes from a declaration of a
// dominator itself.
const (
	no = 1 << iota
	maybe
	yes
)

// addDominators injects dominator values from v into d. If no default has
// been selected from dominators so far, the values are erased. Otherwise,
// both default and new values are merged.
//
// Erasing the previous values when there has been no default so far allows
// interpolations, for instance, to be evaluated in the new context and
// eliminated.
//
// Values are kept when there has been a default (current or ancestor) because
// the current value may contain information that caused that default to be
// selected and thus erasing it would cause that information to be lost.
//
// TODO:
// In principle, information only needs to be kept for discriminator values, or
// any value that was instrumental in selecting the default. This is currently
// hard to do, however, so we just fall back to a stricter mode in the presence
// of defaults.
func (t *trimmer) addDominators(d, v *adt.Vertex, hasDisjunction bool) (doms *adt.Vertex, ambiguous, hasSubs, strict bool) {
	strict = hasDisjunction
	doms = &adt.Vertex{
		Parent: v.Parent,
		Label:  v.Label,
	}
	if d != nil && hasDisjunction {
		doms.Conjuncts = append(doms.Conjuncts, d.Conjuncts...)
	}

	hasDoms := false
	for _, c := range v.Conjuncts {
		isDom, _ := isDominator(c)
		switch {
		case isDom:
			doms.AddConjunct(c)
		default:
			if r, ok := c.Elem().(adt.Resolver); ok {
				x, _ := t.ctx.Resolve(c.Env, r)
				// Even if this is not a dominator now, descendants will be.
				if x != nil && x.Label.IsDef() {
					for _, c := range x.Conjuncts {
						doms.AddConjunct(c)
					}
					break
				}
			}
			hasSubs = true
		}
	}
	doms.Finalize(t.ctx)

	switch x := doms.Value().(type) {
	case *adt.Disjunction:
		switch x.NumDefaults {
		case 1:
			strict = true
		default:
			ambiguous = true
		}
	}

	if doms = doms.Default(); doms.IsErr() {
		ambiguous = true
	}

	_ = hasDoms
	return doms, hasSubs, ambiguous, strict || ambiguous
}

func (t *trimmer) findSubordinates(doms, v *adt.Vertex, hasDisjunction bool) (result int) {
	defer un(t.trace(v))
	defer func() {
		if result == no {
			for _, c := range v.Conjuncts {
				t.markKeep(c.Expr())
			}
		}
	}()

	doms, hasSubs, ambiguous, pickedDefault := t.addDominators(doms, v, hasDisjunction)

	if ambiguous {
		return no
	}

	// TODO(structure sharing): do not descend into vertices whose parent is not
	// equal to the parent. This is not relevant at this time, but may be so in
	// the future.

	if len(v.Arcs) > 0 {
		var match int
		for _, a := range v.Arcs {
			d := doms.Lookup(a.Label)
			match |= t.findSubordinates(d, a, pickedDefault)
		}

		// This also skips embedded scalars if not all fields are removed. In
		// this case we need to preserve the scalar to keep the type of the
		// struct intact, which might as well be done by not removing the scalar
		// type.
		if match&yes == 0 || match&no != 0 {
			return match
		}
	}

	if !t.allowRemove(v) {
		return no
	}

	switch v.BaseValue.(type) {
	case *adt.StructMarker, *adt.ListMarker:
		// Rely on previous processing of the Arcs and the fact that we take the
		// default value to check dominator subsumption, meaning that we don't
		// have to check additional optional constraints to pass subsumption.

	default:
		if !hasSubs {
			return maybe
		}

		// This should normally not be necessary, as subsume should catch this.
		// But as we already take the default value for doms, it doesn't hurt to
		// do it.
		v = v.Default()

		// This is not necessary, but seems like it may result in more
		// user-friendly semantics.
		if v.IsErr() {
			return no
		}

		// TODO: since we take v, instead of the unification of subordinate
		// values, it should suffice to take equality here:
		//    doms ⊑ subs  ==> doms == subs&doms
		if err := subsume.Value(t.ctx, v, doms); err != nil {
			return no
		}
	}

	for _, c := range v.Conjuncts {
		_, allowRemove := isDominator(c)
		if !allowRemove && removable(c, v) {
			t.markRemove(c)
		}
	}

	return yes
}
