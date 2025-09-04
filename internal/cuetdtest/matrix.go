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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
)

// M represents a point in the evaluation matrix of possible
// runtime configurations.
type M struct {
	// Flags is public to allow tests to customise e.g. logging.
	Flags cuedebug.Config

	name     string
	fallback string
	version  internal.EvaluatorVersion
}

func (t *M) Name() string     { return t.name }
func (t *M) Fallback() string { return t.fallback }
func (t *M) IsDefault() bool  { return t.name == DefaultVersion }

func (t *M) CueContext() *cue.Context {
	ctx := cuecontext.New()
	r := (*runtime.Runtime)(ctx)
	r.SetVersion(t.version)
	r.SetDebugOptions(&t.Flags)
	return ctx
}

// Runtime creates a runtime that is configured according to the matrix.
func (t *M) Runtime() *runtime.Runtime {
	return (*runtime.Runtime)(t.CueContext())
}

// TODO(mvdan): the default should now be evalv3.
// We keep it to be v2 for now, as a lot of tests still assume the evalv2 output
// is the "golden output". We will phase that out incrementally.
const DefaultVersion = "v2"

type Matrix []M

var (
	evalv3 = M{
		name:     "v3",
		fallback: "v2",
		version:  internal.EvalV3,
		Flags:    cuedebug.Config{Sharing: true},
	}
	evalv3NoShare = M{
		name:     "v3-noshare",
		fallback: "v2",
		version:  internal.EvalV3,
	}
)

var FullMatrix Matrix = []M{
	evalv3,
	evalv3NoShare,
}

var SmallMatrix Matrix = []M{evalv3}

// Here we could add more matrices when evalv4 eventually comes,
// as long as their names are clear, like SmallV3Matrix and SmallV4Matrix.

// Run runs a subtest with the given name that
// invokes a further subtest for each configuration in the matrix.
func (m Matrix) Run(t *testing.T, name string, f func(t *testing.T, m *M)) {
	t.Run(name, func(t *testing.T) {
		m.Do(t, f)
	})
}

// Do runs f in a subtest for each configuration in the matrix.
func (m Matrix) Do(t *testing.T, f func(t *testing.T, m *M)) {
	for _, c := range m {
		t.Run(c.name, func(t *testing.T) {
			f(t, &c)
		})
	}
}

func (m *M) TODO_Sharing(t testing.TB) {
	if m.Flags.Sharing {
		t.Skip("Skipping v3 with sharing")
	}
}

func (m *M) TODO_NoSharing(t testing.TB) {
	if m.version == internal.EvalV3 && !m.Flags.Sharing {
		t.Skip("Skipping v3 without sharing")
	}
}
