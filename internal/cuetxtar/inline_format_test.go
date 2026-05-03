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
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/core/adt"
)

// TestFormatStructLevelError verifies that a struct whose BaseValue is a
// *adt.Bottom — i.e. the struct as a whole is erroneous — is rendered as a
// bare "_|_ @test(err, ...)" rather than "{_|_, …surviving fields…}".
//
// The successfully-evaluated child arcs of an erroneous struct often diverge
// across evaluator-ordering shifts even when the error itself is the same;
// collapsing to the bare error keeps the inline output focused on the
// semantically meaningful part and makes test assertions stable.
//
// The annotation must accompany the bare _|_ so the error message is visible
// at any nesting depth (formatValue adds it for top-level errors; writeStruct
// adds it for field-level errors).
func TestFormatStructLevelError(t *testing.T) {
	ctx := cuecontext.New()

	// A definition with a structural cycle: #T's bar references #T itself.
	// This produces a vertex whose BaseValue is *adt.Bottom while its arcs
	// (foo, bar) remain populated from the partially-evaluated body.
	v := ctx.CompileString(`
#T: {
	foo: 1
	bar: #T
}
`)
	tv := v.LookupPath(cue.ParsePath("#T"))
	if tv.Err() == nil {
		t.Fatal("expected #T to be an error (structural cycle)")
	}

	// Sanity-check the vertex shape we are exercising: BaseValue=Bottom AND
	// Arcs are populated. Without this combination we would not be testing
	// the collapse path.
	core := tv.Core().V.DerefValue()
	if _, ok := core.BaseValue.(*adt.Bottom); !ok {
		t.Fatalf("expected BaseValue=*adt.Bottom; got %T", core.BaseValue)
	}
	if len(core.Arcs) == 0 {
		t.Fatal("expected #T to have child arcs (the partially-evaluated body)")
	}

	r := &inlineRunner{}
	got := r.formatValue(tv, "")

	// Output must be a bare error, not a struct literal containing the
	// embedded _|_ alongside foo and bar.
	if strings.Contains(got, "{") {
		t.Errorf("expected struct collapsed to bare _|_; got brace in:\n%s", got)
	}
	// Output must be a single-line bare error: no multi-line struct body.
	if strings.Count(got, "\n") > 0 {
		t.Errorf("expected single-line collapsed output; got:\n%s", got)
	}
	// Surviving child fields must not leak. Their declarations would appear
	// as `foo: 1` / `bar: …`; checking for the value side excludes the
	// "#T.bar: structural cycle" substring in the error message.
	if strings.Contains(got, "foo: 1") || strings.Contains(got, "bar: #T") {
		t.Errorf("surviving child fields leaked into output; got:\n%s", got)
	}
	if !strings.HasPrefix(got, "_|_ @test(err,") {
		t.Errorf("expected output to start with `_|_ @test(err,`; got:\n%s", got)
	}
	if !strings.Contains(got, "code=structural_cycle") {
		t.Errorf("expected code=structural_cycle in annotation; got:\n%s", got)
	}
}

// TestFormatStructLevelErrorPropagates verifies the collapse fires through
// error propagation: a parent struct that contains a sub-vertex with
// BaseValue=*adt.Bottom inherits the error (cue.Value.Err walks children),
// so the parent collapses to a bare _|_ as well — surviving sibling fields
// (`good: "ok"`) are intentionally dropped from the rendered output. This
// matches the lenient comparison in cmpStruct, which skips unrelated fields
// when an embedded _|_ is present in the expected struct.
func TestFormatStructLevelErrorPropagates(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
#T: {
	foo: 1
	bar: #T
}
container: {
	good: "ok"
	bad:  #T
}
`)
	c := v.LookupPath(cue.ParsePath("container"))
	if c.Err() == nil {
		t.Fatal("expected container.Err() != nil from #T's structural cycle propagating up")
	}

	r := &inlineRunner{}
	got := r.formatValue(c, "")

	if strings.Contains(got, "{") {
		t.Errorf("expected container collapsed to bare _|_; got brace in:\n%s", got)
	}
	if strings.Contains(got, "good") || strings.Contains(got, "bad:") {
		t.Errorf("expected sibling fields suppressed by collapse; got:\n%s", got)
	}
	if !strings.HasPrefix(got, "_|_ @test(err,") {
		t.Errorf("expected output to start with `_|_ @test(err,`; got:\n%s", got)
	}
}
