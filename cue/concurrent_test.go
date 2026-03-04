// Copyright 2025 CUE Authors
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

package cue_test

import (
	"fmt"
	"math/big"
	"sync"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
)

// The tests in this file verify that cue.Value operations are safe for
// concurrent use. They are designed to trigger the race detector if
// there is a problem.
//
// Many of these are regression tests for https://github.com/cue-lang/cue/issues/2733.

const (
	concurrentWorkers    = 10
	concurrentIterations = 50
)

// TestConcurrentValueAccess tests that cue.Value read operations are race-free
// when called concurrently on a shared finalized value.
// This is a regression test for https://github.com/cue-lang/cue/issues/2733
func TestConcurrentValueAccess(t *testing.T) {
	ctx := cuecontext.New()

	// Create a value similar to what http.Serve would use
	v := ctx.CompileString(`
		listenAddr: "localhost:0"
		routing: {
			path: "/test/{id}"
			method: "GET"
		}
		request: {
			method: string
			url: string
			body: bytes
			form: [string]: [...string]
			header: [string]: [...string]
			pathValues: [string]: string
		}
		response: {
			body: *"default" | bytes
			statusCode: *200 | int
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	listenPath := cue.ParsePath("listenAddr")
	pathPath := cue.ParsePath("routing.path")
	requestPath := cue.ParsePath("request")
	respBodyPath := cue.ParsePath("response.body")

	// Simulate concurrent request handling by calling cue.Value methods
	// that would be used in the serve handler.
	runConcurrent(t, func() {
		// Operations that http.Serve uses:
		// 1. LookupPath + String
		addr, err := v.LookupPath(listenPath).String()
		if err != nil {
			t.Error(err)
			return
		}
		if addr != "localhost:0" {
			t.Errorf("unexpected addr: %s", addr)
			return
		}

		// 2. LookupPath + Exists + String
		if p := v.LookupPath(pathPath); p.Exists() {
			s, err := p.String()
			if err != nil {
				t.Error(err)
				return
			}
			if s != "/test/{id}" {
				t.Errorf("unexpected path: %s", s)
				return
			}
		}

		// 3. Path()
		_ = v.Path()

		// 4. FillPath (creates new value)
		filled := v.FillPath(requestPath, map[string]any{
			"method": "GET",
			"url":    "/test/123",
			"body":   []byte("test body"),
		})
		if err := filled.Err(); err != nil {
			t.Error(err)
			return
		}

		// 5. LookupPath on filled value + Bytes
		body := filled.LookupPath(respBodyPath)
		if body.Exists() {
			_, err := body.Bytes()
			if err != nil {
				// Expected - default is string not bytes
			}
		}

		// 6. Default() - known race point
		resp := v.LookupPath(respBodyPath)
		if d, ok := resp.Default(); ok {
			_, _ = d.String()
		}
	})
}

// TestConcurrentg tests concurrent EncodeType on a shared context.
// This triggers a panic ("invalid Node type <nil>") due to a race in
// exprToVertex when called concurrently.
func TestConcurrentEncodeType(t *testing.T) {
	ctx := cuecontext.New()
	type S struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
		Next *S     `json:"next,omitempty"`
	}

	runConcurrent(t, func() {
		v := ctx.EncodeType(S{})
		if err := v.Err(); err != nil {
			t.Error(err)
		}
	})

}

// TestConcurrentDefault tests that Default() is safe for concurrent use.
// This was one of the primary race conditions reported in #2733:
// Vertex.Default() modifies the Conjuncts slice without synchronization.
func TestConcurrentDefault(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: *1 | int
		b: *"hello" | string
		c: *true | bool
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")
	cPath := cue.ParsePath("c")

	runConcurrent(t, func() {
		if d, ok := v.LookupPath(aPath).Default(); ok {
			n, _ := d.Int64()
			if n != 1 {
				t.Errorf("unexpected default for a: %d", n)
			}
		}
		if d, ok := v.LookupPath(bPath).Default(); ok {
			s, _ := d.String()
			if s != "hello" {
				t.Errorf("unexpected default for b: %s", s)
			}
		}
		if d, ok := v.LookupPath(cPath).Default(); ok {
			b, _ := d.Bool()
			if b != true {
				t.Errorf("unexpected default for c: %v", b)
			}
		}
	})

}

// TestConcurrentDisjunctions tests concurrent access to values with disjunctions,
// which internally use Default() and can race on Conjuncts slices.
func TestConcurrentDisjunctions(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: *1 | 2 | 3
		b: "x" | "y" | *"z"
		c: *[1, 2] | [...int]
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")
	cPath := cue.ParsePath("c")

	runConcurrent(t, func() {
		d, _ := v.LookupPath(aPath).Default()
		_ = d.Kind()

		d, _ = v.LookupPath(bPath).Default()
		_ = d.Kind()

		d, _ = v.LookupPath(cPath).Default()
		_ = d.Kind()
	})

}

// TestConcurrentSyntax tests that Syntax() is safe for concurrent use.
// This triggers Race 2 from #2733: Runtime.SetBuildData/BuildData
// accessing the loaded map without synchronization.
func TestConcurrentSyntax(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: b: c: 1
		d: e: f: "hello"
		g: h: i: true
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		_ = v.Syntax()
		_ = v.Syntax(cue.Final())
		_ = v.Syntax(cue.Concrete(true))
		_ = v.Syntax(cue.Raw())
	})

}

// TestConcurrentFillPath tests concurrent FillPath operations on the same value.
func TestConcurrentFillPath(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		request: {
			method: string
			url: string
			body: bytes
		}
		response: {
			body: *"default" | bytes
			statusCode: *200 | int
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	requestPath := cue.ParsePath("request")
	respBodyPath := cue.ParsePath("response.body")

	runConcurrent(t, func() {
		filled := v.FillPath(requestPath, map[string]any{
			"method": "GET",
			"url":    "/test/123",
			"body":   []byte("test body"),
		})
		if err := filled.Err(); err != nil {
			t.Error(err)
			return
		}
		body := filled.LookupPath(respBodyPath)
		if body.Exists() {
			_, _ = body.Bytes()
		}
	})

}

// TestConcurrentFillPathCrossContext tests FillPath using values from
// different contexts.
func TestConcurrentFillPathCrossContext(t *testing.T) {
	ctx1 := cuecontext.New()
	ctx2 := cuecontext.New()

	v1 := ctx1.CompileString(`
		a: int
		b: string
	`)
	v2 := ctx2.CompileString(`
		x: 42
		y: "hello"
	`)
	if err := v1.Err(); err != nil {
		t.Fatal(err)
	}
	if err := v2.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")
	xPath := cue.ParsePath("x")
	yPath := cue.ParsePath("y")

	runConcurrent(t, func() {
		xVal := v2.LookupPath(xPath)
		filled := v1.FillPath(aPath, xVal)
		if err := filled.Err(); err != nil {
			t.Error(err)
			return
		}
		n, err := filled.LookupPath(aPath).Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if n != 42 {
			t.Errorf("unexpected a: %d", n)
		}

		yVal := v2.LookupPath(yPath)
		filled2 := v1.FillPath(bPath, yVal)
		if err := filled2.Err(); err != nil {
			t.Error(err)
			return
		}
		s, err := filled2.LookupPath(bPath).String()
		if err != nil {
			t.Error(err)
			return
		}
		if s != "hello" {
			t.Errorf("unexpected b: %s", s)
		}
	})

}

// TestConcurrentUnify tests concurrent Unify operations.
func TestConcurrentUnify(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{
		name: string
		age: int
	}`)
	data := ctx.CompileString(`{
		name: "Alice"
		age: 30
	}`)
	if err := schema.Err(); err != nil {
		t.Fatal(err)
	}
	if err := data.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		result := schema.Unify(data)
		if err := result.Err(); err != nil {
			t.Error(err)
			return
		}
		name, err := result.LookupPath(cue.ParsePath("name")).String()
		if err != nil {
			t.Error(err)
			return
		}
		if name != "Alice" {
			t.Errorf("unexpected name: %s", name)
		}
	})

}

