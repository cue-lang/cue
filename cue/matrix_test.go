// Copyright 2024 CUE Authors
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

package cue

import (
	"testing"

	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cuedebug"
)

type evalConfig struct {
	name    string
	version internal.EvaluatorVersion
	flags   cuedebug.Config
}

func runMatrix(t *testing.T, name string, f func(t *testing.T, c *evalConfig)) {
	t.Run(name, func(t *testing.T) {
		doMatrix(t, f)
	})
}

func doMatrix(t *testing.T, f func(t *testing.T, c *evalConfig)) {
	matrix := []*evalConfig{
		{"v2", internal.DefaultVersion, cuedebug.Config{}},
		// TODO: enable this in next CL.
		// {"v3", internal.DevVersion, cuedebug.Config{Sharing: true}},
		// {"v3-noshare", internal.DevVersion, cuedebug.Config{}},
	}

	for _, c := range matrix {
		t.Run(c.name, func(t *testing.T) {
			f(t, c)
		})
	}
}
