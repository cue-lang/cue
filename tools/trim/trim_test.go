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

package trim

import (
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/cuetdtest"
	"cuelang.org/go/internal/cuetxtar"
)

var (
	// TODO(evalv3): many broken tests in new evaluator, use FullMatrix to
	// expose. This is probably due to the changed underlying representation.
	// matrix = cuetdtest.FullMatrix
	matrix = cuetdtest.DefaultOnlyMatrix
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
		ctx := t.Context()
		val := ctx.BuildInstance(a)
		// Note: don't check val.Err because there are deliberate
		// errors in some tests.

		files := a.Files

		err := Files(files, val, &Config{Trace: trace})
		if err != nil {
			t.WriteErrors(errors.Promote(err, ""))
		}

		for _, f := range files {
			t.WriteFile(f)
		}
	})
}
