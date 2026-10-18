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

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-quicktest/qt"
)

// TestModuleFiles pins the rules by which cue fix and cue mod upgrade select
// the files of a module: the ./... rules for the main tree, the trees of
// cue.mod/gen, pkg and usr, _ and testdata directories included, no nested
// modules in the main tree (a vendored copy of a module may carry a cue.mod
// of its own). A directory's files come before its subdirectories. A link
// to a file of the module is dropped for its target, a link to a file
// outside the module or into a nested module and a link to a directory are
// kept as failed, any other link to a file is read through.
func TestModuleFiles(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "mod")
	files := []string{
		"mod/cue.mod/module.cue",
		"mod/.dot.cue",
		"mod/_hidden.cue",
		"mod/a.cue",
		"mod/a_test.cue",
		"mod/a_tool.cue",
		"mod/tmpl.cue",
		"mod/b/b.cue",
		"mod/_dir/x.cue",
		"mod/testdata/t.cue",
		"mod/b/testdata/t.cue",
		"mod/nested/cue.mod/module.cue",
		"mod/nested/n.cue",
		"mod/sub/.hidden/h.cue",
		"mod/cue.mod/gen/k8s.io/api/core/v1/pod.cue",
		"mod/cue.mod/pkg/github.com/org/repo/sub/s.cue",
		"mod/cue.mod/pkg/_x/y.cue",
		"mod/cue.mod/pkg/.h/z.cue",
		"mod/cue.mod/pkg/example.com/v/cue.mod/module.cue",
		"mod/cue.mod/pkg/example.com/v/v.cue",
		"mod/cue.mod/usr/u/u.cue",
		"mod/cue.mod/usr/u/_test/t.cue",
		"outside.cue",
	}
	for _, f := range files {
		p := filepath.Join(base, filepath.FromSlash(f))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(p), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(p, []byte("package p\n"), 0o666)))
	}
	links := runtime.GOOS != "windows"
	if links {
		qt.Assert(t, qt.IsNil(os.Symlink("a.cue", filepath.Join(root, "link.cue"))))
		qt.Assert(t, qt.IsNil(os.Symlink("b", filepath.Join(root, "linkdir"))))
		qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("_dir", "x.cue"), filepath.Join(root, "linkx.cue"))))
		qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("..", "outside.cue"), filepath.Join(root, "out.cue"))))
		qt.Assert(t, qt.IsNil(os.Symlink(filepath.Join("nested", "n.cue"), filepath.Join(root, "linkn.cue"))))
	}

	selected, err := moduleFiles(root)
	qt.Assert(t, qt.IsNil(err))
	var got []string
	for _, f := range selected {
		if f.skip != nil {
			got = append(got, f.FilePath+" (skipped: "+f.skip.Error()+")")
			continue
		}
		if f.fail != nil {
			got = append(got, f.FilePath+" (failed: "+f.fail.Error()+")")
			continue
		}
		qt.Assert(t, qt.IsNotNil(f.Syntax), qt.Commentf("%s", f.FilePath))
		got = append(got, f.FilePath)
	}
	want := []string{"a.cue", "a_test.cue", "a_tool.cue"}
	if links {
		want = append(want,
			"linkdir (failed: symbolic link to a directory is not followed)",
			"linkn.cue (failed: symbolic link into a nested module is not written through)",
			"out.cue (failed: symbolic link to a file outside the module is not written through)",
		)
	}
	want = append(want,
		"tmpl.cue",
		"_dir/x.cue",
		"b/b.cue",
		"b/testdata/t.cue",
		"testdata/t.cue",
		"cue.mod/gen/k8s.io/api/core/v1/pod.cue",
		"cue.mod/pkg/_x/y.cue",
		"cue.mod/pkg/example.com/v/v.cue",
		"cue.mod/pkg/github.com/org/repo/sub/s.cue",
		"cue.mod/usr/u/u.cue",
		"cue.mod/usr/u/_test/t.cue",
	)
	qt.Assert(t, qt.DeepEquals(got, want))
}
