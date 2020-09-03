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

package eval

// TODO: rename this file to fieldset.go

import (
	"cuelang.org/go/internal/core/adt"
)

// fieldSet represents the fields for a single struct literal, along
// the constraints of fields that may be added.
type fieldSet struct {
	next *fieldSet
	pos  adt.Node

	// TODO: look at consecutive identical environments to figure out
	// what belongs to same definition?
	env *adt.Environment
	id  adt.ID

	// field marks the optional conjuncts of all explicit fields.
	// Required fields are marked as empty
	fields []field

	// literal map[adt.Feature][]adt.Node

	// excluded are all literal fields that already exist.
	bulk       []bulkField
	additional []adt.Expr
	isOpen     bool // has a ...
}

func (o *fieldSet) OptionalTypes() (mask adt.OptionalType) {
	for _, f := range o.fields {
		if len(f.optional) > 0 {
			mask = adt.HasField
			break
		}
	}
	for _, b := range o.bulk {
		if b.expr == nil {
			mask |= adt.HasDynamic
		} else {
			mask |= adt.HasPattern
		}
	}
	if o.additional != nil {
		mask |= adt.HasAdditional
	}
	if o.isOpen {
		mask |= adt.IsOpen
	}
	return mask
}

type field struct {
	label    adt.Feature
	optional []adt.Node
}

type bulkField struct {
	check fieldMatcher
	expr  adt.Node // *adt.BulkOptionalField // Conjunct
}

func (o *fieldSet) Accept(c *adt.OpContext, f adt.Feature) bool {
	if len(o.additional) > 0 {
		return true
	}
	if o.fieldIndex(f) >= 0 {
		return true
	}
	for _, b := range o.bulk {
		if b.check.Match(c, f) {
			return true
		}
	}
	return false
}

// MatchAndInsert finds matching optional parts for a given Arc and adds its
// conjuncts. Bulk fields are only applied if no fields match, and additional
// constraints are only added if neither regular nor bulk fields match.
func (o *fieldSet) MatchAndInsert(c *adt.OpContext, arc *adt.Vertex) {
	env := o.env

	// Match normal fields
	p := 0
	for ; p < len(o.fields); p++ {
		if o.fields[p].label == arc.Label {
			break
		}
	}
	if p < len(o.fields) {
		for _, e := range o.fields[p].optional {
			arc.AddConjunct(adt.MakeConjunct(env, e, o.id))
		}
		return
	}

	if !arc.Label.IsRegular() {
		return
	}

	bulkEnv := *env
	bulkEnv.DynamicLabel = arc.Label

	// match bulk optional fields / pattern properties
	matched := false
	for _, f := range o.bulk {
		if f.check.Match(c, arc.Label) {
			matched = true
			if f.expr != nil {
				arc.AddConjunct(adt.MakeConjunct(&bulkEnv, f.expr, o.id))
			}
		}
	}
	if matched {
		return
	}

	// match others
	for _, x := range o.additional {
		arc.AddConjunct(adt.MakeConjunct(env, x, o.id))
	}
}

func (o *fieldSet) fieldIndex(f adt.Feature) int {
	for i := range o.fields {
		if o.fields[i].label == f {
			return i
		}
	}
	return -1
}

func (o *fieldSet) MarkField(c *adt.OpContext, x *adt.Field) {
	if o.fieldIndex(x.Label) < 0 {
		o.fields = append(o.fields, field{label: x.Label})
	}
}

func (o *fieldSet) AddOptional(c *adt.OpContext, x *adt.OptionalField) {
	p := o.fieldIndex(x.Label)
	if p < 0 {
		p = len(o.fields)
		o.fields = append(o.fields, field{label: x.Label})
	}
	o.fields[p].optional = append(o.fields[p].optional, x)
}

func (o *fieldSet) AddDynamic(c *adt.OpContext, env *adt.Environment, x *adt.DynamicField) {
	// not in bulk: count as regular field?
	o.bulk = append(o.bulk, bulkField{dynamicMatcher{env, x.Key}, nil})
}

func (o *fieldSet) AddBulk(c *adt.OpContext, x *adt.BulkOptionalField) {
	v, ok := c.Evaluate(o.env, x.Filter)
	if !ok {
		// TODO: handle dynamically
		return
	}

	if m := o.getMatcher(c, v); m != nil {
		o.bulk = append(o.bulk, bulkField{m, x})
	}
}

func (o *fieldSet) getMatcher(c *adt.OpContext, v adt.Value) fieldMatcher {
	switch f := v.(type) {
	case *adt.Top:
		return typeMatcher(adt.TopKind)

	case *adt.BasicType:
		return typeMatcher(f.K)

	default:
		return o.newPatternMatcher(c, v)
	}
}

func (o *fieldSet) AddEllipsis(c *adt.OpContext, x *adt.Ellipsis) {
	expr := x.Value
	if x.Value == nil {
		o.isOpen = true
		expr = &adt.Top{}
	}
	o.additional = append(o.additional, expr)
}

type fieldMatcher interface {
	Match(c *adt.OpContext, f adt.Feature) bool
}

type typeMatcher adt.Kind

func (m typeMatcher) Match(c *adt.OpContext, f adt.Feature) bool {
	switch f.Typ() {
	case adt.StringLabel:
		return adt.Kind(m)&adt.StringKind != 0

	case adt.IntLabel:
		return adt.Kind(m)&adt.IntKind != 0
	}
	return false
}

type dynamicMatcher struct {
	env  *adt.Environment
	expr adt.Expr
}

func (m dynamicMatcher) Match(c *adt.OpContext, f adt.Feature) bool {
	if !f.IsRegular() || !f.IsString() {
		return false
	}
	v, ok := c.Evaluate(m.env, m.expr)
	if !ok {
		return false
	}
	s, ok := v.(*adt.String)
	if !ok {
		return false
	}
	return f.SelectorString(c) == s.Str
}

type patternMatcher adt.Conjunct

func (m patternMatcher) Match(c *adt.OpContext, f adt.Feature) bool {
	v := adt.Vertex{}
	v.AddConjunct(adt.Conjunct(m))
	label := f.ToValue(c)
	v.AddConjunct(adt.MakeRootConjunct(m.Env, label))
	v.Finalize(c)
	b, _ := v.Value.(*adt.Bottom)
	return b == nil
}

func (o *fieldSet) newPatternMatcher(ctx *adt.OpContext, x adt.Value) fieldMatcher {
	c := adt.MakeRootConjunct(o.env, x)
	return patternMatcher(c)
}
