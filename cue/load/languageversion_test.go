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
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/cueversion"
)

// languageVersionTree holds a module declaring v0.16.0, with a nested module
// of its own, next to files that belong to no module.
const languageVersionTree = `
-- mod/cue.mod/module.cue --
module: "mod.example@v0"
language: version: "v0.16.0"
-- mod/x.cue --
package x

a: 1
-- mod/nested/cue.mod/module.cue --
module: "nested.example@v0"
language: version: "v0.15.0"
-- mod/nested/n.cue --
package n

n: 1
-- broken/cue.mod/module.cue --
this is not CUE
-- loose/y.cue --
b: 2
-- loose/z.cue --
c: 3
-- loose/pkg/p.cue --
package p

p: 1
-- prefix/foo/cue.mod/module.cue --
module: "foo.example@v0"
language: version: "v0.16.0"
-- prefix/foo/f.cue --
package f

f: 1
-- prefix/foobar/b.cue --
package b

b: 1
-- empty/cue.mod/module.cue --
-- empty/e.cue --
e: 1
`

func writeTree(t *testing.T, archive string) string {
	dir := t.TempDir()
	for _, f := range txtar.Parse([]byte(archive)).Files {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		qt.Assert(t, qt.IsNil(os.MkdirAll(filepath.Dir(path), 0o777)))
		qt.Assert(t, qt.IsNil(os.WriteFile(path, f.Data, 0o666)))
	}
	return dir
}

// fileVersions returns the language version each file of inst was parsed at.
func fileVersions(inst *build.Instance) []string {
	var versions []string
	for _, f := range inst.Files {
		versions = append(versions, f.Pos().LanguageVersion())
	}
	return versions
}