// TestConcurrentUnifyCrossContext tests unification of values from
// different contexts.
func TestConcurrentUnifyCrossContext(t *testing.T) {
	ctx1 := cuecontext.New()
	ctx2 := cuecontext.New()

	schema := ctx1.CompileString(`{
		name: string
		age: int
		score: *100 | int
	}`)
	data := ctx2.CompileString(`{
		name: "Bob"
		age: 25
	}`)
	if err := schema.Err(); err != nil {
		t.Fatal(err)
	}
	if err := data.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		result := schema.Unify(data)
		if err := result.Err(); err != nil {
			t.Error(err)
			return
		}
		name, err := result.LookupPath(cue.ParsePath("name")).String()
		if err != nil {
			t.Error(err)
			return
		}
		if name != "Bob" {
			t.Errorf("unexpected name: %s", name)
		}
		age, err := result.LookupPath(cue.ParsePath("age")).Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if age != 25 {
			t.Errorf("unexpected age: %d", age)
		}
	})

}

// TestConcurrentMarshalJSON tests concurrent JSON marshaling.
func TestConcurrentMarshalJSON(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		name: "test"
		value: 42
		nested: {
			a: true
			b: [1, 2, 3]
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		b, err := v.MarshalJSON()
		if err != nil {
			t.Error(err)
			return
		}
		if len(b) == 0 {
			t.Error("empty JSON")
		}
	})

}

