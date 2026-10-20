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

package encodingregistry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// TestRegistrationSchemaRoundTrip guards the round-trip invariant of
// the registration contract: every declarative field of the Encoding
// record is expressible as CUE data against registration-schema.cue,
// and the schema rejects the same name and extension shapes
// checkStructure rejects. The schema is a contract
// document with no other automated tie to the code, so this is the
// only check that keeps the two from drifting.
func TestRegistrationSchemaRoundTrip(t *testing.T) {
	path := filepath.Join("..", "..", "specs", "ace8n-dynamic-filetypes",
		"contracts", "registration-schema.cue")
	src, err := os.ReadFile(path)
	if err != nil {
		// The specs tree is present in the repository but not
		// necessarily in derived source distributions.
		t.Skipf("registration schema not available: %v", err)
	}
	ctx := cuecontext.New()
	schema := ctx.CompileBytes(src, cue.Filename("registration-schema.cue"))
	qt.Assert(t, qt.IsNil(schema.Err()))
	reg := schema.LookupPath(cue.ParsePath("#Registration"))
	qt.Assert(t, qt.IsNil(reg.Err()))

	validate := func(decl map[string]any) error {
		v := reg.Unify(ctx.Encode(decl))
		return v.Validate(cue.Concrete(false))
	}

	// The package documentation's headline example, expressed as data:
	// every declarative field must round-trip, including Binary.
	err = validate(map[string]any{
		"name":       "capnp",
		"extensions": []string{".capnp"},
		"binary":     true,
		"info":       map[string]any{"form": "data"},
	})
	qt.Assert(t, qt.IsNil(err))

	// A declaration using tags, boolTags, and perMode.
	err = validate(map[string]any{
		"name": "kvz",
		"info": map[string]any{"form": "schema"},
		"perMode": map[string]any{
			"export": map[string]any{"form": "data"},
		},
		"tags":     map[string]any{"lang2": map[string]any{"default": "go"}},
		"boolTags": map[string]any{"strict2": map[string]any{"default": true}},
	})
	qt.Assert(t, qt.IsNil(err))

	// The schema rejects what checkStructure rejects: qualifier
	// punctuation in names, and extensions with extra dots or path
	// separators.
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{"name": "a+b"})))
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{"name": "a:b"})))
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{"name": ""})))
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{
		"name": "zz", "extensions": []string{".tar.gz"},
	})))
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{
		"name": "zz", "extensions": []string{"./x"},
	})))
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{
		"name": "zz", "extensions": []string{"kv"},
	})))

	// #Registration is closed: an undeclared field is a drift signal.
	qt.Assert(t, qt.IsNotNil(validate(map[string]any{
		"name": "zz", "nosuchfield": true,
	})))
}
