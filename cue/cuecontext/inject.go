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

package cuecontext

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/value"
)

// Injector allows CUE values to be injected into specific locations
// in CUE code marked with @inject attributes.
type Injector struct {
	values  map[string]cue.Value
	allowFn func(inst *build.Instance, name string) error
}

// NewInjector returns a new [Injector].
func NewInjector() *Injector {
	return &Injector{
		values: make(map[string]cue.Value),
	}
}

// Register associates a CUE value with the given injection name.
// If a value is already registered for that name, the new value
// is unified with the existing one.
func (j *Injector) Register(name string, v cue.Value) {
	if existing, ok := j.values[name]; ok {
		j.values[name] = existing.Unify(v)
	} else {
		j.values[name] = v
	}
}

// Allow sets the function used to determine whether a given injection
// is allowed. It panics if f is nil.
func (j *Injector) Allow(f func(inst *build.Instance, name string) error) {
	if f == nil {
		panic("cuecontext: Allow called with nil function")
	}
	j.allowFn = f
}

// AllowAll permits all injections.
func (j *Injector) AllowAll() {
	j.Allow(func(*build.Instance, string) error { return nil })
}

// Inject returns an [Option] that registers the given [Injector]
// as an injection for @extern(inject) attributes.
func Inject(j *Injector) Option {
	return Option{func(r *runtime.Runtime) {
		r.AddInjection(&injectInjection{injector: j})
	}}
}

type injectInjection struct {
	injector *Injector
}

func (i *injectInjection) Kind() string { return "inject" }

func (i *injectInjection) InjectorForInstance(b *build.Instance, r *runtime.Runtime) (runtime.Injector, errors.Error) {
	return &injectInjector{
		injector: i.injector,
		inst:     b,
	}, nil
}

type injectInjector struct {
	injector *Injector
	inst     *build.Instance
}

func (c *injectInjector) InjectedValue(attr *runtime.ExternAttr, scope *adt.Vertex) (adt.Expr, errors.Error) {
	a := attr.Attr
	injName, _, err := a.Lookup(0, "name")
	if err != nil {
		return nil, errors.Promote(err, "invalid @inject attribute")
	}
	if injName == "" {
		return nil, errors.Newf(a.Pos, "@inject attribute requires a non-empty name argument")
	}

	if c.injector.allowFn == nil {
		return nil, errors.Newf(a.Pos, "injection %q not allowed: no Allow function configured", injName)
	}
	if err := c.injector.allowFn(c.inst, injName); err != nil {
		return nil, errors.Newf(a.Pos, "injection %q not allowed: %v", injName, err)
	}

	v, ok := c.injector.values[injName]
	if !ok {
		return &adt.Top{}, nil
	}

	_, vertex := value.ToInternal(v)
	return vertex, nil
}