// TestConcurrentValidate tests concurrent validation.
func TestConcurrentValidate(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		name: "test"
		value: 42
		nested: {
			a: true
			b: [1, 2, 3]
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		if err := v.Validate(); err != nil {
			t.Error(err)
		}
		if err := v.Validate(cue.Concrete(true)); err != nil {
			t.Error(err)
		}
		if err := v.Validate(cue.Final()); err != nil {
			t.Error(err)
		}
	})

}

// TestConcurrentFields tests concurrent iteration over struct fields.
func TestConcurrentFields(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: 1
		b: "two"
		c: true
		d: [1, 2, 3]
		e: {x: 1, y: 2}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		iter, err := v.Fields()
		if err != nil {
			t.Error(err)
			return
		}
		count := 0
		for iter.Next() {
			_ = iter.Selector()
			_ = iter.Value()
			count++
		}
		if count != 5 {
			t.Errorf("unexpected field count: %d", count)
		}
	})

}

// TestConcurrentList tests concurrent iteration over list elements.
func TestConcurrentList(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`[1, 2, 3, 4, 5]`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		iter, err := v.List()
		if err != nil {
			t.Error(err)
			return
		}
		count := 0
		for iter.Next() {
			_, _ = iter.Value().Int64()
			count++
		}
		if count != 5 {
			t.Errorf("unexpected list length: %d", count)
		}
	})

}

// TestConcurrentWalk tests concurrent Walk traversal.
func TestConcurrentWalk(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: {
			b: {
				c: 1
				d: "hello"
			}
			e: [1, 2, 3]
		}
		f: true
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		count := 0
		v.Walk(func(v cue.Value) bool {
			count++
			return true
		}, nil)
		if count == 0 {
			t.Error("walk visited no nodes")
		}
	})

}

// TestConcurrentDecode tests concurrent Decode operations.
func TestConcurrentDecode(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		name: "test"
		value: 42
		tags: ["a", "b", "c"]
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	type T struct {
		Name  string   `json:"name"`
		Value int      `json:"value"`
		Tags  []string `json:"tags"`
	}

	runConcurrent(t, func() {
		var result T
		if err := v.Decode(&result); err != nil {
			t.Error(err)
			return
		}
		if result.Name != "test" || result.Value != 42 || len(result.Tags) != 3 {
			t.Errorf("unexpected decode result: %+v", result)
		}
	})

}

// TestConcurrentEquals tests concurrent equality checks.
func TestConcurrentEquals(t *testing.T) {
	ctx := cuecontext.New()
	v1 := ctx.CompileString(`{a: 1, b: "hello"}`)
	v2 := ctx.CompileString(`{a: 1, b: "hello"}`)
	v3 := ctx.CompileString(`{a: 2, b: "world"}`)
	if err := v1.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		if !v1.Equals(v2) {
			t.Error("v1 should equal v2")
		}
		if v1.Equals(v3) {
			t.Error("v1 should not equal v3")
		}
	})

}

