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

	// field marks the optional conjuncts of all explicit Fields.
	// Required Fields are marked as empty
	Fields []FieldInfo

	Dynamic []*adt.DynamicField

	// excluded are all literal fields that already exist.
	Bulk []bulkField

	Additional []adt.Expr
	IsOpen     bool // has a ...
}

func (o *fieldSet) OptionalTypes() (mask adt.OptionalType) {
	for _, f := range o.Fields {
		if len(f.Optional) > 0 {
			mask = adt.HasField
			break
		}
	}
	if len(o.Dynamic) > 0 {
		mask |= adt.HasDynamic
	}
	if len(o.Bulk) > 0 {
		mask |= adt.HasPattern
	}
	if o.Additional != nil {
		mask |= adt.HasAdditional
	}
	if o.IsOpen {
		mask |= adt.IsOpen
	}
	return mask
}

func (o *fieldSet) IsOptional(label adt.Feature) bool {
	for _, f := range o.Fields {
		if f.Label == label && len(f.Optional) > 0 {
			return true
		}
	}
	return false
}

type FieldInfo struct {
	Label    adt.Feature
	Optional []adt.Node
}

type bulkField struct {
	check      fieldMatcher
	expr       adt.Node // *adt.BulkOptionalField // Conjunct
	additional bool     // used with ...
}

func (o *fieldSet) Accept(c *adt.OpContext, f adt.Feature) bool {
	if len(o.Additional) > 0 {
		return true
	}
	if o.fieldIndex(f) >= 0 {
		return true
	}
	for _, d := range o.Dynamic {
		m := dynamicMatcher{d.Key}
		if m.Match(c, o.env, f) {
			return true
		}
	}
	for _, b := range o.Bulk {
		if b.check.Match(c, o.env, f) {
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
	matched := false
outer:
	for _, f := range o.Fields {
		if f.Label == arc.Label {
			for _, e := range f.Optional {
				arc.AddConjunct(adt.MakeConjunct(env, e, o.id))
			}
			matched = true
			break outer
		}
	}

	if !arc.Label.IsRegular() {
		return
	}

	bulkEnv := *env
	bulkEnv.DynamicLabel = arc.Label
	bulkEnv.Deref = nil
	bulkEnv.Cycles = nil

	// match bulk optional fields / pattern properties
	for _, f := range o.Bulk {
		if matched && f.additional {
			continue
		}
		if f.check.Match(c, o.env, arc.Label) {
			matched = true
			if f.expr != nil {
				arc.AddConjunct(adt.MakeConjunct(&bulkEnv, f.expr, o.id))
			}
		}
	}
	if matched {
		return
	}

	addEnv := *env
	addEnv.Deref = nil
	addEnv.Cycles = nil

	// match others
	for _, x := range o.Additional {
		arc.AddConjunct(adt.MakeConjunct(&addEnv, x, o.id))
	}
}

func (o *fieldSet) fieldIndex(f adt.Feature) int {
	for i := range o.Fields {
		if o.Fields[i].Label == f {
			return i
		}
	}
	return -1
}

func (o *fieldSet) MarkField(c *adt.OpContext, f adt.Feature) {
	if o.fieldIndex(f) < 0 {
		o.Fields = append(o.Fields, FieldInfo{Label: f})
	}
}

func (o *fieldSet) AddOptional(c *adt.OpContext, x *adt.OptionalField) {
	p := o.fieldIndex(x.Label)
	if p < 0 {
		p = len(o.Fields)
		o.Fields = append(o.Fields, FieldInfo{Label: x.Label})
	}
	o.Fields[p].Optional = append(o.Fields[p].Optional, x)
}

func (o *fieldSet) AddDynamic(c *adt.OpContext, x *adt.DynamicField) {
	o.Dynamic = append(o.Dynamic, x)
}

func (o *fieldSet) AddBulk(c *adt.OpContext, x *adt.BulkOptionalField) {
	v, ok := c.Evaluate(o.env, x.Filter)
	if !ok {
		// TODO: handle dynamically
		return
	}

	if m := o.getMatcher(c, v); m != nil {
		o.Bulk = append(o.Bulk, bulkField{m, x, false})
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
		o.IsOpen = true
		expr = &adt.Top{}
	}
	o.Additional = append(o.Additional, expr)
}

type fieldMatcher interface {
	Match(c *adt.OpContext, env *adt.Environment, f adt.Feature) bool
}

type typeMatcher adt.Kind

func (m typeMatcher) Match(c *adt.OpContext, env *adt.Environment, f adt.Feature) bool {
	switch f.Typ() {
	case adt.StringLabel:
		return adt.Kind(m)&adt.StringKind != 0

	case adt.IntLabel:
		return adt.Kind(m)&adt.IntKind != 0
	}
	return false
}

type dynamicMatcher struct {
	expr adt.Expr
}

func (m dynamicMatcher) Match(c *adt.OpContext, env *adt.Environment, f adt.Feature) bool {
	if !f.IsRegular() || !f.IsString() {
		return false
	}
	v, ok := c.Evaluate(env, m.expr)
	if !ok {
		return false
	}
	s, ok := v.(*adt.String)
	if !ok {
		return false
	}
	label := f.StringValue(c)
	return label == s.Str
}

type patternMatcher adt.Conjunct

func (m patternMatcher) Match(c *adt.OpContext, env *adt.Environment, f adt.Feature) bool {
	v := adt.Vertex{}
	v.AddConjunct(adt.Conjunct(m))
	label := f.ToValue(c)
	v.AddConjunct(adt.MakeRootConjunct(m.Env, label))
	v.Finalize(c)
	b, _ := v.BaseValue.(*adt.Bottom)
	return b == nil
}

func (o *fieldSet) newPatternMatcher(ctx *adt.OpContext, x adt.Value) fieldMatcher {
	c := adt.MakeRootConjunct(o.env, x)
	return patternMatcher(c)
}
