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

package cmd

import (
	"testing"
)

func TestImport(t *testing.T) {
	cmd := newImportCmd(newRootCmd())
	mustParseFlags(t, cmd,
		"-o", "-", "-f", "--files")
	runCommand(t, cmd, "import_files")

	cmd = newImportCmd(newRootCmd())
	mustParseFlags(t, cmd,
		"-o", "-", "-f", "-l", `"\(strings.ToLower(kind))" "\(name)"`)
	runCommand(t, cmd, "import_path")

	cmd = newImportCmd(newRootCmd())
	mustParseFlags(t, cmd,
		"-o", "-", "-f", "-l", `"\(strings.ToLower(kind))"`, "--list")
	runCommand(t, cmd, "import_list")

	cmd = newImportCmd(newRootCmd())
	mustParseFlags(t, cmd,
		"-o", "-", "-f", "--list",
		"-l", `"\(strings.ToLower(kind))" "\(name)"`, "--recursive")
	runCommand(t, cmd, "import_hoiststr")
}
