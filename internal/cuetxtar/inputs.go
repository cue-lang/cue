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

package cuetxtar

import (
	"os"
	"testing"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/cuetest"
)

// UpdateInputs is for tests which generate the input files of the golden
// archive at path, whose output sections are then filled in by the test
// itself. It carries the archive's current output sections over to the
// given inputs and hands the result to [cuetest.WriteGoldenFile], so that
// the archive is rewritten or reported as stale only when the inputs differ.
// Callers must be gated on [cuetest.UpdateOrDiffGoldenFiles].
func UpdateInputs(t testing.TB, path string, inputs []byte) {
	t.Helper()
	archive := txtar.Parse(inputs)
	if data, err := os.ReadFile(path); err == nil {
		for _, f := range txtar.Parse(data).Files {
			if isOutputFile(f.Name) {
				archive.Files = append(archive.Files, f)
			}
		}
	}
	cuetest.WriteGoldenFile(t, path, txtar.Format(archive))
}
