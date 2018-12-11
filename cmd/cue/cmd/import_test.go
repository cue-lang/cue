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

import "testing"

func TestImport(t *testing.T) {
	importCmd.ParseFlags([]string{
		"-o", "-", "-f", "--files",
	})
	runCommand(t, importCmd.RunE, "import_files")

	*files = false
	importCmd.ParseFlags([]string{
		"-f", "-l", `"\(strings.ToLower(kind))" "\(name)"`,
	})
	runCommand(t, importCmd.RunE, "import_path")

	importCmd.ParseFlags([]string{
		"-f", "-l", `"\(strings.ToLower(kind))"`, "--list",
	})
	runCommand(t, importCmd.RunE, "import_list")

	importCmd.ParseFlags([]string{
		"-f", "-l", `"\(strings.ToLower(kind))" "\(name)"`, "--recursive",
	})
	runCommand(t, importCmd.RunE, "import_hoiststr")
}
