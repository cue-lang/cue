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

package cmd

import "testing"

func TestVet(t *testing.T) {
	runCommand(t, newVetCmd(newRootCmd()), "vet")

	cmd := newVetCmd(newRootCmd())
	mustParseFlags(t, cmd, "-c")
	runCommand(t, cmd, "vet_conc")

	cmd = newVetCmd(newRootCmd())
	runCommand(t, cmd, "vet_file", "./testdata/vet/vet.cue", "./testdata/vet/data.yaml")

	cmd = newVetCmd(newRootCmd())
	mustParseFlags(t, cmd, "-e", "File")
	runCommand(t, cmd, "vet_expr", "./testdata/vet/vet.cue", "./testdata/vet/data.yaml")

}
