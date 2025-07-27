// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func closeAll(v cue.Value) cue.Value {
	path := cue.MakePath(cue.Def("#x"))
	return v.Context().CompileString("#x: _").FillPath(path, v).LookupPath(path)
}

type editConfig struct {
	Edits []cue.Value `json:"edits,omitempty"`
}

// Modifying values can cause a mix of non-finalized nodes as children of
// a finalized parent. Ensure that this does not cause issues.
func TestAPIModifyingValues(t *testing.T) {
	ctx := cuecontext.New()

	v := ctx.CompileString(`
        edits: [{
            type: "replace"
            data!: "^wrong"
        }]
    `)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	closed := closeAll(ctx.EncodeType(editConfig{}))

	v = v.Unify(closed)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	var cfg editConfig
	if err := v.Decode(&cfg); err != nil {
		t.Fatal(err)
	}

	w := ctx.CompileString(`
        type!: "fill" | "replace"
        data!: _
    `)

	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	x := cfg.Edits[0]
	x = x.Unify(w)

	if err := x.Err(); err != nil {
		t.Fatal(err)
	}
}