func TestLanguageVersion(t *testing.T) {
	root := writeTree(t, languageVersionTree)
	current := cueversion.LanguageVersion()

	tests := []struct {
		name    string
		dir     string // relative to root
		args    []string
		stdin   string
		version string

		// Either the versions of the files of the single instance, or an
		// error which all of the substrings must appear in.
		want    []string
		wantErr []string
	}{{
		name:    "LooseFile",
		dir:     "loose",
		args:    []string{"y.cue"},
		version: "v0.17.0",
		want:    []string{"v0.17.0"},
	}, {
		name:    "LooseFiles",
		dir:     "loose",
		args:    []string{"y.cue", "z.cue"},
		version: "v0.17.0",
		want:    []string{"v0.17.0", "v0.17.0"},
	}, {
		name:    "LoosePackage",
		dir:     "loose",
		args:    []string{"./pkg"},
		version: "v0.17.0",
		want:    []string{"v0.17.0"},
	}, {
		name:    "Stdin",
		dir:     "loose",
		args:    []string{"-"},
		stdin:   "s: 1\n",
		version: "v0.17.0",
		want:    []string{"v0.17.0"},
	}, {
		name: "NoOption",
		dir:  "loose",
		args: []string{"y.cue"},
		want: []string{current},
	}, {
		name:    "CurrentRejected",
		dir:     "loose",
		args:    []string{"y.cue"},
		version: "current",
		wantErr: []string{`language version "current" is not canonical`},
	}, {
		name: "NoOptionInsideModule",
		dir:  "mod",
		args: []string{"."},
		want: []string{"v0.16.0"},
	}, {
		name:    "NotCanonical",
		dir:     "loose",
		args:    []string{"y.cue"},
		version: "v0.17",
		wantErr: []string{`language version "v0.17" is not canonical (must include major, minor and patch versions)`},
	}, {
		name:    "TooEarly",
		dir:     "loose",
		args:    []string{"y.cue"},
		version: "v0.7.0",
		wantErr: []string{`language version "v0.7.0" is too early for module.cue schema (earliest allowed is `},
	}, {
		name:    "TooLate",
		dir:     "loose",
		args:    []string{"y.cue"},
		version: "v9.0.0",
		wantErr: []string{`language version "v9.0.0" may not be after current language version ` + current},
	}, {
		// The option is only a fallback: files of a module that declares a
		// version keep it, and it is no error if nothing falls back.
		name:    "PackageInsideModule",
		dir:     "mod",
		args:    []string{"."},
		version: "v0.17.0",
		want:    []string{"v0.16.0"},
	}, {
		name:    "DefaultPackageInsideModule",
		dir:     "mod",
		version: "v0.17.0",
		want:    []string{"v0.16.0"},
	}, {
		name:    "FileInsideModule",
		dir:     "mod",
		args:    []string{"x.cue"},
		version: "v0.15.0",
		want:    []string{"v0.16.0"},
	}, {
		name:    "FileOutsideRoot",
		dir:     "mod",
		args:    []string{"../loose/y.cue"},
		version: "v0.17.0",
		want:    []string{"v0.17.0"},
	}, {
		// Only the file outside the module falls back.
		name:    "MixedFiles",
		dir:     "mod",
		args:    []string{"../loose/y.cue", "x.cue"},
		version: "v0.17.0",
		want:    []string{"v0.17.0", "v0.16.0"},
	}, {
		// A file of another module is read as without the option.
		name:    "FileOfOtherModule",
		dir:     "loose",
		args:    []string{"../mod/nested/n.cue"},
		version: "v0.17.0",
		want:    []string{current},
	}, {
		// The pattern's scan starts at prefix, which is outside any module,
		// and skips the nested module foo, so only foobar matches.
		name:    "PartialPattern",
		dir:     "prefix",
		args:    []string{"./foo..."},
		version: "v0.17.0",
		want:    []string{"v0.17.0"},
	}, {
		name:    "PatternInsideModule",
		dir:     "prefix/foo",
		args:    []string{"./..."},
		version: "v0.17.0",
		want:    []string{"v0.16.0"},
	}, {
		// The working directory's module is broken, but the input does not
		// belong to it.
		name:    "BrokenUnrelatedModule",
		dir:     "broken",
		args:    []string{"../loose/y.cue"},
		version: "v0.17.0",
		want:    []string{"v0.17.0"},
	}, {
		// A module file that declares no version fails to load, with or
		// without the option.
		name:    "UnversionedModule",
		dir:     "empty",
		args:    []string{"e.cue"},
		version: "v0.17.0",
		wantErr: []string{"no language version declared in module.cue"},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &load.Config{
				Dir:             filepath.Join(root, test.dir),
				LanguageVersion: test.version,
			}
			if test.stdin != "" {
				cfg.Stdin = strings.NewReader(test.stdin)
			}
			insts := load.Instances(test.args, cfg)
			qt.Assert(t, qt.HasLen(insts, 1))
			inst := insts[0]
			if test.wantErr != nil {
				qt.Assert(t, qt.IsNotNil(inst.Err))
				msg := inst.Err.Error()
				for _, want := range test.wantErr {
					qt.Check(t, qt.StringContains(msg, want))
				}
				return
			}
			qt.Assert(t, qt.IsNil(inst.Err))
			qt.Assert(t, qt.DeepEquals(fileVersions(inst), test.want))
		})
	}
}

// TestLanguageVersionOverlay checks that byte overlays of a module-less load
// are parsed at the chosen version too.
func TestLanguageVersionOverlay(t *testing.T) {
	dir := t.TempDir()
	insts := load.Instances([]string{"o.cue"}, &load.Config{
		Dir:             dir,
		LanguageVersion: "v0.17.0",
		Overlay: map[string]load.Source{
			filepath.Join(dir, "o.cue"): load.FromString("o: 1\n"),
		},
	})
	qt.Assert(t, qt.HasLen(insts, 1))
	qt.Assert(t, qt.IsNil(insts[0].Err))
	qt.Assert(t, qt.DeepEquals(fileVersions(insts[0]), []string{"v0.17.0"}))
}

