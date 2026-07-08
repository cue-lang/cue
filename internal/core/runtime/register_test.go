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
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
)

// TestRegisterPackage checks that a package registered on a runtime
// outside the build.Instance mechanism can be imported and evaluated,
// provided the compiler is told the import path is known.
func TestRegisterPackage(t *testing.T) {
	r := runtime.New()

	// Compile and finalize the package to be registered.
	const pkgPath = "example.com/answer"
	pf, err := parser.ParseFile("answer.cue", "package answer\n\nAnswer: 42\n")
	qt.Assert(t, qt.IsNil(err))
	pv, cerr := compile.Files(nil, r, pkgPath, pf)
	qt.Assert(t, qt.IsNil(cerr))
	pctx := adt.New(pv, &adt.Config{Runtime: r})
	pv.Finalize(pctx)
	qt.Assert(t, qt.IsNil(pv.Err(pctx)))
	r.RegisterPackage(pkgPath, pv)

	// Without KnownImport, the import does not compile.
	mf, err := parser.ParseFile("main.cue", `
import "example.com/answer"

x: answer.Answer
`)
	qt.Assert(t, qt.IsNil(err))
	_, cerr = compile.Files(nil, r, "main", mf)
	qt.Assert(t, qt.ErrorMatches(cerr, `.*: import "example\.com/answer" not found`))

	// With KnownImport, the import compiles and evaluates through the
	// registered package.
	cfg := &compile.Config{
		KnownImport: func(path string) bool { return path == pkgPath },
	}
	mv, cerr := compile.Files(cfg, r, "main", mf)
	qt.Assert(t, qt.IsNil(cerr))
	ctx := adt.New(mv, &adt.Config{Runtime: r})
	mv.Finalize(ctx)
	qt.Assert(t, qt.IsNil(mv.Err(ctx)))

	x := mv.Lookup(adt.MakeStringLabel(r, "x"))
	qt.Assert(t, qt.IsNotNil(x))
	num, ok := x.Value().(*adt.Num)
	qt.Assert(t, qt.IsTrue(ok))
	i, err := num.X.Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(i, int64(42)))
}
