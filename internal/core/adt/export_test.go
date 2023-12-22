// Copyright 2023 CUE Authors
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

// The functions and types in this file are use to construct test cases for
// fields_test.go and constraints_test.

// MatchPatternValue exports matchPatternValue for testing.
func MatchPatternValue(ctx *OpContext, p Value, f Feature, label Value) bool {
	return matchPatternValue(ctx, p, f, label)
}

// FieldTester is used low-level testing of field insertion. It simulates
// how the evaluator inserts fields. This allows the closedness algorithm to be
// tested independently of the underlying evaluator implementation.
type FieldTester struct {
	*OpContext
	n    *nodeContext
	cc   *closeContext
	Root *Vertex
}

func NewFieldTester(r Runtime) *FieldTester {
	v := &Vertex{}
	ctx := New(v, &Config{Runtime: r})
	n := v.getNodeContext(ctx, 1)

	return &FieldTester{
		OpContext: ctx,
		n:         n,
		cc:        v.rootCloseContext(),
		Root:      v,
	}
}

func (x *FieldTester) Error() string {
	if b, ok := x.n.node.BaseValue.(*Bottom); ok && b.Err != nil {
		return b.Err.Error()
	}
	return ""
}

type declaration func(cc *closeContext)

// Run simulates a CUE evaluation of the given declarations.
func (x *FieldTester) Run(sub ...declaration) {
	x.cc.incDependent()
	for _, s := range sub {
		s(x.cc)
	}
	x.cc.decDependent(x.n)
}

// Def represents fields that define a definition, such that
// Def(Field("a", "foo"), Field("b", "bar")) represents:
//
//	#D: {
//		a: "foo"
//		b: "bar"
//	}
//
// For some unique #D.
func (x *FieldTester) Def(sub ...declaration) declaration {
	return func(cc *closeContext) {
		ci := CloseInfo{cc: cc}
		ci, dc := ci.spawnCloseContext(closeDef)

		dc.incDependent()
		for _, sfn := range sub {
			sfn(dc)
		}
		dc.decDependent(x.n)
	}
}

// Embed represents fields embedded within the current node, such that
// Embed(Field("a", "foo"), Def(Field("b", "bar"))) represents:
//
//	{
//		{
//			a: "foo"
//			#D
//		}
//	}
//
// For some #D: b: "bar".
func (x *FieldTester) Embed(sub ...declaration) declaration {
	return func(cc *closeContext) {
		ci := CloseInfo{cc: cc}
		ci, dc := ci.spawnCloseContext(closeEmbed)

		dc.incDependent()
		for _, sfn := range sub {
			sfn(dc)
		}
		dc.decDependent(x.n)
	}
}

// EmbedDef represents fields that define a struct and embedded within the
// current node.
func (x *FieldTester) EmbedDef(sub ...declaration) declaration {
	return x.Embed(x.Def(sub...))
}

// Field defines a field declaration such that Field("a", "foo") represents
//
//	a: "foo"
//
// The value can be of type string, int64, bool, or Expr.
// Duplicate values (conjuncts) are retained as the deduplication check is
// bypassed for this.
func (x *FieldTester) Field(label string, a any) declaration {
	return x.field(label, a, false)
}

// FieldDedup is like Field, but enables conjunct deduplication.
func (x *FieldTester) FieldDedup(label string, a any) declaration {
	return x.field(label, a, true)
}

func (x *FieldTester) field(label string, a any, dedup bool) declaration {
	f := x.StringLabel(label)

	var v Expr
	switch a := a.(type) {
	case Expr:
		v = a
	case string:
		v = x.NewString(a)
	case int:
		v = x.NewInt64(int64(a))
	case bool:
		v = x.newBool(a)
	default:
		panic("type not supported")
	}

	return func(cc *closeContext) {
		var c Conjunct
		c.Env = &Environment{Vertex: x.Root}
		c.CloseInfo.cc = cc
		c.x = v
		c.CloseInfo.FromDef = cc.isDef
		c.CloseInfo.FromEmbed = cc.isEmbed

		x.n.insertArc(f, ArcMember, c, dedup)
	}
}

// Pat represents a pattern constraint, such that Pat(`<"a"`, "foo") represents
//
//	[<"a"]: "foo"
func (x *FieldTester) Pat(pattern Value, v Expr) declaration {
	if pattern == nil {
		panic("nil pattern")
	}
	if v == nil {
		panic("nil expr")
	}
	return func(cc *closeContext) {
		var c Conjunct
		c.Env = &Environment{Vertex: x.Root}
		c.CloseInfo.cc = cc
		c.x = v
		c.CloseInfo.FromDef = cc.isDef
		c.CloseInfo.FromEmbed = cc.isEmbed

		x.n.insertPattern(pattern, c)
	}
}
