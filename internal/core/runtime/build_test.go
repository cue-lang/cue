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

package runtime_test

import (
	"reflect"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
)

type emptyScope struct{}

func (emptyScope) Parent() compile.Scope { return nil }
func (emptyScope) Vertex() *adt.Vertex   { return &adt.Vertex{} }

// TestBuildConfigFields checks that every field of [runtime.Config], including
// those of the embedded [compile.Config], is classified as either changing
// what an instance builds to, so that [runtime.Runtime.Build] must not share
// such a build with other builds of the instance, or not.
func TestBuildConfigFields(t *testing.T) {
	r := runtime.New()
	// A non-zero value for each field which changes what an instance builds
	// to. A field added here must also be reported by Config.hasOptions.
	options := map[string]any{
		"Scope":      compile.Scope(emptyScope{}),
		"Imports":    func(*ast.Ident) string { return "" },
		"ImportPath": "mod.test/other",
	}
	// A non-zero value for each field which does not, or nil for a field
	// which cannot be set here.
	neutral := map[string]any{
		"Runtime":  r,
		"Filename": "other.cue",
		"Counts":   &stats.Counts{},
		"Config":   nil, // the embedded compile.Config, whose fields are listed
		"pkgPath":  nil, // set by the compiler from the instance's ID
	}

	newInstance := func() *build.Instance {
		b := build.NewContext().NewInstance("", nil)
		//lint:ignore SA1019 AddFile is due to be renamed, not removed
		if err := b.AddFile("a.cue", "a: 1"); err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, f := range reflect.VisibleFields(reflect.TypeFor[runtime.Config]()) {
		val, isOption := options[f.Name]
		if !isOption {
			var ok bool
			if val, ok = neutral[f.Name]; !ok {
				t.Errorf("runtime.Config field %s is not classified as changing what an instance builds to or not", f.Name)
				continue
			}
		}
		if val == nil {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			var cfg runtime.Config
			reflect.ValueOf(&cfg).Elem().FieldByIndex(f.Index).Set(reflect.ValueOf(val))

			b := newInstance()
			plain, err := r.Build(nil, b)
			if err != nil {
				t.Fatal(err)
			}
			withField, err := r.Build(&cfg, b)
			if err != nil {
				t.Fatal(err)
			}
			if shared := withField == plain; shared == isOption {
				t.Errorf("build with %s shared with a plain build: %v, want %v", f.Name, shared, !isOption)
			}
			if again, _ := r.Build(nil, b); again != plain {
				t.Errorf("plain build after a build with %s is not shared with the first", f.Name)
			}
		})
	}
}
