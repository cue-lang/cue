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

// This file implements helpers for the out/errors.txt documentary section.

package cuetxtar

import (
	"fmt"
	"io"

	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
)

// PrintErrors writes all errors found in the vertex tree to w, each
// prefixed with [code]. Child-error markers (errors propagated from children)
// are suppressed; only the originating errors are reported.
//
// Used to populate the out/errors.txt (inline tests) and out/<name>/errors.txt
// (golden-file tests) documentary sections.
func PrintErrors(w io.Writer, v *adt.Vertex, cfg *cueerrors.Config) {
	printVertexErrorsRec(w, v, cfg)
}

func printVertexErrorsRec(w io.Writer, v *adt.Vertex, cfg *cueerrors.Config) {
	if b := v.Bottom(); b != nil {
		if !b.ChildError && b.Err != nil {
			fmt.Fprintf(w, "[%s] ", b.Code)
			cueerrors.Print(w, b.Err, cfg)
		}
		if !b.HasRecursive {
			return
		}
	}
	for _, a := range v.Arcs {
		if a.Label.IsLet() {
			continue
		}
		printVertexErrorsRec(w, a, cfg)
	}
}
