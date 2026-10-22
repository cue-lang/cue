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

package cuecontext_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

// TestDeadcodeElimination checks that the linker discards unused methods
// in a program embedding CUE built with the cuenotemplate tag.
func TestDeadcodeElimination(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and links a program")
	}
	bin := filepath.Join(t.TempDir(), "deadcode")
	out, err := exec.Command("go", "build", "-tags=cuenotemplate", "-o", bin, "./testdata/deadcode").CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("%s", out))
	out, err = exec.Command("go", "tool", "nm", bin).CombinedOutput()
	qt.Assert(t, qt.IsNil(err), qt.Commentf("%s", out))
	kept := strings.Contains(string(out), "main.canary.DeadcodeCanary")
	qt.Check(t, qt.IsFalse(kept))
}
