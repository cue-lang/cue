// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yaml_test

import (
	"os"
	"testing"

	"cuelang.org/go/internal/cueexperiment"
)

// TestMain pins the tests in this package to the go.yaml.in/yaml/v3
// based implementation it contains. The exported API steers to
// [cuelang.org/go/internal/encoding/yaml/goccy] when the yamlgoccy
// experiment is enabled, which is the default; that package carries
// its own tests.
func TestMain(m *testing.M) {
	if err := cueexperiment.Init(); err != nil {
		panic(err)
	}
	cueexperiment.Flags.YAMLGoccy = false
	os.Exit(m.Run())
}
