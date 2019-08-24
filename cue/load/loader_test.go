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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"unicode"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/str"
	"github.com/kylelemons/godebug/diff"
)

// TestLoad is an end-to-end test.
func TestLoad(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	testdataDir := filepath.Join(cwd, testdata)
	dirCfg := &Config{Dir: testdataDir}

	args := str.StringList
	testCases := []struct {
		cfg  *Config
		args []string
		want string
	}{{
		// Even though the directory is called testdata, the last path in
		// the module is test. So "package test" is correctly the default
		// package of this directory.
		cfg:  dirCfg,
		args: nil,
		want: `
path:   example.org/test
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata
files:
    $CWD/testdata/test.cue
imports:
    example.org/test/sub: $CWD/testdata/sub/sub.cue`,
	}, {
		// Even though the directory is called testdata, the last path in
		// the module is test. So "package test" is correctly the default
		// package of this directory.
		cfg:  dirCfg,
		args: args("."),
		want: `
path:   example.org/test
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata
files:
    $CWD/testdata/test.cue
imports:
    example.org/test/sub: $CWD/testdata/sub/sub.cue`,
	}, {
		// TODO:
		// - paths are incorrect, should be example.org/test/other:main and
		//   example.org/test/other/file, respectively.
		// - referenced import path of files is wrong.
		cfg:  dirCfg,
		args: args("./other/..."),
		want: `
path:   example.org/test
module: example.org/test
root:   $CWD/testdata/
dir:    $CWD/testdata/other
files:
	$CWD/testdata/other/main.cue
imports:
	./file: $CWD/testdata/other/file/file.cue

path:   example.org/test/file
module: example.org/test
root:   $CWD/testdata/
dir:    $CWD/testdata/other/file
files:
	$CWD/testdata/other/file/file.cue`,
	}, {
		cfg:  dirCfg,
		args: args("./anon"),
		want: `
err:    build constraints exclude all CUE files in ./anon
path:   example.org/test/anon
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/anon`,
	}, {
		// TODO:
		// - paths are incorrect, should be example.org/test/other:main and
		//   example.org/test/other/file, respectively.
		cfg:  dirCfg,
		args: args("./other"),
		want: `
path:   example.org/test/other
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/other
files:
	$CWD/testdata/other/main.cue
imports:
	./file: $CWD/testdata/other/file/file.cue`,
	}, {
		// TODO:
		// - incorrect path, should be example.org/test/hello:test
		cfg:  dirCfg,
		args: args("./hello"),
		want: `
path:   example.org/test/hello
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/hello
files:
	$CWD/testdata/test.cue
	$CWD/testdata/hello/test.cue
imports:
	example.org/test/sub: $CWD/testdata/sub/sub.cue`,
	}, {
		cfg:  dirCfg,
		args: args("./anon.cue", "./other/anon.cue"),
		want: `
path:   ""
module: ""
root:   $CWD/testdata
dir:    $CWD/testdata
files:
	$CWD/testdata/anon.cue
	$CWD/testdata/other/anon.cue`,
	}, {
		cfg: dirCfg,
		// Absolute file is normalized.
		args: args(filepath.Join(cwd, "testdata", "anon.cue")),
		want: `
path:   ""
module: ""
root:   $CWD/testdata
dir:    $CWD/testdata
files:
	$CWD/testdata/anon.cue`,
	}, {
		// NOTE: dir should probably be set to $CWD/testdata, but either way.
		cfg:  dirCfg,
		args: args("non-existing"),
		want: `
err:    cannot find package "non-existing"
path:   ""
module: example.org/test
root:   $CWD/testdata
dir:    non-existing `,
	}, {
		cfg:  dirCfg,
		args: args("./empty"),
		want: `
err:    no CUE files in ./empty
path:   example.org/test/empty
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/empty`,
	}, {
		cfg:  dirCfg,
		args: args("./imports"),
		want: `
path:   example.org/test/imports
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/imports
files:
	$CWD/testdata/imports/imports.cue
imports:
	acme.com/catch: $CWD/testdata/pkg/acme.com/catch/catch.cue
	acme.com/helper: $CWD/testdata/pkg/acme.com/helper/helper.cue`,
	}}
	for i, tc := range testCases {
		t.Run(strconv.Itoa(i)+"/"+strings.Join(tc.args, ":"), func(t *testing.T) {
			pkgs := Instances(tc.args, tc.cfg)

			buf := &bytes.Buffer{}
			err := pkgInfo.Execute(buf, pkgs)
			if err != nil {
				t.Fatal(err)
			}

			got := strings.TrimSpace(buf.String())
			got = strings.Replace(got, cwd, "$CWD", -1)
			// Make test work with Windows.
			got = strings.Replace(got, string(filepath.Separator), "/", -1)

			want := strings.TrimSpace(tc.want)
			want = strings.Replace(want, "\t", "    ", -1)
			if got != want {
				t.Errorf("\n%s", diff.Diff(got, want))
				t.Logf("\n%s", got)
			}
		})
	}
}

var pkgInfo = template.Must(template.New("pkg").Parse(`
{{- range . -}}
{{- if .Err}}err:    {{.Err}}{{end}}
path:   {{if .ImportPath}}{{.ImportPath}}{{else}}""{{end}}
module: {{if .Module}}{{.Module}}{{else}}""{{end}}
root:   {{.Root}}
dir:    {{.Dir}}
{{if .Files -}}
files:
{{- range .Files}}
    {{.Filename}}
{{- end -}}
{{- end}}
{{if .Imports -}}
imports:
{{- range .Dependencies}}
    {{.ImportPath}}:{{range .Files}} {{.Filename}}{{end}}
{{- end}}
{{end -}}
{{- end -}}
`))

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
