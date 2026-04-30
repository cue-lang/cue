// Copyright 2026 The CUE Authors
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

package load_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/embed"
	"cuelang.org/go/cue/load"
)

// TestEmbedFromOverlayAndFS verifies that @embed resolves files from both
// load.Config.Overlay and load.Config.FS without disk I/O.
// Regression test for https://cuelang.org/issue/4343.
func TestEmbedFromOverlayAndFS(t *testing.T) {
	const moduleCue = `module: "test.local"
language: version: "v0.14.0"
`
	const pkgCue = `@extern(embed)
package pkg

single: _ @embed(file=data.yaml)
all: _ @embed(glob=*.yaml)
`
	const yaml1 = "k: 42\n"
	const yaml2 = "k: 7\n"

	files := map[string]string{
		"cue.mod/module.cue": moduleCue,
		"pkg/x.cue":          pkgCue,
		"pkg/data.yaml":      yaml1,
		"pkg/other.yaml":     yaml2,
	}

	check := func(t *testing.T, cfg *load.Config) {
		t.Helper()
		ctx := cuecontext.New(cuecontext.WithInjection(embed.New()))
		insts := load.Instances([]string{"./pkg"}, cfg)
		qt.Assert(t, qt.HasLen(insts, 1))
		qt.Assert(t, qt.IsNil(insts[0].Err))
		v := ctx.BuildInstance(insts[0])
		qt.Assert(t, qt.IsNil(v.Err()))
		single, err := v.LookupPath(cue.MakePath(cue.Str("single"), cue.Str("k"))).Int64()
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(single, int64(42)))
		dataK, err := v.LookupPath(cue.MakePath(cue.Str("all"), cue.Str("data.yaml"), cue.Str("k"))).Int64()
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(dataK, int64(42)))
		otherK, err := v.LookupPath(cue.MakePath(cue.Str("all"), cue.Str("other.yaml"), cue.Str("k"))).Int64()
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(otherK, int64(7)))
	}

	t.Run("Overlay", func(t *testing.T) {
		cwd, err := os.Getwd()
		qt.Assert(t, qt.IsNil(err))
		overlay := map[string]load.Source{}
		for name, content := range files {
			overlay[filepath.Join(cwd, name)] = load.FromString(content)
		}
		check(t, &load.Config{
			Dir:     cwd,
			Overlay: overlay,
		})
	})

	t.Run("FS", func(t *testing.T) {
		mapFS := fstest.MapFS{}
		for name, content := range files {
			mapFS[name] = &fstest.MapFile{Data: []byte(content)}
		}
		check(t, &load.Config{
			FS: mapFS,
		})
	})
}
