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

// MatchAndInsert finds matching optional parts for a given Arc and adds its
// conjuncts. Bulk fields are only applied if no fields match, and additional
// constraints are only added if neither regular nor bulk fields match.
func (o *StructInfo) MatchAndInsert(c *OpContext, arc *Vertex) {
	env := o.Env

	closeInfo := o.CloseInfo
	closeInfo.IsClosed = false

	// Match normal fields
	matched := false
outer:
	for _, f := range o.Fields {
		if f.Label == arc.Label {
			for _, e := range f.Optional {
				arc.AddConjunct(MakeConjunct(env, e, closeInfo))
			}
			matched = true
			break outer
		}
	}

	if !arc.Label.IsRegular() {
		return
	}

	if len(o.Bulk) > 0 {
		bulkEnv := *env
		bulkEnv.DynamicLabel = arc.Label
		bulkEnv.Deref = nil
		bulkEnv.Cycles = nil

		// match bulk optional fields / pattern properties
		for _, b := range o.Bulk {
			// if matched && f.additional {
			// 	continue
			// }
			if matchBulk(c, env, b, arc.Label) {
				matched = true
				info := closeInfo.SpawnSpan(b.Value, ConstraintSpan)
				arc.AddConjunct(MakeConjunct(&bulkEnv, b, info))
			}
		}
	}

	if matched || len(o.Additional) == 0 {
		return
	}

	addEnv := *env
	addEnv.Deref = nil
	addEnv.Cycles = nil

	// match others
	for _, x := range o.Additional {
		info := closeInfo
		if _, ok := x.(*Top); !ok {
			info = info.SpawnSpan(x, ConstraintSpan)
		}
		arc.AddConjunct(MakeConjunct(&addEnv, x, info))
	}
}

func matchBulk(c *OpContext, env *Environment, x *BulkOptionalField, f Feature) bool {
	v, ok := c.Evaluate(env, x.Filter)
	if !ok {
		// TODO: handle dynamically
		return false
	}

	m := getMatcher(c, env, v)
	if m == nil {
		return false
	}

	c.inConstraint++
	ret := m.Match(c, env, f)
	c.inConstraint--
	return ret
}

func getMatcher(c *OpContext, env *Environment, v Value) fieldMatcher {
	switch f := v.(type) {
	case *Top:
		return typeMatcher(TopKind)

	case *BasicType:
		return typeMatcher(f.K)

	default:
		return newPatternMatcher(c, env, v)
	}
}

type fieldMatcher interface {
	Match(c *OpContext, env *Environment, f Feature) bool
}

type typeMatcher Kind

func (m typeMatcher) Match(c *OpContext, env *Environment, f Feature) bool {
	switch f.Typ() {
	case StringLabel:
		return Kind(m)&StringKind != 0

	case IntLabel:
		return Kind(m)&IntKind != 0
	}
	return false
}

type dynamicMatcher struct {
	expr Expr
}

func (m dynamicMatcher) Match(c *OpContext, env *Environment, f Feature) bool {
	if !f.IsRegular() || !f.IsString() {
		return false
	}
	v, ok := c.Evaluate(env, m.expr)
	if !ok {
		return false
	}
	s, ok := v.(*String)
	if !ok {
		return false
	}
	label := f.StringValue(c)
	return label == s.Str
}

type patternMatcher Conjunct

func (m patternMatcher) Match(c *OpContext, env *Environment, f Feature) bool {
	v := Vertex{}
	v.AddConjunct(Conjunct(m))
	label := f.ToValue(c)
	v.AddConjunct(MakeRootConjunct(m.Env, label))
	v.Finalize(c)
	b, _ := v.BaseValue.(*Bottom)
	return b == nil
}

func newPatternMatcher(ctx *OpContext, env *Environment, x Value) fieldMatcher {
	c := MakeRootConjunct(env, x)
	return patternMatcher(c)
}
