// Copyright 2023 CUE Authors
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

// TestExe expects cmd/cue to be configured with wasm support,
// which it only is with the cuewasm build tag enabled.
//go:build cuewasm

package wasm_test

import (
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/internal/cuetest"

	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
)

// We are using TestMain because we want to ensure Wasm is enabled and
// works as expected with the command-line tool.
func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"cue": cmd.MainTest,
	}))
}

// TestExe tests Wasm using the command-line tool.
func TestExe(t *testing.T) {
	root := must(filepath.Abs("testdata"))(t)
	wasmFiles := filepath.Join(root, "cue")
	p := testscript.Params{
		Dir:                 "testdata/cue",
		UpdateScripts:       cuetest.UpdateGoldenFiles,
		RequireExplicitExec: true,
		Setup: func(e *testscript.Env) error {
			copyWasmFiles(t, e.WorkDir, wasmFiles)
			return nil
		},
		Condition: cuetest.Condition,
	}
	if err := gotooltest.Setup(&p); err != nil {
		t.Fatal(err)
	}
	testscript.Run(t, p)
}