const versionAttrTree = `
-- loose/attr.cue --
@version(v0.17.0)

a: 1
-- loose/plain.cue --
b: 2
-- mod/cue.mod/module.cue --
module: "mod.example@v0"
language: version: "v0.16.0"
-- mod/v.cue --
@version(v0.17.0)

package x

v: 1
`

func TestVersionAttributeLoad(t *testing.T) {
	root := writeTree(t, versionAttrTree)
	current := cueversion.LanguageVersion()

	tests := []struct {
		name    string
		dir     string
		args    []string
		version string

		want         []string // versions of the instance's files
		wantInstance string   // the instance's TargetLanguageVersion
		wantErr      string
	}{{
		name:         "Attribute",
		dir:          "loose",
		args:         []string{"attr.cue"},
		want:         []string{"v0.17.0"},
		wantInstance: "",
	}, {
		name:         "WinsOverOption",
		dir:          "loose",
		args:         []string{"attr.cue"},
		version:      "v0.16.0",
		want:         []string{"v0.17.0"},
		wantInstance: "v0.16.0",
	}, {
		// Files without the attribute take the option; the files disagree,
		// but export still targets the option's version.
		name:         "MixedWithOption",
		dir:          "loose",
		args:         []string{"attr.cue", "plain.cue"},
		version:      "v0.16.0",
		want:         []string{"v0.17.0", "v0.16.0"},
		wantInstance: "v0.16.0",
	}, {
		name:         "MixedWithoutOption",
		dir:          "loose",
		args:         []string{"attr.cue", "plain.cue"},
		want:         []string{"v0.17.0", current},
		wantInstance: "",
	}, {
		name:    "InsideModule",
		dir:     "mod",
		args:    []string{"."},
		wantErr: "the @version attribute cannot be used in a file of the module whose cue.mod/module.cue declares language version v0.16.0",
	}, {
		name:    "InsideModuleFile",
		dir:     "mod",
		args:    []string{"v.cue"},
		wantErr: "the @version attribute cannot be used in a file of the module whose cue.mod/module.cue declares language version v0.16.0",
	}, {
		// A file outside the module root belongs to no module, whatever the
		// working directory: its attribute applies, then the option, and only
		// without either the version of the working directory's module, whose
		// load it joins without the option.
		name:         "OutsideRootAttribute",
		dir:          "mod",
		args:         []string{"../loose/attr.cue"},
		want:         []string{"v0.17.0"},
		wantInstance: "v0.16.0",
	}, {
		name:         "OutsideRootAttributeWithOption",
		dir:          "mod",
		args:         []string{"../loose/attr.cue"},
		version:      "v0.15.0",
		want:         []string{"v0.17.0"},
		wantInstance: "v0.15.0",
	}, {
		name:         "OutsideRootPlainWithOption",
		dir:          "mod",
		args:         []string{"../loose/plain.cue"},
		version:      "v0.17.0",
		want:         []string{"v0.17.0"},
		wantInstance: "v0.17.0",
	}, {
		name:         "OutsideRootPlain",
		dir:          "mod",
		args:         []string{"../loose/plain.cue"},
		want:         []string{"v0.16.0"},
		wantInstance: "v0.16.0",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			insts := load.Instances(test.args, &load.Config{
				Dir:             filepath.Join(root, test.dir),
				LanguageVersion: test.version,
			})
			qt.Assert(t, qt.HasLen(insts, 1))
			inst := insts[0]
			if test.wantErr != "" {
				qt.Assert(t, qt.IsNotNil(inst.Err))
				qt.Assert(t, qt.StringContains(inst.Err.Error(), test.wantErr))
				return
			}
			qt.Assert(t, qt.IsNil(inst.Err))
			qt.Assert(t, qt.DeepEquals(fileVersions(inst), test.want))
			qt.Assert(t, qt.Equals(inst.TargetLanguageVersion(), test.wantInstance))
		})
	}
}
