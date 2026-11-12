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

package cuetxtar

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"golang.org/x/tools/txtar"
)

// EvalCounts counts how often marked fields of a test archive are evaluated.
// See [Test.CountEvals].
type EvalCounts struct {
	t      *Test
	counts map[string]int // nil if the archive marks no fields
	paused bool
}

// CountEvals lets a test archive observe how much of a configuration gets
// evaluated. In a CUE file with the file attribute @extern(count), the value
// of a field with the attribute @count() is unified with 1, and each
// evaluation of that field is counted. The counts are kept per name, which is
// the argument of the attribute, as in @count(name), or else the field's label.
//
// CountEvals sets this up for values built with ctx.
func (t *Test) CountEvals(ctx *cue.Context) *EvalCounts {
	c := &EvalCounts{t: t}
	uses := func(f txtar.File) bool {
		return bytes.Contains(f.Data, []byte("@extern(count)"))
	}
	if slices.ContainsFunc(t.Archive.Files, uses) {
		c.counts = make(map[string]int)
		(*runtime.Runtime)(ctx).AddInjection(countInjection{c})
	}
	return c
}

// Pause stops counting evaluations until the returned function is called,
// so that a test can leave out the evaluations it causes itself.
func (c *EvalCounts) Pause() (resume func()) {
	c.paused = true
	return func() { c.paused = false }
}

// Write records the counts so far, one line per name, in the output section
// with the given name. It does nothing if the archive marks no fields.
func (c *EvalCounts) Write(section string) {
	if c.counts == nil {
		return
	}
	w := c.t.Writer(section)
	for _, name := range slices.Sorted(maps.Keys(c.counts)) {
		fmt.Fprintf(w, "%s: %d\n", name, c.counts[name])
	}
}

type countInjection struct{ c *EvalCounts }

func (countInjection) Kind() string { return "count" }

func (i countInjection) InjectorForInstance(*build.Instance, *runtime.Runtime) (runtime.Injector, errors.Error) {
	return i, nil
}

func (i countInjection) InjectedValue(attr *runtime.ExternAttr, _ *adt.Vertex) (adt.Expr, errors.Error) {
	name, _ := attr.Attr.String(0)
	if f, ok := attr.Parent.(*ast.Field); ok && name == "" {
		name, _, _ = ast.LabelName(f.Label)
	}
	i.c.counts[name] += 0 // list fields which are never evaluated too
	return &adt.CallExpr{Fun: &adt.Builtin{
		Result: adt.IntKind,
		Name:   "count",
		Func: func(call adt.BuiltinCallContext) adt.Expr {
			if !i.c.paused {
				i.c.counts[name]++
			}
			return call.OpContext().NewInt64(1)
		},
	}}, nil
}
