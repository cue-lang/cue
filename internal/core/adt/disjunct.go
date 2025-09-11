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

package adt

import (
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// Nodes man not reenter a disjunction.
//
// Copy one layer deep; throw away items on failure.

// DISJUNCTION ALGORITHM
//
// The basic concept of the algorithm is to use backtracking to find valid
// disjunctions. The algorithm can stop if two matching disjuncts are found
// where one does not subsume the other.
//
// At a later point, we can introduce a filter step to filter out possible
// disjuncts based on, say, discriminator fields or field exclusivity (oneOf
// fields in Protobuf).
//
// To understand the details of the algorithm, it is important to understand
// some properties of disjunction.
//
//
// EVALUATION OF A DISJUNCTION IS SELF CONTAINED
//
// In other words, fields outside of a disjunction cannot bind to values within
// a disjunction whilst evaluating that disjunction. This allows the computation
// of disjunctions to be isolated from side effects.
//
// The intuition behind this is as follows: as a disjunction is not a concrete
// value, it is not possible to lookup a field within a disjunction if it has
// not yet been evaluated. So if a reference within a disjunction that is needed
// to disambiguate that disjunction refers to a field outside the scope of the
// disjunction which, in turn, refers to a field within the disjunction, this
// results in a cycle error. We achieve this by not removing the cycle marker of
// the Vertex of the disjunction until the disjunction is resolved.
//
// Note that the following disjunct is still allowed:
//
//    a: 1
//    b: a
//
// Even though `a` refers to the root of the disjunction, it does not _select
// into_ the disjunction. Implementation-wise, it also doesn't have to, as the
// respective vertex is available within the Environment. Referencing a node
// outside the disjunction that in turn selects the disjunction root, however,
// will result in a detected cycle.
//
// As usual, cycle detection should be interpreted marked as incomplete, so that
// the referring node will not be fixed to an error prematurely.
//
//
// SUBSUMPTION OF AMBIGUOUS DISJUNCTS
//
// A disjunction can be evaluated to a concrete value if only one disjunct
// remains. Aside from disambiguating through unification failure, disjuncts
// may also be disambiguated by taking the least specific of two disjuncts.
// For instance, if a subsumes b, then the result of disjunction may be a.
//
//   NEW ALGORITHM NO LONGER VERIFIES SUBSUMPTION. SUBSUMPTION IS INHERENTLY
//   IMPRECISE (DUE TO BULK OPTIONAL FIELDS). OTHER THAN THAT, FOR SCALAR VALUES
//   IT JUST MEANS THERE IS AMBIGUITY, AND FOR STRUCTS IT CAN LEAD TO STRANGE
//   CONSEQUENCES.
//
//   USE EQUALITY INSTEAD:
//     - Undefined == error for optional fields.
//     - So only need to check exact labels for vertices.

type envDisjunct struct {
	env     *Environment
	cloneID CloseInfo
	holeID  int

	// TODO: src is set but never used. It seems like it should be used
	// in either [nodeContext.crossProduct] or [nodeContext.collectErrors]?
	src       Node
	disjuncts []disjunct
}

func (n *nodeContext) makeError() {
	code := IncompleteError

	if len(n.disjunctErrs) > 0 {
		code = EvalError
		for _, c := range n.disjunctErrs {
			if c.Code > code {
				code = c.Code
			}
		}
	}

	b := &Bottom{
		Code: code,
		Err:  n.disjunctError(),
		Node: n.node,
	}
	n.node.SetValue(n.ctx, b)
}

func mode(hasDefault, marked bool) defaultMode {
	var mode defaultMode
	switch {
	case !hasDefault:
		mode = maybeDefault
	case marked:
		mode = isDefault
	default:
		mode = notDefault
	}
	return mode
}

// Default rules from spec:
//
// U1: (v1, d1) & v2       => (v1&v2, d1&v2)
// U2: (v1, d1) & (v2, d2) => (v1&v2, d1&d2)
//
// D1: (v1, d1) | v2       => (v1|v2, d1)
// D2: (v1, d1) | (v2, d2) => (v1|v2, d1|d2)
//
// M1: *v        => (v, v)
// M2: *(v1, d1) => (v1, d1)
//
// NOTE: M2 cannot be *(v1, d1) => (v1, v1), as this has the weird property
// of making a value less specific. This causes issues, for instance, when
// trimming.
//
// The old implementation does something similar though. It will discard
// default information after first determining if more than one conjunct
// has survived.
//
// def + maybe -> def
// not + maybe -> def
// not + def   -> def

type defaultMode uint8

const (
	maybeDefault defaultMode = iota
	isDefault
	notDefault
)

// combineDefaults combines default modes for unifying conjuncts.
//
// Default rules from spec:
//
// U1: (v1, d1) & v2       => (v1&v2, d1&v2)
// U2: (v1, d1) & (v2, d2) => (v1&v2, d1&d2)
func combineDefault(a, b defaultMode) defaultMode {
	if a > b {
		return a
	}
	return b
}

// disjunctError returns a compound error for a failed disjunction.
//
// TODO(perf): the set of errors is now computed during evaluation. Eventually,
// this could be done lazily.
func (n *nodeContext) disjunctError() (errs errors.Error) {
	ctx := n.ctx

	disjuncts := selectErrors(n.disjunctErrs)
	var pos errors.Error

	if len(n.userErrs) > 0 {
		pos = disjuncts
		disjuncts = selectErrors(n.userErrs)
	}

	if disjuncts == nil {
		errs = ctx.Newf("empty disjunction") // XXX: add space to sort first
	} else {
		disjuncts = errors.Sanitize(disjuncts)
		k := len(errors.Errors(disjuncts))
		if k == 1 {
			if pos != nil {
				addDisjunctPositions(disjuncts.(*ValueError), pos)
			}
			return disjuncts
		}
		// prefix '-' to sort to top
		errs = ctx.Newf("%d errors in empty disjunction:", k)
		if pos != nil {
			addDisjunctPositions(errs.(*ValueError), pos)
		}
		errs = errors.Append(errs, disjuncts)
	}

	return errs
}

func addDisjunctPositions(dst *ValueError, src errors.Error) {
	for _, p := range src.InputPositions() {
		dst.AddPos(p)
	}
}

func selectErrors(a []*Bottom) (errs errors.Error) {
	// return all errors if less than a certain number.
	if len(a) <= 2 {
		for _, b := range a {
			errs = errors.Append(errs, b.Err)

		}
		return errs
	}

	// First select only relevant errors.
	isIncomplete := false
	k := 0
	for _, b := range a {
		if !isIncomplete && b.Code >= IncompleteError {
			k = 0
			isIncomplete = true
		}
		a[k] = b
		k++
	}
	a = a[:k]

	// filter errors
	positions := map[token.Pos]bool{}

	add := func(b *Bottom, p token.Pos) bool {
		if positions[p] {
			return false
		}
		positions[p] = true
		errs = errors.Append(errs, b.Err)
		return true
	}

	for _, b := range a {
		// TODO: Should we also distinguish by message type?
		if add(b, b.Err.Position()) {
			continue
		}
		for _, p := range b.Err.InputPositions() {
			if add(b, p) {
				break
			}
		}
	}

	return errs
}
