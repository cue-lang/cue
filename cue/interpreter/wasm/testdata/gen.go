// Copyright 2023 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

//go:generate go run gen.go

func main() {
	// We do not want to burden our users, not even our developers
	// with a Rust dependency, so only build the Rust Wasm binaries
	// if this variable is set.
	if _, ok := os.LookupEnv("CUE_WASM_BUILD_RUST"); !ok {
		return
	}

	cwd, _ := os.Getwd()
	src := filepath.Join(cwd, "rust")
	buildRust(src)

	target := filepath.Join(src, "target")
	bins := filepath.Join(target, "wasm32-unknown-unknown", "release")
	cue := filepath.Join(cwd, "cue")
	copyWasm(bins, cue)

	os.RemoveAll(target)
}

func buildRust(srcDir string) {
	cmd := exec.Command(
		"cargo", "build", "--release", "--target", "wasm32-unknown-unknown",
	)
	cmd.Dir = srcDir
	cmd.Run()
}

func copyWasm(binDir, wasmDir string) {
	files, _ := filepath.Glob(filepath.Join(binDir, "*.wasm"))
	for _, f := range files {
		os.Rename(f, filepath.Join(wasmDir, filepath.Base(f)))
	}
}
