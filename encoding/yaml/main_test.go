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

func TestMain(m *testing.M) {
	if err := cueexperiment.Init(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// forEachImpl runs f as a subtest with the default go.yaml.in/yaml/v3
// based implementation, and again with the goccy/go-yaml based one
// selected by the yamlgoccy experiment.
func forEachImpl(t *testing.T, f func(t *testing.T, goccy bool)) {
	for _, goccy := range []bool{false, true} {
		name := "yamlv3"
		if goccy {
			name = "goccy"
		}
		t.Run(name, func(t *testing.T) {
			old := cueexperiment.Flags.YAMLGoccy
			cueexperiment.Flags.YAMLGoccy = goccy
			t.Cleanup(func() { cueexperiment.Flags.YAMLGoccy = old })
			f(t, goccy)
		})
	}
}
