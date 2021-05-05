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

	"github.com/kylelemons/godebug/diff"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/str"
)

// TestLoad is an end-to-end test.
func TestLoad(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	testdataDir := filepath.Join(cwd, testdata)
	dirCfg := &Config{
		Dir:   testdataDir,
		Tools: true,
	}

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
display:.
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
display:.
files:
    $CWD/testdata/test.cue
imports:
    example.org/test/sub: $CWD/testdata/sub/sub.cue`,
	}, {
		// TODO:
		// - path incorrect, should be example.org/test/other:main.
		cfg:  dirCfg,
		args: args("./other/..."),
		want: `
err:    import failed: relative import paths not allowed ("./file")
path:   ""
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/cue.mod/gen
display:`,
	}, {
		cfg:  dirCfg,
		args: args("./anon"),
		want: `
err:    build constraints exclude all CUE files in ./anon:
	anon/anon.cue: no package name
path:   example.org/test/anon
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/anon
display:./anon`,
	}, {
		// TODO:
		// - paths are incorrect, should be example.org/test/other:main.
		cfg:  dirCfg,
		args: args("./other"),
		want: `
err:    import failed: relative import paths not allowed ("./file")
path:   example.org/test/other:main
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/other
display:./other
files:
	$CWD/testdata/other/main.cue`,
	}, {
		// TODO:
		// - incorrect path, should be example.org/test/hello:test
		cfg:  dirCfg,
		args: args("./hello"),
		want: `
path:   example.org/test/hello:test
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/hello
display:./hello
files:
	$CWD/testdata/test.cue
	$CWD/testdata/hello/test.cue
imports:
	example.org/test/sub: $CWD/testdata/sub/sub.cue`,
	}, {
		// TODO:
		// - incorrect path, should be example.org/test/hello:test
		cfg:  dirCfg,
		args: args("example.org/test/hello:test"),
		want: `
path:   example.org/test/hello:test
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/hello
display:example.org/test/hello:test
files:
	$CWD/testdata/test.cue
	$CWD/testdata/hello/test.cue
imports:
	example.org/test/sub: $CWD/testdata/sub/sub.cue`,
	}, {
		// TODO:
		// - incorrect path, should be example.org/test/hello:test
		cfg:  dirCfg,
		args: args("example.org/test/hello:nonexist"),
		want: `
err:    build constraints exclude all CUE files in example.org/test/hello:nonexist:
    anon.cue: no package name
    test.cue: package is test, want nonexist
    hello/test.cue: package is test, want nonexist
path:   example.org/test/hello:nonexist
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/hello
display:example.org/test/hello:nonexist`,
	}, {
		cfg:  dirCfg,
		args: args("./anon.cue", "./other/anon.cue"),
		want: `
path:   ""
module: ""
root:   $CWD/testdata
dir:    $CWD/testdata
display:command-line-arguments
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
display:command-line-arguments
files:
    $CWD/testdata/anon.cue`,
	}, {
		cfg:  dirCfg,
		args: args("-"),
		want: `
path:   ""
module: ""
root:   $CWD/testdata
dir:    $CWD/testdata
display:command-line-arguments
files:
    -`,
	}, {
		// NOTE: dir should probably be set to $CWD/testdata, but either way.
		cfg:  dirCfg,
		args: args("non-existing"),
		want: `
err:    cannot find package "non-existing"
path:   non-existing
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/cue.mod/gen/non-existing
display:non-existing`,
	}, {
		cfg:  dirCfg,
		args: args("./empty"),
		want: `
err:    no CUE files in ./empty
path:   example.org/test/empty
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/empty
display:./empty`,
	}, {
		cfg:  dirCfg,
		args: args("./imports"),
		want: `
path:   example.org/test/imports
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/imports
display:./imports
files:
	$CWD/testdata/imports/imports.cue
imports:
	acme.com/catch: $CWD/testdata/cue.mod/pkg/acme.com/catch/catch.cue
	acme.com/helper:helper1: $CWD/testdata/cue.mod/pkg/acme.com/helper/helper1.cue`,
	}, {
		cfg:  dirCfg,
		args: args("./toolonly"),
		want: `
path:   example.org/test/toolonly:foo
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/toolonly
display:./toolonly
files:
	$CWD/testdata/toolonly/foo_tool.cue`,
	}, {
		cfg: &Config{
			Dir: testdataDir,
		},
		args: args("./toolonly"),
		want: `
err:    build constraints exclude all CUE files in ./toolonly:
    anon.cue: no package name
    test.cue: package is test, want foo
    toolonly/foo_tool.cue: _tool.cue files excluded in non-cmd mode
path:   example.org/test/toolonly:foo
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/toolonly
display:./toolonly`,
	}, {
		cfg: &Config{
			Dir:  testdataDir,
			Tags: []string{"prod"},
		},
		args: args("./tags"),
		want: `
path:   example.org/test/tags
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/tags
display:./tags
files:
	$CWD/testdata/tags/prod.cue`,
	}, {
		cfg: &Config{
			Dir:  testdataDir,
			Tags: []string{"prod", "foo=bar"},
		},
		args: args("./tags"),
		want: `
path:   example.org/test/tags
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/tags
display:./tags
files:
	$CWD/testdata/tags/prod.cue`,
	}, {
		cfg: &Config{
			Dir:  testdataDir,
			Tags: []string{"prod"},
		},
		args: args("./tagsbad"),
		want: `
err:    multiple @if attributes (and 2 more errors)
path:   example.org/test/tagsbad
module: example.org/test
root:   $CWD/testdata
dir:    $CWD/testdata/tagsbad
display:./tagsbad`,
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
				t.Errorf("\n%s", diff.Diff(want, got))
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
display:{{.DisplayPath}}
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
			// Not necessary, but nice to add.
			abs("cue.mod"): FromString(`module: "acme.com"`),

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
		b, err := format.Node(inst.Value().Syntax(cue.Final()))
		if err != nil {
			t.Error(err)
			continue
		}
		if got := string(bytes.Map(rmSpace, b)); got != want[i] {
			t.Errorf("%s: got %s; want %s", inst.Dir, got, want[i])
		}
	}
}
