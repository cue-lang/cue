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

package encodingregistry

import (
	"io"
	"testing"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/filetypes"
)

// TestPrevalidatedSkipsTemplateValidation is a white-box test: it asserts
// that RegisterWithoutFullValidation never calls validateInfo — the only path that
// compiles the types.cue template — while Register does, so that
// prevalidated registrations avoid that cost at startup.
func TestPrevalidatedSkipsTemplateValidation(t *testing.T) {
	stub := func(*build.File, io.Reader) (Decoder, error) { return nil, nil }

	filetypes.ResetDynamicRegistryForTesting()
	t.Cleanup(filetypes.ResetDynamicRegistryForTesting)

	before := validateInfoCalls.Load()
	if err := registerWithoutFullValidation(Encoding{
		Name:       "pcskip",
		Extensions: []string{".pcskip"},
		Info:       FileInfo{Form: "data"},
		NewDecoder: stub,
	}); err != nil {
		t.Fatalf("registerWithoutFullValidation: %v", err)
	}
	if got := validateInfoCalls.Load() - before; got != 0 {
		t.Errorf("RegisterWithoutFullValidation called validateInfo %d times; want 0 (no template compile)", got)
	}

	// Pure-Go composition validation is still applied, without falling back
	// to the CUE template when it finds an error.
	filetypes.ResetDynamicRegistryForTesting()
	before = validateInfoCalls.Load()
	if err := registerWithoutFullValidation(Encoding{
		Name:       "pcinvalid",
		Extensions: []string{".pcinvalid"},
		Info:       FileInfo{Interpretation: "jsonschema", Form: "data"},
		NewDecoder: stub,
	}); err == nil {
		t.Fatal("registerWithoutFullValidation accepted incompatible interpretation and form")
	}
	if got := validateInfoCalls.Load() - before; got != 0 {
		t.Errorf("invalid RegisterWithoutFullValidation called validateInfo %d times; want 0 (no template compile)", got)
	}

	// Register, by contrast, does validate against the template.
	filetypes.ResetDynamicRegistryForTesting()
	before = validateInfoCalls.Load()
	if err := register(Encoding{
		Name:       "pcval",
		Extensions: []string{".pcval"},
		Info:       FileInfo{Form: "data"},
		NewDecoder: stub,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got := validateInfoCalls.Load() - before; got == 0 {
		t.Errorf("Register called validateInfo 0 times; want > 0")
	}
}
