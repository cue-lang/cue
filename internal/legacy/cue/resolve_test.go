// Copyright 2018 The CUE Authors
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

package cue

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/core/adt"
)

func TestX(t *testing.T) {
	// Don't remove. For debugging.
	in := `
	`

	if strings.TrimSpace(in) == "" {
		t.Skip()
	}
}

// var traceOn = flag.Bool("debug", false, "enable tracing")

// func compileFileWithErrors(t *testing.T, body string) (*context, *structLit, error) {
// 	t.Helper()
// 	ctx, inst, err := compileInstance(t, body)
// 	return ctx, inst.root, err
// }

// func compileFile(t *testing.T, body string) (*context, *structLit) {
// 	t.Helper()
// 	ctx, inst, errs := compileInstance(t, body)
// 	if errs != nil {
// 		t.Fatal(errs)
// 	}
// 	return ctx, inst.root
// }

func compileInstance(t *testing.T, body string) (*context, *Instance, error) {
	var r Runtime
	inst, err := r.Compile("test", body)

	if err != nil {
		x := newInstance(newIndex(sharedIndex), nil, &adt.Vertex{})
		ctx := x.newContext()
		return ctx, x, err
	}

	return r.index().newContext(), inst, nil
}
