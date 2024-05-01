// Copyright 2024 The CUE Authors
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

// Package testing is a helper package for test packages in the CUE project.
// As such it should only be imported in _test.go files.
package cuetdtest

import (
	"testing"

	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/tdtest"
)

func init() {
	tdtest.UpdateTests = cuetest.UpdateGoldenFiles
}

type T struct {
	*tdtest.T
	M *M
}

// Run creates a new table-driven test using the CUE testing defaults.
func Run[TC any](t *testing.T, table []TC, fn func(t *T, tc *TC)) {
	FullMatrix.Do(t, func(m *M) {
		tdtest.Run(m.T, table, func(t *tdtest.T, tc *TC) {
			m.T = t.T
			if !m.IsDefault() {
				// Do not update table-driven tests if this is not the default
				// test.
				t.Update(false)
			}
			fn(&T{t, m}, tc)
		})
	})
}
