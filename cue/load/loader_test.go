// Copyright 2018 The CUE Authors
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

package load

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"cuelang.org/go/cue"
	build "cuelang.org/go/cue/build"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/str"
)

// TestLoad is an end-to-end test.
func TestLoad(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	args := str.StringList
	testCases := []struct {
		args []string
		want string
		err  string
	}{{
		args: nil,
		want: "test: test.cue (1 files)",
	}, {
		args: args("."),
		want: "test: test.cue (1 files)",
	}, {
		args: args("./other/..."),
		want: `
main: other/main.cue (1 files)
	file: other/file/file.cue (1 files);main: other/main.cue (1 files)
	file: other/file/file.cue (1 files)`,
	}, {
		args: args("./anon"),
		want: ":  (0 files)",
		err:  "build constraints exclude all CUE files",
	}, {
		args: args("./other"),
		want: `
main: other/main.cue (1 files)
	file: other/file/file.cue (1 files)`,
	}, {
		args: args("./hello"),
		want: "test: test.cue hello/test.cue (2 files)",
	}, {
		args: args("./anon.cue", "./other/anon.cue"),
		want: ": ./anon.cue ./other/anon.cue (2 files)",
	}, {
		// Absolute file is normalized.
		args: args(filepath.Join(cwd, "testdata", "anon.cue")),
		want: ": ./anon.cue (1 files)",
	}, {
		args: args("non-existing"),
		want: ":  (0 files)",
		err:  `cannot find package "non-existing"`,
	}, {
		args: args("./empty"),
		want: ":  (0 files)",
		err:  `no CUE files in ./empty`,
	}, {
		args: args("./imports"),
		want: `
imports: imports/imports.cue (1 files)
	catch: pkg/acme.com/catch/catch.cue (1 files)`,
		err: ``,
	}}
	for i, tc := range testCases {
		t.Run(strconv.Itoa(i)+"/"+strings.Join(tc.args, ":"), func(t *testing.T) {
			c := &Config{Dir: filepath.Join(cwd, testdata)}
			pkgs := Instances(tc.args, c)

			var errs, data []string
			for _, p := range pkgs {
				if p.Err != nil {
					errs = append(errs, p.Err.Error())
				}
				got := strings.TrimSpace(pkgInfo(pkgs[0]))
				data = append(data, got)
			}

			if err := strings.Join(errs, ";"); err == "" != (tc.err == "") ||
				err != "" && !strings.Contains(err, tc.err) {
				t.Errorf("error:\n got: %v\nwant: %v", err, tc.err)
			}
			got := strings.Join(data, ";")
			// Make test work with Windows.
			got = strings.Replace(got, string(filepath.Separator), "/", -1)
			want := strings.TrimSpace(tc.want)
			if got != want {
				t.Errorf("got:\n%v\nwant:\n%v", got, want)
			}
		})
	}
}

func pkgInfo(p *build.Instance) string {
	b := &bytes.Buffer{}
	fmt.Fprintf(b, "%s: %s (%d files)\n",
		p.PkgName, strings.Join(p.CUEFiles, " "), len(p.Files))
	for _, p := range p.Imports {
		fmt.Fprintf(b, "\t%s\n", pkgInfo(p))
	}
	return b.String()
}

func TestOverlays(t *testing.T) {
	cwd, _ := os.Getwd()
	abs := func(path string) string {
		return filepath.Join(cwd, path)
	}
	c := &Config{
		Overlay: map[string]Source{
			abs("dir/top.cue"): FromBytes([]byte(`
			   package top
			   msg: "Hello"
			`)),
			abs("dir/b/foo.cue"): FromString(`
			   package foo

			   a: <= 5
			`),
			abs("dir/b/bar.cue"): FromString(`
			   package foo

			   a: >= 5
			`),
		},
	}
	want := []string{
		`{msg:"Hello"}`,
		`{a:5}`,
	}
	rmSpace := func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}
	for i, inst := range cue.Build(Instances([]string{"./dir/..."}, c)) {
		if inst.Err != nil {
			t.Error(inst.Err)
			continue
		}
		b, err := format.Node(inst.Value().Syntax())
		if err != nil {
			t.Error(err)
			continue
		}
		if got := string(bytes.Map(rmSpace, b)); got != want[i] {
			t.Errorf("%s: got %s; want %s", inst.Dir, got, want)
		}
	}
}
