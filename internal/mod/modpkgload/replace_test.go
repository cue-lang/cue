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

package modpkgload

import (
	"context"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// recordingRegistry records the module versions it is asked to fetch.
type recordingRegistry struct {
	fetched []module.Version
}

func (r *recordingRegistry) Fetch(_ context.Context, m module.Version) (module.SourceLoc, error) {
	r.fetched = append(r.fetched, m)
	return module.SourceLoc{Dir: m.String()}, nil
}

func (r *recordingRegistry) ModFile(context.Context, module.Version) (*modfile.File, error) {
	return nil, nil
}

func (r *recordingRegistry) ModuleVersions(context.Context, string) ([]string, error) {
	return nil, nil
}

// TestReplacementsAreMajorVersionSpecific checks that a replacement for one
// major version of a module does not apply to a different major version of
// the same module.
func TestReplacementsAreMajorVersionSpecific(t *testing.T) {
	mf := &modfile.File{
		Module:   "main.example@v0",
		Language: &modfile.Language{Version: "v0.9.0"},
		Deps: map[string]*modfile.Dep{
			"example.com/foo@v0": {Version: "v0.2.0", Replace: "example.com/bar@v0.1.0"},
		},
	}
	repls, err := NewReplacements(mf)
	qt.Assert(t, qt.IsNil(err))

	reg := new(recordingRegistry)
	rr := NewReplacingRegistry(reg, repls, nil)

	// Fetching the replaced major version is redirected to the replacement.
	_, err = rr.Fetch(context.Background(), module.MustNewVersion("example.com/foo@v0", "v0.2.0"))
	qt.Assert(t, qt.IsNil(err))

	// Fetching a different major version of the same module is not replaced.
	_, err = rr.Fetch(context.Background(), module.MustNewVersion("example.com/foo@v1", "v1.0.0"))
	qt.Assert(t, qt.IsNil(err))

	got := mapSlice(reg.fetched, module.Version.String)
	qt.Assert(t, qt.DeepEquals(got, []string{
		"example.com/bar@v0.1.0",
		"example.com/foo@v1.0.0",
	}))
}
