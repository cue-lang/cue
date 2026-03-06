// Copyright 2026 CUE Authors
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

package rpcfunc_test

import (
	"fmt"
	"net"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/rpcfunc"

	qt "github.com/go-quicktest/qt"
)

func newTestClientServer(s *rpcfunc.Server) *rpcfunc.Client {
	serverConn, clientConn := net.Pipe()
	go s.Serve(serverConn)
	return rpcfunc.NewClient(clientConn)
}

func TestSingleFunc(t *testing.T) {
	s := rpcfunc.NewServer()
	id := rpcfunc.RegisterFunc1(s, func(x int) (int, error) {
		return x * 2, nil
	})
	s.AddInjection("double", fmt.Sprintf("Caller(%d)", id))

	client := newTestClientServer(s)
	defer client.Close()

	j := cuecontext.NewInjector()
	j.AllowAll()
	ctx := cuecontext.New(cuecontext.Inject(j))

	err := client.RegisterAll(ctx, j)
	qt.Assert(t, qt.IsNil(err))

	v := ctx.CompileString(`
		@extern(inject)
		package p
		double: _ @inject(name="double")
		x: double(21)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(x.Err()))

	got, err := x.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(42)))
}

func TestTwoArgFunc(t *testing.T) {
	s := rpcfunc.NewServer()
	id := rpcfunc.RegisterFunc2(s, func(a, b int) (int, error) {
		return a + b, nil
	})
	s.AddInjection("math", fmt.Sprintf("add: Caller(%d)", id))

	client := newTestClientServer(s)
	defer client.Close()

	j := cuecontext.NewInjector()
	j.AllowAll()
	ctx := cuecontext.New(cuecontext.Inject(j))

	err := client.RegisterAll(ctx, j)
	qt.Assert(t, qt.IsNil(err))

	v := ctx.CompileString(`
		@extern(inject)
		package p
		math: _ @inject(name="math")
		x: math.add(3, 4)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(x.Err()))

	got, err := x.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(7)))
}

func TestMultipleFuncsOneInjection(t *testing.T) {
	s := rpcfunc.NewServer()
	addID := rpcfunc.RegisterFunc2(s, func(a, b int) (int, error) {
		return a + b, nil
	})
	mulID := rpcfunc.RegisterFunc2(s, func(a, b int) (int, error) {
		return a * b, nil
	})
	s.AddInjection("math", fmt.Sprintf("add: Caller(%d), mul: Caller(%d)", addID, mulID))

	client := newTestClientServer(s)
	defer client.Close()

	j := cuecontext.NewInjector()
	j.AllowAll()
	ctx := cuecontext.New(cuecontext.Inject(j))

	err := client.RegisterAll(ctx, j)
	qt.Assert(t, qt.IsNil(err))

	v := ctx.CompileString(`
		@extern(inject)
		package p
		math: _ @inject(name="math")
		sum: math.add(3, 4)
		product: math.mul(3, 4)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	sum := v.LookupPath(cue.ParsePath("sum"))
	got, err := sum.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(7)))

	product := v.LookupPath(cue.ParsePath("product"))
	got, err = product.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(12)))
}

func TestMultipleInjections(t *testing.T) {
	s := rpcfunc.NewServer()
	doubleID := rpcfunc.RegisterFunc1(s, func(x int) (int, error) {
		return x * 2, nil
	})
	greetID := rpcfunc.RegisterFunc1(s, func(name string) (string, error) {
		return "hello, " + name, nil
	})
	s.AddInjection("nums", fmt.Sprintf("double: Caller(%d)", doubleID))
	s.AddInjection("strs", fmt.Sprintf("greet: Caller(%d)", greetID))

	client := newTestClientServer(s)
	defer client.Close()

	j := cuecontext.NewInjector()
	j.AllowAll()
	ctx := cuecontext.New(cuecontext.Inject(j))

	err := client.RegisterAll(ctx, j)
	qt.Assert(t, qt.IsNil(err))

	v := ctx.CompileString(`
		@extern(inject)
		package p
		nums: _ @inject(name="nums")
		strs: _ @inject(name="strs")
		x: nums.double(21)
		y: strs.greet("world")
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	xVal, err := x.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(xVal, int64(42)))

	y := v.LookupPath(cue.ParsePath("y"))
	yVal, err := y.String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(yVal, "hello, world"))
}

func TestErrorPropagation(t *testing.T) {
	s := rpcfunc.NewServer()
	id := rpcfunc.RegisterFunc1(s, func(x int) (int, error) {
		return 0, fmt.Errorf("cannot process %d", x)
	})
	s.AddInjection("fail", fmt.Sprintf("Caller(%d)", id))

	client := newTestClientServer(s)
	defer client.Close()

	j := cuecontext.NewInjector()
	j.AllowAll()
	ctx := cuecontext.New(cuecontext.Inject(j))

	err := client.RegisterAll(ctx, j)
	qt.Assert(t, qt.IsNil(err))

	v := ctx.CompileString(`
		@extern(inject)
		package p
		fail: _ @inject(name="fail")
		x: fail(42)
	`)
	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNotNil(x.Err()))
	qt.Assert(t, qt.ErrorMatches(x.Err(), `.*cannot process 42.*`))
}

func TestStructArgs(t *testing.T) {
	type Point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	s := rpcfunc.NewServer()
	id := rpcfunc.RegisterFunc1(s, func(p Point) (int, error) {
		return p.X + p.Y, nil
	})
	s.AddInjection("geom", fmt.Sprintf("sumCoords: Caller(%d)", id))

	client := newTestClientServer(s)
	defer client.Close()

	j := cuecontext.NewInjector()
	j.AllowAll()
	ctx := cuecontext.New(cuecontext.Inject(j))

	err := client.RegisterAll(ctx, j)
	qt.Assert(t, qt.IsNil(err))

	v := ctx.CompileString(`
		@extern(inject)
		package p
		geom: _ @inject(name="geom")
		x: geom.sumCoords({x: 3, y: 4})
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(x.Err()))

	got, err := x.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(7)))
}
