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

package adt_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
)

// evalSource compiles and finalizes src with the given Go context,
// returning the operation context used.
func evalSource(t *testing.T, goCtx context.Context, src string) *adt.OpContext {
	t.Helper()
	r := runtime.New()
	f, err := parser.ParseFile("cancel_test.cue", src)
	qt.Assert(t, qt.IsNil(err))
	v, cerr := compile.Files(nil, r, "test", f)
	qt.Assert(t, qt.IsNil(cerr))
	ctx := adt.New(v, &adt.Config{
		Runtime: r,
		Context: goCtx,
	})
	v.Finalize(ctx)
	return ctx
}

// expensiveSource returns CUE source whose evaluation schedules many
// thousands of tasks, ensuring the amortized cancellation poll fires.
func expensiveSource() string {
	var sb strings.Builder
	sb.WriteString("l: [")
	for i := range 30 {
		fmt.Fprintf(&sb, "%d,", i)
	}
	sb.WriteString("]\n")
	sb.WriteString("out: [for i in l for j in l for k in l {i*10000 + j*100 + k}]\n")
	return sb.String()
}

func TestCancelEvaluation(t *testing.T) {
	// A canceled context interrupts the evaluation and records the
	// cancellation on the operation context.
	goCtx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := evalSource(t, goCtx, expensiveSource())
	b := ctx.Canceled()
	qt.Assert(t, qt.IsNotNil(b))
	qt.Assert(t, qt.Equals(b.Code, adt.CanceledError))
	qt.Assert(t, qt.ErrorIs(b.Err, context.Canceled))
}

func TestNoCancelEvaluation(t *testing.T) {
	// The same evaluation completes when the context is never canceled.
	ctx := evalSource(t, context.Background(), expensiveSource())
	qt.Assert(t, qt.IsNil(ctx.Canceled()))

	// And when no Go context is configured at all.
	r := runtime.New()
	f, err := parser.ParseFile("cancel_test.cue", expensiveSource())
	qt.Assert(t, qt.IsNil(err))
	v, cerr := compile.Files(nil, r, "test", f)
	qt.Assert(t, qt.IsNil(cerr))
	octx := adt.New(v, &adt.Config{Runtime: r})
	v.Finalize(octx)
	qt.Assert(t, qt.IsNil(octx.Canceled()))
}
