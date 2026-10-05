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

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package list

import (
	"slices"
	"sort"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/value"
)

// valueSorter defines a sort.Interface; implemented in cue/builtinutil.go.
type valueSorter struct {
	ctx *adt.OpContext
	a   []cue.Value
	err error

	cmp *adt.Vertex
	// The labels of the comparator fields.
	less, x, y adt.Feature
}

func (s *valueSorter) ret() ([]cue.Value, error) {
	if s.err != nil {
		return nil, s.err
	}
	// The input slice is already a copy and that we can modify it safely.
	return s.a, nil
}

func (s *valueSorter) Len() int      { return len(s.a) }
func (s *valueSorter) Swap(i, j int) { s.a[i], s.a[j] = s.a[j], s.a[i] }
func (s *valueSorter) Less(i, j int) bool {
	if s.err != nil {
		return false
	}

	return s.lessNew(i, j)
}

func (s *valueSorter) lessNew(i, j int) bool {
	ctx := s.ctx

	n := &adt.Vertex{
		Label:     s.cmp.Label,
		Parent:    s.cmp.Parent,
		Conjuncts: s.cmp.Conjuncts,
	}
	// Create the arcs x and y holding the inputs before anything may evaluate
	// them, such as a comprehension. Resolving the arcs first and inserting
	// the inputs into x and y would evaluate such a comprehension, and with it
	// x and y, too early.
	n.Arcs = []*adt.Vertex{
		{Label: s.x, Parent: n, Conjuncts: s.a[i].Core().V.Conjuncts},
		{Label: s.y, Parent: n, Conjuncts: s.a[j].Core().V.Conjuncts},
	}

	// Resolve the direct arcs like makeValueSorter does, as they may come
	// from embeddings or unifications.
	n.CompleteArcsShallow(ctx)

	less := n.Lookup(s.less)

	// TODO(perf): if we can determine that the comparator values for
	// x and y are idempotent (no arcs and a basevalue being top or
	// a struct or list marker), then we do not need to reevaluate the input.
	// In that case, the arcs x and y could take the BaseValue and Arcs of
	// the inputs instead of their conjuncts. This may improve performance
	// significantly.

	// Err finalizes less, and BoolValue then records an error for a value
	// which is not a concrete bool. Keep the error as a [cue.Value] error so
	// that its code is preserved, as a non-concrete less field is an
	// incomplete error rather than a fatal one.
	b := less.Err(ctx)
	isLess := ctx.BoolValue(less)
	if b == nil {
		b = ctx.Err()
	}
	if b != nil {
		s.err = value.Make(ctx, b).Err()
		return true
	}

	return isLess
}

func makeValueSorter(list []cue.Value, cmp cue.Value) (s valueSorter) {
	v := cmp.Core()
	ctx := eval.NewContext(v.R, v.V)

	n := &adt.Vertex{
		Label:     v.V.Label,
		Parent:    v.V.Parent,
		Conjuncts: v.V.Conjuncts,
	}
	n.CompleteArcsShallow(ctx)

	s = valueSorter{
		a:    list,
		ctx:  ctx,
		cmp:  n,
		less: ctx.StringLabel("less"),
		x:    ctx.StringLabel("x"),
		y:    ctx.StringLabel("y"),
	}
	// The comparator is already concrete here, so a missing field is fatal
	// rather than incomplete; a constraint such as less?: bool is no field.
	for _, f := range []adt.Feature{s.less, s.x, s.y} {
		if arc := n.Lookup(f); arc == nil || arc.ArcType != adt.ArcMember {
			return valueSorter{err: errors.Newf(token.NoPos, "field not found: %s", f.StringValue(ctx))}
		}
	}
	return s
}

// Sort sorts data while keeping the original order of equal elements.
// It does O(n*log(n)) comparisons.
//
// cmp is a struct of the form {T: _, x: T, y: T, less: bool}, where
// less should reflect x < y.
//
// Example:
//
//	Sort([2, 3, 1], list.Ascending)
//
//	Sort([{a: 2}, {a: 3}, {a: 1}], {x: {}, y: {}, less: x.a < y.a})
func Sort(list []cue.Value, cmp cue.Value) (sorted []cue.Value, err error) {
	s := makeValueSorter(list, cmp)

	// The input slice is already a copy and that we can modify it safely.
	sort.Stable(&s)
	return s.ret()
}

// Deprecated: use [Sort], which is always stable
func SortStable(list []cue.Value, cmp cue.Value) (sorted []cue.Value, err error) {
	s := makeValueSorter(list, cmp)
	sort.Stable(&s)
	return s.ret()
}

// SortStrings sorts a list of strings in increasing order.
func SortStrings(a []string) []string {
	slices.Sort(a)
	return a
}

// IsSorted tests whether a list is sorted.
//
// See Sort for an example comparator.
func IsSorted(list []cue.Value, cmp cue.Value) (bool, error) {
	s := makeValueSorter(list, cmp)
	sorted := sort.IsSorted(&s)
	if s.err != nil {
		return false, s.err
	}
	return sorted, nil
}

// IsSortedStrings tests whether a list is a sorted list of strings.
func IsSortedStrings(a []string) bool {
	return slices.IsSorted(a)
}