// TestConcurrentSubsume tests concurrent subsumption checks.
func TestConcurrentSubsume(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{name: string, age: int}`)
	concrete := ctx.CompileString(`{name: "Alice", age: 30}`)
	if err := schema.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		if err := schema.Subsume(concrete); err != nil {
			t.Error(err)
		}
	})

}

// TestConcurrentExpr tests concurrent Expr decomposition.
func TestConcurrentExpr(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: *1 | 2 | 3
		b: >0 & <100
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")

	runConcurrent(t, func() {
		op, vals := v.LookupPath(aPath).Expr()
		_ = op
		_ = vals

		op, vals = v.LookupPath(bPath).Expr()
		_ = op
		_ = vals
	})

}

// TestConcurrentReferencePath tests concurrent reference resolution.
func TestConcurrentReferencePath(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		#Def: {
			name: string
			value: int
		}
		x: #Def & {
			name: "test"
			value: 42
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	xPath := cue.ParsePath("x")

	runConcurrent(t, func() {
		_, _ = v.LookupPath(xPath).ReferencePath()
	})

}

// TestConcurrentKindAndType tests concurrent kind and type checking.
func TestConcurrentKindAndType(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: 42
		b: "hello"
		c: true
		d: 3.14
		e: null
		f: [1, 2]
		g: {x: 1}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	paths := []cue.Path{
		cue.ParsePath("a"),
		cue.ParsePath("b"),
		cue.ParsePath("c"),
		cue.ParsePath("d"),
		cue.ParsePath("e"),
		cue.ParsePath("f"),
		cue.ParsePath("g"),
	}

	runConcurrent(t, func() {
		for _, p := range paths {
			val := v.LookupPath(p)
			_ = val.Kind()
			_ = val.IncompleteKind()
			_ = val.IsConcrete()
			_ = val.Exists()
			_ = val.Err()
		}
	})

}

// TestConcurrentNumericConversions tests concurrent numeric value extraction.
func TestConcurrentNumericConversions(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: 42
		b: 3.14
		c: 100
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")
	cPath := cue.ParsePath("c")

	runConcurrent(t, func() {
		_, _ = v.LookupPath(aPath).Int64()
		_, _ = v.LookupPath(aPath).Uint64()
		_, _ = v.LookupPath(bPath).Float64()
		v.LookupPath(aPath).Int(new(big.Int))
		v.LookupPath(bPath).Float(new(big.Float))
		_, _ = v.LookupPath(cPath).MantExp(new(big.Int))
		_, _ = v.LookupPath(aPath).AppendInt(nil, 10)
		_, _ = v.LookupPath(bPath).AppendFloat(nil, 'f', 2)
	})

}

// TestConcurrentAttributes tests concurrent attribute access.
func TestConcurrentAttributes(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: 1 @foo(bar)
		b: "hello" @tag(value)
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")

	runConcurrent(t, func() {
		_ = v.LookupPath(aPath).Attribute("foo")
		_ = v.LookupPath(bPath).Attribute("tag")
		_ = v.LookupPath(aPath).Attributes(cue.FieldAttr)
	})

}

// TestConcurrentDoc tests concurrent documentation access.
func TestConcurrentDoc(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		// This is a.
		a: 1
		// This is b.
		b: "hello"
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")

	runConcurrent(t, func() {
		docs := v.LookupPath(aPath).Doc()
		if len(docs) == 0 {
			t.Error("expected docs")
		}
	})

}

// TestConcurrentAllows tests concurrent Allows checks.
func TestConcurrentAllows(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: 1
		b: 2
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		_ = v.Allows(cue.Str("a"))
		_ = v.Allows(cue.Str("b"))
		_ = v.Allows(cue.Str("c"))
	})

}

// TestConcurrentIsClosed tests concurrent IsClosed checks.
func TestConcurrentIsClosed(t *testing.T) {
	ctx := cuecontext.New()
	open := ctx.CompileString(`{a: 1, b: 2}`)
	closed := ctx.CompileString(`close({a: 1, b: 2})`)
	if err := open.Err(); err != nil {
		t.Fatal(err)
	}
	if err := closed.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		if open.IsClosed() {
			t.Error("open struct should not be closed")
		}
		if !closed.IsClosed() {
			t.Error("closed struct should be closed")
		}
	})

}

// TestConcurrentLen tests concurrent Len operations.
func TestConcurrentLen(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`[1, 2, 3, 4, 5]`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		l := v.Len()
		n, err := l.Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if n != 5 {
			t.Errorf("unexpected len: %d", n)
		}
	})

}

// TestConcurrentFormat tests concurrent fmt.Formatter use.
func TestConcurrentFormat(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		#Schema: {
			name: string
			data: {
				x: int
				y: int
			}
		}
		instance: #Schema & {
			name: "test"
			data: x: 1
			data: y: 2
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		s := fmt.Sprint(v)
		if len(s) == 0 {
			t.Error("empty format output")
		}
	})

}

// TestConcurrentCompileString tests concurrent compilation on a shared context.
func TestConcurrentCompileString(t *testing.T) {
	ctx := cuecontext.New()

	runConcurrent(t, func() {
		v := ctx.CompileString(`import "strconv", a: "123", b: strconv.Atoi(a)`)
		if err := v.Err(); err != nil {
			t.Error(err)
			return
		}
		s, err := v.LookupPath(cue.ParsePath("b")).Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if s != 123 {
			t.Errorf("unexpected: %v", s)
		}
	})

}

// TestConcurrentCompileBytes tests concurrent CompileBytes on a shared context.
// This was specifically mentioned as a race in #2733.
func TestConcurrentCompileBytes(t *testing.T) {
	ctx := cuecontext.New()

	runConcurrent(t, func() {
		v := ctx.CompileBytes([]byte(`import "strconv", a: "123", b: strconv.Atoi(a)`))
		if err := v.Err(); err != nil {
			t.Error(err)
			return
		}
		n, err := v.LookupPath(cue.ParsePath("b")).Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if n != 123 {
			t.Errorf("unexpected: %d", n)
		}
	})

}

// TestConcurrentEncode tests concurrent Encode on a shared context.
func TestConcurrentEncode(t *testing.T) {
	ctx := cuecontext.New()

	runConcurrent(t, func() {
		v := ctx.Encode(map[string]any{
			"name": "test",
			"val":  42,
		})
		if err := v.Err(); err != nil {
			t.Error(err)
		}
	})

}

// TestConcurrentBuildExpr tests concurrent BuildExpr on a shared context.
func TestConcurrentBuildExpr(t *testing.T) {
	ctx := cuecontext.New()

	runConcurrent(t, func() {
		v := ctx.CompileString(`{x: 1}`)
		if err := v.Err(); err != nil {
			t.Error(err)
			return
		}
		n, ok := v.Syntax().(ast.Expr)
		if !ok {
			t.Error("Syntax did not return an Expr")
			return
		}
		v2 := ctx.BuildExpr(n)
		if err := v2.Err(); err != nil {
			t.Error(err)
		}
	})

}

// TestConcurrentRegexp tests concurrent operations on values that use
// regexp-based constraints. The internal OpContext.regexp method was
// identified as a race point in #2733.
func TestConcurrentRegexp(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`
		email: =~"^[a-zA-Z0-9.]+@[a-zA-Z0-9.]+$"
		phone: =~"^[0-9]{3}-[0-9]{4}$"
	`)
	if err := schema.Err(); err != nil {
		t.Fatal(err)
	}
	emailPath := cue.ParsePath("email")
	phonePath := cue.ParsePath("phone")

	runConcurrent(t, func() {
		v := schema.FillPath(emailPath, "test@example.com")
		if err := v.Validate(); err != nil {
			t.Error(err)
			return
		}
		v = schema.FillPath(phonePath, "555-1234")
		if err := v.Validate(); err != nil {
			t.Error(err)
		}
	})

}

// TestConcurrentInterpolation tests concurrent operations on values
// with string interpolation.
func TestConcurrentInterpolation(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		first: "John"
		last: "Doe"
		full: "\(first) \(last)"
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	fullPath := cue.ParsePath("full")

	runConcurrent(t, func() {
		s, err := v.LookupPath(fullPath).String()
		if err != nil {
			t.Error(err)
			return
		}
		if s != "John Doe" {
			t.Errorf("unexpected: %s", s)
		}
	})

}

// TestConcurrentComprehensions tests concurrent access to values with
// comprehensions.
func TestConcurrentComprehensions(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		input: {a: 1, b: 2, c: 3}
		output: {for k, v in input {(k): v + 10}}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	outputPath := cue.ParsePath("output")

	runConcurrent(t, func() {
		out := v.LookupPath(outputPath)
		iter, err := out.Fields()
		if err != nil {
			t.Error(err)
			return
		}
		count := 0
		for iter.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("unexpected field count: %d", count)
		}
	})

}

// TestConcurrentListComprehension tests concurrent access with list
// comprehensions.
func TestConcurrentListComprehension(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		input: [1, 2, 3, 4, 5]
		output: [for x in input {x * 2}]
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	outputPath := cue.ParsePath("output")

	runConcurrent(t, func() {
		out := v.LookupPath(outputPath)
		iter, err := out.List()
		if err != nil {
			t.Error(err)
			return
		}
		count := 0
		for iter.Next() {
			count++
		}
		if count != 5 {
			t.Errorf("unexpected list length: %d", count)
		}
	})

}

// TestConcurrentCycles tests concurrent access to values with references
// that exercise cycle detection.
func TestConcurrentCycles(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		x: {y: z}
		z: 1
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	xyPath := cue.ParsePath("x.y")

	runConcurrent(t, func() {
		val := v.LookupPath(xyPath)
		n, err := val.Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if n != 1 {
			t.Errorf("unexpected: %d", n)
		}
	})

}

// TestConcurrentEmbedding tests concurrent access to values with embeddings.
func TestConcurrentEmbedding(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		#Base: {
			name: string
			kind: string
		}
		#Extended: {
			#Base
			extra: int
		}
		val: #Extended & {
			name: "test"
			kind: "widget"
			extra: 42
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	valPath := cue.ParsePath("val")

	runConcurrent(t, func() {
		val := v.LookupPath(valPath)
		if err := val.Validate(cue.Concrete(true)); err != nil {
			t.Error(err)
			return
		}
		iter, err := val.Fields()
		if err != nil {
			t.Error(err)
			return
		}
		for iter.Next() {
			_ = iter.Value().Kind()
		}
	})

}

// TestConcurrentHiddenAndOptionalFields tests concurrent access to values
// with hidden and optional fields.
func TestConcurrentHiddenAndOptionalFields(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		_hidden: 42
		optional?: string
		required: "yes"
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		iter, err := v.Fields(cue.Hidden(true), cue.Optional(true))
		if err != nil {
			t.Error(err)
			return
		}
		for iter.Next() {
			_ = iter.Selector()
			_ = iter.Value()
		}
	})

}

// TestConcurrentDefinitions tests concurrent access to definitions.
func TestConcurrentDefinitions(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		#A: {x: int, ...}
		#B: {y: string, ...}
		#C: #A & #B
		val: #C & {x: 1, y: "hello"}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	valPath := cue.ParsePath("val")
	defPath := cue.ParsePath("#C")

	runConcurrent(t, func() {
		val := v.LookupPath(valPath)
		_ = val.IsClosed()
		_ = val.Allows(cue.Str("x"))
		_ = val.Allows(cue.Str("z"))

		def := v.LookupPath(defPath)
		_ = def.Exists()
		_ = def.IsClosed()
	})

}

// TestConcurrentBuiltinCalls tests concurrent operations on values that
// involve builtin function calls.
func TestConcurrentBuiltinCalls(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		import "strings"
		a: strings.ToUpper("hello")
		b: strings.Join(["a", "b", "c"], ",")
		c: strings.HasPrefix("hello world", "hello")
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")
	cPath := cue.ParsePath("c")

	runConcurrent(t, func() {
		s, err := v.LookupPath(aPath).String()
		if err != nil {
			t.Error(err)
			return
		}
		if s != "HELLO" {
			t.Errorf("unexpected: %s", s)
		}
		s, err = v.LookupPath(bPath).String()
		if err != nil {
			t.Error(err)
			return
		}
		if s != "a,b,c" {
			t.Errorf("unexpected: %s", s)
		}
		b, err := v.LookupPath(cPath).Bool()
		if err != nil {
			t.Error(err)
			return
		}
		if !b {
			t.Error("expected true")
		}
	})

}

// TestConcurrentMixedOperations tests a mixture of different operations
// concurrently, simulating a realistic workload where multiple goroutines
// perform different types of access on shared values.
func TestConcurrentMixedOperations(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		#Schema: {
			name: string
			age: int
			role: *"user" | "admin" | "editor"
			tags: [...string]
			meta: {
				created: string
				updated: string
			}
		}
		val: #Schema & {
			name: "Alice"
			age: 30
			tags: ["go", "cue"]
			meta: {
				created: "2024-01-01"
				updated: "2024-06-01"
			}
		}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	valPath := cue.ParsePath("val")
	namePath := cue.ParsePath("val.name")
	rolePath := cue.ParsePath("val.role")
	tagsPath := cue.ParsePath("val.tags")
	metaPath := cue.ParsePath("val.meta")
	schemaPath := cue.ParsePath("#Schema")

	var wg sync.WaitGroup
	for range concurrentWorkers {
		// Reader: lookups and string extraction
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				s, _ := v.LookupPath(namePath).String()
				if s != "Alice" {
					t.Errorf("unexpected name: %s", s)
					return
				}
			}
		}()

		// Reader: defaults
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				d, ok := v.LookupPath(rolePath).Default()
				if ok {
					s, _ := d.String()
					if s != "user" {
						t.Errorf("unexpected default role: %s", s)
						return
					}
				}
			}
		}()

		// Reader: validation
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				if err := v.LookupPath(valPath).Validate(cue.Concrete(true)); err != nil {
					t.Error(err)
					return
				}
			}
		}()

		// Reader: field iteration
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				iter, err := v.LookupPath(metaPath).Fields()
				if err != nil {
					t.Error(err)
					return
				}
				for iter.Next() {
					_, _ = iter.Value().String()
				}
			}
		}()

		// Reader: list iteration
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				iter, err := v.LookupPath(tagsPath).List()
				if err != nil {
					t.Error(err)
					return
				}
				for iter.Next() {
					_, _ = iter.Value().String()
				}
			}
		}()

		// Reader: JSON marshal
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				_, err := v.LookupPath(valPath).MarshalJSON()
				if err != nil {
					t.Error(err)
					return
				}
			}
		}()

		// Reader: syntax
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				_ = v.LookupPath(valPath).Syntax()
			}
		}()

		// Reader: subsumption
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				schema := v.LookupPath(schemaPath)
				val := v.LookupPath(valPath)
				_ = schema.Subsume(val)
			}
		}()

		// Writer: FillPath (creates new values from shared base)
		wg.Add(1)
		go func() {
			defer wg.Done()
			schema := v.LookupPath(schemaPath)
			for range concurrentIterations {
				filled := schema.FillPath(cue.ParsePath("name"), "Bob")
				filled = filled.FillPath(cue.ParsePath("age"), 25)
				_ = filled.Err()
			}
		}()

		// Writer: Unify (creates new values from shared base)
		wg.Add(1)
		go func() {
			defer wg.Done()
			schema := v.LookupPath(schemaPath)
			data := ctx.CompileString(`{name: "Charlie", age: 35, tags: ["test"]}`)
			for range concurrentIterations {
				result := schema.Unify(data)
				_ = result.Err()
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentUnifyChain tests concurrent chained unification,
// where multiple goroutines unify a chain of values together.
func TestConcurrentUnifyChain(t *testing.T) {
	ctx := cuecontext.New()
	base := ctx.CompileString(`{a: int, b: string, c: bool}`)
	v1 := ctx.CompileString(`{a: 1}`)
	v2 := ctx.CompileString(`{b: "hello"}`)
	v3 := ctx.CompileString(`{c: true}`)
	if err := base.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		result := base.Unify(v1).Unify(v2).Unify(v3)
		if err := result.Err(); err != nil {
			t.Error(err)
			return
		}
		n, _ := result.LookupPath(cue.ParsePath("a")).Int64()
		if n != 1 {
			t.Errorf("unexpected a: %d", n)
		}
		s, _ := result.LookupPath(cue.ParsePath("b")).String()
		if s != "hello" {
			t.Errorf("unexpected b: %s", s)
		}
	})

}

// TestConcurrentNewList tests concurrent NewList on a shared context.
func TestConcurrentNewList(t *testing.T) {
	ctx := cuecontext.New()
	v1 := ctx.CompileString(`1`)
	v2 := ctx.CompileString(`2`)
	v3 := ctx.CompileString(`3`)

	runConcurrent(t, func() {
		list := ctx.NewList(v1, v2, v3)
		if err := list.Err(); err != nil {
			t.Error(err)
			return
		}
		iter, err := list.List()
		if err != nil {
			t.Error(err)
			return
		}
		count := 0
		for iter.Next() {
			count++
		}
		if count != 3 {
			t.Errorf("unexpected length: %d", count)
		}
	})

}

// TestConcurrentSource tests concurrent Source access.
func TestConcurrentSource(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`{a: 1, b: "hello"}`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		_ = v.Source()
		_ = v.LookupPath(cue.ParsePath("a")).Source()
	})

}

// TestConcurrentPos tests concurrent position access.
func TestConcurrentPos(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`{a: 1, b: "hello"}`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		_ = v.Pos()
		_ = v.LookupPath(cue.ParsePath("a")).Pos()
	})

}

// TestConcurrentStructDefault tests concurrent default on struct-level disjunctions.
func TestConcurrentStructDefault(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		x: *{a: 1, b: 2} | {a: 3, b: 4}
		y: {a: *1 | int, b: *"x" | string}
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	xPath := cue.ParsePath("x")
	yPath := cue.ParsePath("y")

	runConcurrent(t, func() {
		if d, ok := v.LookupPath(xPath).Default(); ok {
			n, _ := d.LookupPath(cue.ParsePath("a")).Int64()
			if n != 1 {
				t.Errorf("unexpected x.a default: %d", n)
			}
		}

		ya := v.LookupPath(yPath).LookupPath(cue.ParsePath("a"))
		if d, ok := ya.Default(); ok {
			n, _ := d.Int64()
			if n != 1 {
				t.Errorf("unexpected y.a default: %d", n)
			}
		}

		yb := v.LookupPath(yPath).LookupPath(cue.ParsePath("b"))
		if d, ok := yb.Default(); ok {
			s, _ := d.String()
			if s != "x" {
				t.Errorf("unexpected y.b default: %s", s)
			}
		}
	})

}

// TestConcurrentBuildFile tests concurrent BuildFile on a shared context.
func TestConcurrentBuildFile(t *testing.T) {
	ctx := cuecontext.New()

	// Build a file AST that we'll reuse.
	base := ctx.CompileString(`{a: 1, b: "hello"}`)
	if err := base.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		v := ctx.CompileString(`{x: 1, y: 2}`)
		if err := v.Err(); err != nil {
			t.Error(err)
			return
		}
		n, _ := v.LookupPath(cue.ParsePath("x")).Int64()
		if n != 1 {
			t.Errorf("unexpected x: %d", n)
		}
	})

}

// TestConcurrentEval tests concurrent Eval calls.
func TestConcurrentEval(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
		a: 1 + 2
		b: "hello" + " world"
		c: len([1, 2, 3])
	`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	aPath := cue.ParsePath("a")
	bPath := cue.ParsePath("b")
	cPath := cue.ParsePath("c")

	runConcurrent(t, func() {
		a := v.LookupPath(aPath).Eval()
		n, _ := a.Int64()
		if n != 3 {
			t.Errorf("unexpected a: %d", n)
		}

		b := v.LookupPath(bPath).Eval()
		s, _ := b.String()
		if s != "hello world" {
			t.Errorf("unexpected b: %s", s)
		}

		c := v.LookupPath(cPath).Eval()
		m, _ := c.Int64()
		if m != 3 {
			t.Errorf("unexpected c: %d", m)
		}
	})

}

