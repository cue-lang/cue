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

package cue_test

import (
	"path"
	"testing"
	"testing/fstest"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	cueload "cuelang.org/go/cue/load"
	"github.com/go-quicktest/qt"
)

func TestBuildInstanceFromFS(t *testing.T) {
	mapFS := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`
				module: "example.com/test@v0"
				language: version: "v0.12.0"
			`),
		},
		"x.cue": &fstest.MapFile{
			Data: []byte(`
				package test
				import "example.com/test/sub"
				a: sub.B
			`),
		},
		"sub/y.cue": &fstest.MapFile{
			Data: []byte(`package sub, B: 42`),
		},
	}
	inst := cueload.Instances(nil, &cueload.Config{
		FS: mapFS,
		FromFSPath: func(s string) string {
			return path.Join("/customPrefix", s)
		},
	})[0]
	qt.Assert(t, qt.IsNil(inst.Err))
	// Check that Dir and Root use the mapped display paths.
	qt.Assert(t, qt.Equals(inst.Dir, "/customPrefix"))
	qt.Assert(t, qt.Equals(inst.Root, "/customPrefix"))

	ctx := cuecontext.New()
	v := ctx.BuildInstance(inst)
	qt.Assert(t, qt.IsNil(v.Err()))

	a, _ := v.LookupPath(cue.ParsePath("a")).Int64()
	qt.Assert(t, qt.Equals(a, int64(42)))
}

func TestBuildInstanceFromFSErrorPaths(t *testing.T) {
	mapFS := fstest.MapFS{
		"cue.mod/module.cue": &fstest.MapFile{
			Data: []byte(`
				module: "example.com/test@v0"
				language: version: "v0.12.0"
			`),
		},
		"x.cue": &fstest.MapFile{
			Data: []byte(`
				package test
				a: 1&2
			`),
		},
	}
	inst := cueload.Instances(nil, &cueload.Config{
		FS: mapFS,
		FromFSPath: func(s string) string {
			return path.Join("/customPrefix", s)
		},
	})[0]
	qt.Assert(t, qt.IsNil(inst.Err))

	ctx := cuecontext.New()
	v := ctx.BuildInstance(inst)
	err := v.Err()
	qt.Assert(t, qt.IsNotNil(err))
	t.Logf("error %q", errors.Details(err, nil))
	qt.Assert(t, qt.Matches(errors.Details(err, nil), `(?s).*/customPrefix/.*`))
}
