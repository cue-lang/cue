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

package cuetdtest

import (
	"testing"

	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
)

type M struct {
	*testing.T

	name     string
	fallback string
	version  internal.EvaluatorVersion
	flags    cuedebug.Config
}

func (t *M) Name() string     { return t.name }
func (t *M) Fallback() string { return t.fallback }
func (t *M) IsDefault() bool  { return t.name == DefaultVersion }

// Runtime creates a runtime that is configured according to the matrix.
func (t *M) Runtime() *runtime.Runtime {
	r := runtime.New()
	t.UpdateRuntime(r)
	return r
}

func (t *M) UpdateRuntime(r *runtime.Runtime) {
	r.SetSettings(t.version, t.flags)
}

// RunMatrix runs f for each configuration in the matrix.
func RunMatrix(t *testing.T, name string, f func(t *M)) {
	t.Run(name, func(t *testing.T) {
		do(t, FullMatrix, f)
	})
}

const DefaultVersion = "v2"

type Matrix []M

var FullMatrix Matrix = []M{{
	name:    DefaultVersion,
	version: internal.DefaultVersion,
}, {
	name:     "v3",
	fallback: "v2",
	version:  internal.DevVersion,
	flags:    cuedebug.Config{Sharing: true},
}, {
	name:     "v3-noshare",
	fallback: "v2",
	version:  internal.DevVersion,
}}

var SmallMatrix Matrix = FullMatrix[:2]

func (m Matrix) Do(t *testing.T, f func(t *M)) {
	do(t, m, f)
}

func do(t *testing.T, matrix Matrix, f func(t *M)) {
	for _, c := range matrix {
		t.Run(c.name, func(t *testing.T) {
			c.T = t
			f(&c)
		})
	}
}

func (t *M) TODO_V3() {
	if t.version == internal.DevVersion {
		t.Skip("Skipping v3")
	}
}

func (t *M) TODO_Sharing() {
	if t.flags.Sharing {
		t.Skip("Skipping v3 with sharing")
	}
}

func (t *M) TODO_NoSharing() {
	if t.version == internal.DevVersion && !t.flags.Sharing {
		t.Skip("Skipping v3 without sharing")
	}
}
