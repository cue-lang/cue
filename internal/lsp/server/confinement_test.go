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

package server

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const module = "cuelang.org/go"

// languageServerOnly lists the modules which the module requires for
// the language server alone, by module path. A program using CUE's Go
// API must not come to link them either.
var languageServerOnly = []string{}

// confined reports whether a package serves the language server alone:
// the code copied from golang.org/x/tools, which is under another
// license than the rest of the module, and the packages of the modules
// in languageServerOnly.
func confined(pkg string) bool {
	if strings.HasPrefix(pkg, module+"/internal/golangorgx/") {
		return true
	}
	for _, mod := range languageServerOnly {
		if pkg == mod || strings.HasPrefix(pkg, mod+"/") {
			return true
		}
	}
	return false
}

// mayDepend reports whether a package may depend on a confined one:
// the cue command, the language server, its integration tests, and the
// confined packages themselves.
func mayDepend(pkg string) bool {
	switch {
	case pkg == module+"/cmd/cue", pkg == module+"/cmd/cue/cmd":
		return true
	case strings.HasPrefix(pkg, module+"/internal/lsp/"):
		return true
	case strings.HasPrefix(pkg+"/", module+"/cmd/cue/cmd/integration/"):
		return true
	}
	return confined(pkg)
}

// TestLanguageServerDependenciesConfined checks that only the cue
// command and the language server depend on the packages which serve
// the language server alone. A program which uses CUE's Go API then
// never links them, whatever it imports: it takes on neither their
// license nor their weight.
func TestLanguageServerDependenciesConfined(t *testing.T) {
	if testing.Short() {
		t.Skip("lists every package of the module with its dependencies")
	}
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go command to list the packages with: %v", err)
	}
	// Outside a workspace the pattern matches the packages of this
	// module alone, which are the ones the rule is about.
	cmd := exec.Command(goCmd, "list", "-f", `{{.ImportPath}} {{join .Deps " "}}`, module+"/...")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	listed := 0
	for line := range strings.Lines(string(out)) {
		pkg, deps, _ := strings.Cut(strings.TrimSpace(line), " ")
		if pkg == "" {
			continue
		}
		listed++
		if mayDepend(pkg) {
			continue
		}
		for dep := range strings.FieldsSeq(deps) {
			if confined(dep) {
				t.Errorf("%s depends on %s; only the cue command and the language server may", pkg, dep)
				break
			}
		}
	}
	// A listing which leaves most packages out would pass for nothing.
	if listed < 100 {
		t.Fatalf("go list gave %d packages of %s; expected at least 100", listed, module)
	}
}
