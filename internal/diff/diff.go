// Copyright 2019 CUE Authors
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

package diff

import (
	"cuelang.org/go/cue"
)

// Profile configures a diff operation.
type Profile struct {
	Concrete bool

	// Hidden fields are only useful to compare when a values are from the same
	// package.
	SkipHidden bool

	// TODO: Use this method instead of SkipHidden. To do this, we need to have
	// access the package associated with a hidden field, which is only
	// accessible through the Iterator API. And we should probably get rid of
	// the cue.Struct API.
	//
	// HiddenPkg compares hidden fields for the package if this is not the empty
	// string. Use "_" for the anonymous package.
	// HiddenPkg string
}

var (
	// Schema is the standard profile used for comparing schema.
	Schema = &Profile{}

	// Final is the standard profile for comparing data.
	Final = &Profile{
		Concrete: true,
	}
)

// TODO: don't return Kind, which is always Modified or not.

// Diff is a shorthand for Schema.Diff.
func Diff(x, y cue.Value) (Kind, *EditScript) {
	return Schema.Diff(x, y)
}

// Diff returns an edit script representing the difference between x and y.
func (p *Profile) Diff(x, y cue.Value) (Kind, *EditScript) {
	d := differ{cfg: *p}
	k, es := d.diffValue(x, y)
	if k == Modified && es == nil {
		es = &EditScript{X: x, Y: y}
	}
	return k, es
}

// Kind identifies the kind of operation of an edit script.
type Kind uint8

const (
	// Identity indicates that a value pair is identical in both list X and Y.
	Identity Kind = iota
	// UniqueX indicates that a value only exists in X and not Y.
	UniqueX
	// UniqueY indicates that a value only exists in Y and not X.
	UniqueY
	// Modified indicates that a value pair is a modification of each other.
	Modified
)

// EditScript represents the series of differences between two CUE values.
// x and y must be either both list or struct.
type EditScript struct {
	X, Y  cue.Value
	Edits []Edit
}

// Edit represents a single operation within an edit-script.
type Edit struct {
	Kind Kind
	XSel cue.Selector // valid if UniqueY
	YSel cue.Selector // valid if UniqueX
	Sub  *EditScript  // non-nil if Modified
}

type differ struct {
	cfg Profile
}

func (d *differ) diffValue(x, y cue.Value) (Kind, *EditScript) {
	if d.cfg.Concrete {
		x, _ = x.Default()
		y, _ = y.Default()
	}
	if x.IncompleteKind() != y.IncompleteKind() {
		return Modified, nil
	}

	switch xc, yc := x.IsConcrete(), y.IsConcrete(); {
	case xc != yc:
		return Modified, nil

	case xc && yc:
		switch k := x.Kind(); k {
		case cue.StructKind:
			return d.diffStruct(x, y)

		case cue.ListKind:
			return d.diffList(x, y)
		}
		fallthrough

	default:
		// In concrete mode we do not care about non-concrete values.
		if d.cfg.Concrete {
			return Identity, nil
		}

		if !x.Equals(y) {
			return Modified, nil
		}
	}

	return Identity, nil
}

type field struct {
	sel cue.Selector
	val cue.Value
}

// TODO(mvdan): use slices.Collect once we swap cue.Iterator for a Go iterator
func (d *differ) collectFields(v cue.Value) []field {
	iter, _ := v.Fields(cue.Hidden(!d.cfg.SkipHidden), cue.Definitions(true), cue.Optional(true))
	var fields []field
	for iter.Next() {
		fields = append(fields, field{iter.Selector(), iter.Value()})
	}
	return fields
}

