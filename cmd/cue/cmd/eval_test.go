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

func TestEval(t *testing.T) {
	runCommand(t, newEvalCmd(), "eval")

	cmd := newEvalCmd()
	mustParseFlags(t, cmd, "-c", "-a")
	runCommand(t, cmd, "eval_conc")

	cmd = newEvalCmd()
	mustParseFlags(t, cmd, "-c", "-e", "b.a.b", "-e", "b.idx")
	runCommand(t, cmd, "eval_expr")
}
