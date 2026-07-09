// Copyright 2026 The CUE Authors
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

package cue_test

import (
	"testing"

	"cuelang.org/go/cue/parser"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/v2bridge"
	"github.com/go-quicktest/qt"
)

// compileValue compiles a CUE source string to a lazy cue/v2 Value,
// bootstrapping through the bridge the way a loader would.
func compileValue(t *testing.T, src string) cue.Value {
	t.Helper()
	rt := runtime.New()
	f, err := parser.ParseFile("test.cue", src)
	qt.Assert(t, qt.IsNil(err))
	v, cerr := compile.Files(nil, rt, "test", f)
	if cerr != nil {
		t.Fatalf("compile error: %v", cerr)
	}
	return v2bridge.NewVertexValue(rt, v).(cue.Value)
}