func (d *differ) diffStruct(x, y cue.Value) (Kind, *EditScript) {
	xFields := d.collectFields(x)
	yFields := d.collectFields(y)

	// Best-effort topological sort, prioritizing x over y, using a variant of
	// Kahn's algorithm (see, for instance
	// https://www.geeksforgeeks.org/topological-sorting-indegree-based-solution/).
	// We assume that the order of the elements of each value indicate an edge
	// in the graph. This means that only the next unprocessed nodes can be
	// those with no incoming edges.
	xMap := make(map[cue.Selector]struct{}, len(xFields))
	yMap := make(map[cue.Selector]int, len(yFields))
	for _, f := range xFields {
		xMap[f.sel] = struct{}{}
	}
	for i, f := range yFields {
		yMap[f.sel] = i + 1
	}

	edits := []Edit{}
	differs := false

	for xi, yi := 0, 0; xi < len(xFields) || yi < len(yFields); {
		// Process zero nodes
		for ; xi < len(xFields); xi++ {
			xf := xFields[xi]
			yp := yMap[xf.sel]
			if yp > 0 {
				break
			}
			edits = append(edits, Edit{UniqueX, xf.sel, cue.Selector{}, nil})
			differs = true
		}
		for ; yi < len(yFields); yi++ {
			yf := yFields[yi]
			if yMap[yf.sel] == 0 {
				// already done
				continue
			}
			if _, ok := xMap[yf.sel]; ok {
				break
			}
			yMap[yf.sel] = 0
			edits = append(edits, Edit{UniqueY, cue.Selector{}, yf.sel, nil})
			differs = true
		}

		// Compare nodes
		for ; xi < len(xFields); xi++ {
			xf := xFields[xi]
			yp := yMap[xf.sel]
			if yp == 0 {
				break
			}
			// If yp != xi+1, the topological sort was not possible.
			yMap[xf.sel] = 0

			yf := yFields[yp-1]

			var kind Kind
			var script *EditScript
			switch {
			case xf.sel.IsDefinition() != yf.sel.IsDefinition(), xf.sel.ConstraintType() != yf.sel.ConstraintType():
				kind = Modified
			default:
				// TODO(perf): consider evaluating lazily.
				kind, script = d.diffValue(xf.val, yf.val)
			}

			edits = append(edits, Edit{kind, xf.sel, yf.sel, script})
			differs = differs || kind != Identity
		}
	}
	if !differs {
		return Identity, nil
	}
	return Modified, &EditScript{X: x, Y: y, Edits: edits}
}

func (d *differ) diffList(x, y cue.Value) (Kind, *EditScript) {
	xs := collectList(x)
	ys := collectList(y)

	edits := myersEdits(xs, ys)
	edits = d.mergeAdjacentEdits(edits, xs, ys)

	for _, e := range edits {
		if e.Kind != Identity {
			return Modified, &EditScript{X: x, Y: y, Edits: edits}
		}
	}
	return Identity, nil
}

// collectList collects all elements of a CUE list into a slice.
func collectList(v cue.Value) []cue.Value {
	var elems []cue.Value
	for it, _ := v.List(); it.Next(); {
		elems = append(elems, it.Value())
	}
	return elems
}

