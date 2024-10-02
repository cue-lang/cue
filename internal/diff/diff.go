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
		es = &EditScript{x: x, y: y}
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
	x, y  cue.Value
	edits []Edit
}

// Len returns the number of edits.
func (es *EditScript) Len() int {
	return len(es.edits)
}

// Label returns a string representation of the label.
func (es *EditScript) LabelX(i int) string {
	e := es.edits[i]
	p := e.XPos()
	if p < 0 {
		return ""
	}
	return label(es.x, p)
}

func (es *EditScript) LabelY(i int) string {
	e := es.edits[i]
	p := e.YPos()
	if p < 0 {
		return ""
	}
	return label(es.y, p)
}

// TODO: support label expressions.
func label(v cue.Value, i int) string {
	st, err := v.Struct()
	if err != nil {
		return ""
	}

	// TODO: return formatted expression for optionals.
	f := st.Field(i)
	str := f.Selector
	if f.IsOptional {
		str += "?"
	}
	str += ":"
	return str
}

// ValueX returns the value of X involved at step i.
func (es *EditScript) ValueX(i int) (v cue.Value) {
	p := es.edits[i].XPos()
	if p < 0 {
		return v
	}
	st, err := es.x.Struct()
	if err != nil {
		return v
	}
	return st.Field(p).Value
}

// ValueY returns the value of Y involved at step i.
func (es *EditScript) ValueY(i int) (v cue.Value) {
	p := es.edits[i].YPos()
	if p < 0 {
		return v
	}
	st, err := es.y.Struct()
	if err != nil {
		return v
	}
	return st.Field(p).Value
}

// Edit represents a single operation within an edit-script.
type Edit struct {
	kind Kind
	xPos int32       // 0 if UniqueY
	yPos int32       // 0 if UniqueX
	sub  *EditScript // non-nil if Modified
}

func (e Edit) Kind() Kind { return e.kind }
func (e Edit) XPos() int  { return int(e.xPos - 1) }
func (e Edit) YPos() int  { return int(e.yPos - 1) }

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
			edits = append(edits, Edit{UniqueX, int32(xi + 1), 0, nil})
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
			edits = append(edits, Edit{UniqueY, 0, int32(yi + 1), nil})
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

			edits = append(edits, Edit{kind, int32(xi + 1), int32(yp), script})
			differs = differs || kind != Identity
		}
	}
	if !differs {
		return Identity, nil
	}
	return Modified, &EditScript{x: x, y: y, edits: edits}
}

// TODO: right now we do a simple element-by-element comparison. Instead,
// use an algorithm that approximates a minimal Levenshtein distance, like the
// one in github.com/google/go-cmp/internal/diff.
func (d *differ) diffList(x, y cue.Value) (Kind, *EditScript) {
	ix, _ := x.List()
	iy, _ := y.List()

	edits := []Edit{}
	differs := false
	i := int32(1)

	for {
		// TODO: This would be much easier with a Next/Done API.
		hasX := ix.Next()
		hasY := iy.Next()
		if !hasX {
			for hasY {
				differs = true
				edits = append(edits, Edit{UniqueY, 0, i, nil})
				hasY = iy.Next()
				i++
			}
			break
		}
		if !hasY {
			for hasX {
				differs = true
				edits = append(edits, Edit{UniqueX, i, 0, nil})
				hasX = ix.Next()
				i++
			}
			break
		}

		// Both x and y have a value.
		kind, script := d.diffValue(ix.Value(), iy.Value())
		if kind != Identity {
			differs = true
		}
		edits = append(edits, Edit{kind, i, i, script})
		i++
	}
	if !differs {
		return Identity, nil
	}
	return Modified, &EditScript{x: x, y: y, edits: edits}
}