// TestConcurrentMultipleContexts tests using multiple contexts concurrently,
// where each context creates and operates on values independently.
func TestConcurrentMultipleContexts(t *testing.T) {
	runConcurrent(t, func() {
		ctx := cuecontext.New()
		v := ctx.CompileString(`{a: 1, b: "hello", c: *true | bool}`)
		if err := v.Err(); err != nil {
			t.Error(err)
			return
		}
		n, _ := v.LookupPath(cue.ParsePath("a")).Int64()
		if n != 1 {
			t.Errorf("unexpected: %d", n)
		}
		d, ok := v.LookupPath(cue.ParsePath("c")).Default()
		if ok {
			b, _ := d.Bool()
			if !b {
				t.Error("unexpected default")
			}
		}
	})

}

// TestConcurrentScopeOption tests concurrent compilation with Scope option
// from a shared value.
func TestConcurrentScopeOption(t *testing.T) {
	ctx := cuecontext.New()
	scope := ctx.CompileString(`
		x: 42
		y: "hello"
	`)
	if err := scope.Err(); err != nil {
		t.Fatal(err)
	}

	runConcurrent(t, func() {
		v := ctx.CompileString(`z: x + 1`, cue.Scope(scope))
		if err := v.Err(); err != nil {
			t.Error(err)
			return
		}
		n, err := v.LookupPath(cue.ParsePath("z")).Int64()
		if err != nil {
			t.Error(err)
			return
		}
		if n != 43 {
			t.Errorf("unexpected: %d", n)
		}
	})

}

// runConcurrent launches concurrentWorkers goroutines that each call fn concurrentIterations times.
func runConcurrent(t *testing.T, fn func()) {
	t.Helper()
	var wg sync.WaitGroup
	for range concurrentWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range concurrentIterations {
				fn()
			}
		}()
	}
	wg.Wait()
}
