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

package cuecontext_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"

	qt "github.com/go-quicktest/qt"
)

func TestInjectBasic(t *testing.T) {
	j := cuecontext.NewInjector()
	j.Allow(cuecontext.AllowAll())
	ctx := cuecontext.New(cuecontext.Inject(j))

	j.Register("example.com/foo", ctx.CompileString(`"hello"`))

	v := ctx.CompileString(`
		@extern(inject)

		package foo

		x: _ @inject(name="example.com/foo")
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	qt.Assert(t, qt.IsNil(x.Err()))

	got, err := x.String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "hello"))
}

func TestInjectUnifyMultiple(t *testing.T) {
	j := cuecontext.NewInjector()
	j.Allow(cuecontext.AllowAll())
	ctx := cuecontext.New(cuecontext.Inject(j))

	j.Register("example.com/val", ctx.CompileString(`{a: int}`))
	j.Register("example.com/val", ctx.CompileString(`{a: 42}`))

	v := ctx.CompileString(`
		@extern(inject)

		package foo

		x: _ @inject(name="example.com/val")
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	a := v.LookupPath(cue.ParsePath("x.a"))
	qt.Assert(t, qt.IsNil(a.Err()))

	got, err := a.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(42)))
}

func TestInjectMissing(t *testing.T) {
	j := cuecontext.NewInjector()
	j.Allow(cuecontext.AllowAll())
	ctx := cuecontext.New(cuecontext.Inject(j))

	v := ctx.CompileString(`
		@extern(inject)

		package foo

		x: "default" @inject(name="example.com/missing")
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	got, err := x.String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "default"))
}

func TestInjectAllowRejects(t *testing.T) {
	j := cuecontext.NewInjector()
	j.Allow(func(inst *build.Instance, name string) error {
		return fmt.Errorf("not permitted")
	})
	ctx := cuecontext.New(cuecontext.Inject(j))

	v := ctx.CompileString(`
		@extern(inject)

		package foo

		x: _ @inject(name="example.com/foo")
	`)
	qt.Assert(t, qt.IsNotNil(v.Err()))
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*not permitted.*`))
}

func TestInjectNoAllowFunction(t *testing.T) {
	j := cuecontext.NewInjector()
	// Deliberately not calling j.Allow
	ctx := cuecontext.New(cuecontext.Inject(j))

	v := ctx.CompileString(`
		@extern(inject)

		package foo

		x: _ @inject(name="example.com/foo")
	`)
	qt.Assert(t, qt.IsNotNil(v.Err()))
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*no Allow function configured.*`))
}

func TestInjectAllowNilPanics(t *testing.T) {
	j := cuecontext.NewInjector()
	qt.Assert(t, qt.PanicMatches(func() {
		j.Allow(nil)
	}, `cuecontext: Allow called with nil function`))
}

func TestInjectNoExternAttribute(t *testing.T) {
	j := cuecontext.NewInjector()
	j.Allow(cuecontext.AllowAll())
	ctx := cuecontext.New(cuecontext.Inject(j))

	j.Register("example.com/foo", ctx.CompileString(`"injected"`))

	// No @extern(inject) file-level attribute, so @inject should be ignored.
	v := ctx.CompileString(`
		package foo

		x: "original" @inject(name="example.com/foo")
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	x := v.LookupPath(cue.ParsePath("x"))
	got, err := x.String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "original"))
}

func TestInjectEmptyName(t *testing.T) {
	j := cuecontext.NewInjector()
	j.Allow(cuecontext.AllowAll())
	ctx := cuecontext.New(cuecontext.Inject(j))

	v := ctx.CompileString(`
		@extern(inject)

		package foo

		x: _ @inject(name="")
	`)
	qt.Assert(t, qt.IsNotNil(v.Err()))
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*non-empty name.*`))
}