// myersEdits computes a minimal edit script between xs and ys using Myers'
// diff algorithm. The returned edits contain only Identity, UniqueX, and
// UniqueY kinds. Modified edits with sub-scripts are produced separately by
// mergeAdjacentEdits.
//
// Based on "An O(ND) Difference Algorithm and Its Variations" by Eugene W.
// Myers (1986).
func myersEdits(xs, ys []cue.Value) []Edit {
	nx, ny := len(xs), len(ys)
	if nx == 0 && ny == 0 {
		return nil
	}

	// maxD is the maximum possible edit distance.
	maxD := nx + ny
	offset := maxD // maps diagonal k to index k+offset in v

	// v[k+offset] is the furthest x-coordinate reachable on diagonal k.
	// Diagonal k is defined as x - y = k.
	v := make([]int, 2*maxD+1)
	v[1+offset] = 0 // starting sentinel: diagonal 1, x=0

	// trace[d] stores a copy of v captured before computing step d.
	// It is used during backtracking to recover the edit path.
	trace := make([][]int, 0, maxD+1)

	finalD := -1

loop:
	for d := 0; d <= maxD; d++ {
		vcopy := make([]int, len(v))
		copy(vcopy, v)
		trace = append(trace, vcopy)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+offset] < v[k+1+offset]) {
				x = v[k+1+offset] // move down (UniqueY: advance y, x stays)
			} else {
				x = v[k-1+offset] + 1 // move right (UniqueX: advance x)
			}
			y := x - k
			// Follow diagonal: matching elements are free moves.
			for x < nx && y < ny && xs[x].Equals(ys[y]) {
				x++
				y++
			}
			v[k+offset] = x
			if x == nx && y == ny {
				finalD = d
				break loop
			}
		}
	}

	if finalD < 0 {
		// Should not happen: maxD == nx+ny guarantees termination.
		// Defensive fallback: mark all of X as removed, all of Y as added.
		edits := make([]Edit, 0, nx+ny)
		for i := range xs {
			edits = append(edits, Edit{UniqueX, cue.Index(i), cue.Selector{}, nil})
		}
		for j := range ys {
			edits = append(edits, Edit{UniqueY, cue.Selector{}, cue.Index(j), nil})
		}
		return edits
	}

	// Backtrack from (nx, ny) to (0, 0) to recover the edit path.
	// Edits are appended in reverse order and flipped at the end.
	edits := make([]Edit, 0, nx+ny)
	x, y := nx, ny
	for d := finalD; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[k-1+offset] < v[k+1+offset]) {
			prevK = k + 1 // came from a down move (UniqueY)
		} else {
			prevK = k - 1 // came from a right move (UniqueX)
		}
		prevX := v[prevK+offset]
		prevY := prevX - prevK

		// Walk back along the diagonal (Identity edits).
		for x > prevX && y > prevY {
			x--
			y--
			edits = append(edits, Edit{Identity, cue.Index(x), cue.Index(y), nil})
		}

		// The non-diagonal move that started this diagonal.
		if prevK == k+1 {
			// Down move: UniqueY (y was advanced, x stayed).
			y--
			edits = append(edits, Edit{UniqueY, cue.Selector{}, cue.Index(y), nil})
		} else {
			// Right move: UniqueX (x was advanced).
			x--
			edits = append(edits, Edit{UniqueX, cue.Index(x), cue.Selector{}, nil})
		}
	}
	// Remaining diagonal at d=0 (all Identity).
	for x > 0 && y > 0 {
		x--
		y--
		edits = append(edits, Edit{Identity, cue.Index(x), cue.Index(y), nil})
	}

	// Edits were accumulated in reverse; flip to restore forward order.
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}
	return edits
}

// mergeAdjacentEdits post-processes the raw edit script from myersEdits to
// produce Modified edits for adjacent UniqueX/UniqueY pairs that have the
// same structural kind. This preserves recursive sub-diff output: two list
// elements that differ by only one field are shown as Modified with a
// sub-script rather than as a bare deletion + insertion.
func (d *differ) mergeAdjacentEdits(edits []Edit, xs, ys []cue.Value) []Edit {
	result := make([]Edit, 0, len(edits))
	i := 0
	for i < len(edits) {
		if edits[i].Kind != UniqueX {
			result = append(result, edits[i])
			i++
			continue
		}
		// Collect a contiguous run of UniqueX edits.
		xStart := i
		for i < len(edits) && edits[i].Kind == UniqueX {
			i++
		}
		xEnd := i

		// Collect the immediately following run of UniqueY edits.
		yStart := i
		for i < len(edits) && edits[i].Kind == UniqueY {
			i++
		}
		yEnd := i

		// Pair them up (the shorter run limits the pairing count).
		xi, yi := xStart, yStart
		for xi < xEnd && yi < yEnd {
			xe := edits[xi]
			ye := edits[yi]
			xv := xs[xe.XSel.Index()]
			yv := ys[ye.YSel.Index()]
			if xv.IncompleteKind() == yv.IncompleteKind() {
				kind, script := d.diffValue(xv, yv)
				result = append(result, Edit{kind, xe.XSel, ye.YSel, script})
			} else {
				result = append(result, xe)
				result = append(result, ye)
			}
			xi++
			yi++
		}
		// Append any unpaired edits from the longer run.
		for ; xi < xEnd; xi++ {
			result = append(result, edits[xi])
		}
		for ; yi < yEnd; yi++ {
			result = append(result, edits[yi])
		}
	}
	return result
}
