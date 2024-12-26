// Copyright 2019 CUE Authors
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

package trim_test

import (
	"testing"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/tools/trim"
	trimv3 "cuelang.org/go/tools/trim/evalv3"
	"github.com/go-quicktest/qt"
)

var (
	matrix = cuetdtest.SmallMatrix
)

const trace = false

func TestTrimFiles(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "trim",
		Matrix: matrix,
	}

	test.Run(t, func(t *cuetxtar.Test) {

		a := t.Instance()
		ctx := t.CueContext()
		val := ctx.BuildInstance(a)
		// Note: don't require val.Err to be nil because there are deliberate
		// errors in some tests, to ensure trim still works even with some errors.
		hadError := val.Err() != nil

		files := a.Files

		var err error
		v, _ := (*runtime.Runtime)(ctx).Settings()
		if v == internal.DevVersion {
			err = trimv3.Files(files, a.Dir, val, &trimv3.Config{})
		} else {
			err = trim.Files(files, val, &trim.Config{Trace: trace})
		}
		if err != nil {
			t.WriteErrors(errors.Promote(err, ""))
		}

		// If the files could be built without an error before,
		// they should still build without an error after trimming.
		// This might not be true if, for example, unused imports are not removed.
		// Note that we need a new build.Instance to build the ast.Files from scratch again.
		if !hadError {
			a := build.NewContext().NewInstance("", nil)
			for _, file := range files {
				a.AddSyntax(file)
			}
			val := ctx.BuildInstance(a)
			qt.Assert(t, qt.IsNil(val.Err()))
		}

		for _, f := range files {
			t.WriteFile(f)
		}
	})
}
