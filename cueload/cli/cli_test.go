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

package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cueload"
	"cuelang.org/go/cueload/cli"
)

var ctxbg = context.Background()

const moduleCUE = `module: "main.example@v0"
language: version: "v0.9.0"
`

func baseFiles() map[string]string {
	return map[string]string{
		"work/cue.mod/module.cue": moduleCUE,
		"work/pkg/pkg.cue": `package pkg

a: 1
b: a + 1
`,
		"work/extra.json": `{"c": 3}` + "\n",
		"work/schema.cue": "a: int\n",
		"work/multi.yaml": "a: 1\n---\na: true\n---\na: 3\n",
		"work/kinds.yaml": "kind: \"alpha\"\nn: 1\n---\nkind: \"beta\"\nn: 2\n",
		"work/data.json":  `{"x": 1}` + "\n",
	}
}

func testFS(files map[string]string) fstest.MapFS {
	m := make(fstest.MapFS)
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func newLoader(t *testing.T, files map[string]string, mut func(*cueload.Config)) *cueload.Loader {
	t.Helper()
	cfg := &cueload.Config{
		FS:  testFS(files),
		Dir: "/work",
	}
	if mut != nil {
		mut(cfg)
	}
	l, err := cueload.New(cfg)
	qt.Assert(t, qt.IsNil(err))
	return l
}

// runCmd collects the results and per-result errors of running c.
func runCmd(t *testing.T, l *cueload.Loader, c *cli.Command) ([]cli.Result, []error) {
	t.Helper()
	var results []cli.Result
	var errs []error
	for res, err := range c.Run(ctxbg, l) {
		results = append(results, res)
		errs = append(errs, err)
	}
	return results, errs
}

// output returns the concatenated encoded output of all results,
// requiring every result to have succeeded.
func output(t *testing.T, results []cli.Result, errs []error) string {
	t.Helper()
	var sb strings.Builder
	for i, res := range results {
		qt.Assert(t, qt.IsNil(errs[i]))
		qt.Assert(t, qt.IsNotNil(res.Output))
		sb.Write(res.Output.Data)
	}
	return sb.String()
}

func TestExportPackageJSON(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"./pkg"})
	qt.Assert(t, qt.IsNil(err))

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"a\":1,\"b\":2}\n"))
	qt.Assert(t, qt.Equals(results[0].Output.Name, "-"))
	qt.Assert(t, qt.IsNotNil(results[0].Origin.Package))
	qt.Assert(t, qt.Equals(results[0].Origin.Package.Name(), "pkg"))
}

func TestExportDataFileMerge(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"./pkg", "extra.json"})
	qt.Assert(t, qt.IsNil(err))

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"a\":1,\"b\":2,\"c\":3}\n"))
}

func TestExportNoMergeStreams(t *testing.T) {
	// With -m=false the documents stream per doc, each unified with
	// the package.
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"./pkg", "extra.json"})
	qt.Assert(t, qt.IsNil(err))
	merge := false
	c.Merge = &merge

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"a\":1,\"b\":2,\"c\":3}\n"))
	// Streaming results keep their file origin.
	qt.Assert(t, qt.Equals(results[0].Origin.File.Name, "extra.json"))
}

func TestVetMultiDocYAML(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeVet, []string{"schema.cue", "multi.yaml"})
	qt.Assert(t, qt.IsNil(err))

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 3))
	qt.Assert(t, qt.IsNil(errs[0]))
	qt.Assert(t, qt.ErrorMatches(errs[1], "(?s).*conflicting values.*"))
	qt.Assert(t, qt.IsNil(errs[2]))

	// Per-document origins identify the failing document.
	qt.Assert(t, qt.Equals(results[1].Origin.File.Name, "multi.yaml"))
	qt.Assert(t, qt.Equals(results[1].Origin.Index, 1))

	// Vet results carry no encoded output.
	qt.Assert(t, qt.IsNil(results[0].Output))
}

func TestVetNotConcrete(t *testing.T) {
	// Vet of data requires concrete results: a schema field the data
	// does not provide is an error.
	files := baseFiles()
	files["work/schema2.cue"] = "a: int\nq: int\n"
	l := newLoader(t, files, nil)
	c, err := cli.ParseArgs(cli.ModeVet, []string{"schema2.cue", "data.json"})
	qt.Assert(t, qt.IsNil(err))

	// data.json has x only; schema2 requires a and q to be concrete.
	_, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.ErrorMatches(errs[0], "(?s).*incomplete value.*"))
}

func TestPlacementStatic(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	// The full-path form denotes labels statically; a bare identifier
	// would be an expression evaluated against each record.
	c, err := cli.ParseArgs(cli.ModeExport, []string{"data.json"})
	qt.Assert(t, qt.IsNil(err))
	c.Path = []string{"outer: inner:"}

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"outer\":{\"inner\":{\"x\":1}}}\n"))

	// String literal labels are static too.
	c, err = cli.ParseArgs(cli.ModeExport, []string{"data.json"})
	qt.Assert(t, qt.IsNil(err))
	c.Path = []string{`"outer"`, `"inner"`}
	results, errs = runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"outer\":{\"inner\":{\"x\":1}}}\n"))
}

func TestPlacementDynamicIdent(t *testing.T) {
	// A bare identifier refers to a field of each record: the records
	// place under the value of their kind field.
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"kinds.yaml"})
	qt.Assert(t, qt.IsNil(err))
	c.Path = []string{"kind"}

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs),
		"{\"alpha\":{\"kind\":\"alpha\",\"n\":1},\"beta\":{\"kind\":\"beta\",\"n\":2}}\n"))
}

func TestPlacementDynamic(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"kinds.yaml"})
	qt.Assert(t, qt.IsNil(err))
	c.Path = []string{`"k-\(kind)"`}

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs),
		"{\"k-alpha\":{\"kind\":\"alpha\",\"n\":1},\"k-beta\":{\"kind\":\"beta\",\"n\":2}}\n"))
}

func TestPlacementDynamicWithContext(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"kinds.yaml"})
	qt.Assert(t, qt.IsNil(err))
	c.Path = []string{`"\(filename)-\(index)of\(recordCount)"`}
	c.WithContext = true

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs),
		"{\"kinds.yaml-0of2\":{\"kind\":\"alpha\",\"n\":1},\"kinds.yaml-1of2\":{\"kind\":\"beta\",\"n\":2}}\n"))
}

func TestList(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"kinds.yaml"})
	qt.Assert(t, qt.IsNil(err))
	c.List = true
	c.Path = []string{"items:"}

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs),
		"{\"items\":[{\"kind\":\"alpha\",\"n\":1},{\"kind\":\"beta\",\"n\":2}]}\n"))
}

func TestExpressions(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeEval, []string{"./pkg"})
	qt.Assert(t, qt.IsNil(err))
	c.Expressions = []string{"a + b", "b"}

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 2))
	qt.Assert(t, qt.IsNil(errs[0]))
	qt.Assert(t, qt.IsNil(errs[1]))
	// The CUE encoder separates documents with a blank line.
	qt.Assert(t, qt.Equals(string(results[0].Output.Data), "3\n"))
	qt.Assert(t, qt.Equals(string(results[1].Output.Data), "\n2\n"))
}

func TestTagsAndBuildTags(t *testing.T) {
	files := baseFiles()
	files["work/tags/tags.cue"] = `package tags

env: string @tag(env)
`
	files["work/tags/extra.cue"] = `@if(extra)

package tags

more: true
`
	c, err := cli.ParseArgs(cli.ModeExport, []string{"./tags"})
	qt.Assert(t, qt.IsNil(err))
	c.Tags = []string{"env=prod", "extra"}

	l := newLoader(t, files, c.ApplyToConfig)
	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"env\":\"prod\",\"more\":true}\n"))
}

func TestDefaultTagVars(t *testing.T) {
	tv := cli.DefaultTagVars()
	for _, name := range []string{"now", "os", "arch", "cwd", "username", "hostname", "rand"} {
		v, ok := tv[name]
		qt.Assert(t, qt.IsTrue(ok), qt.Commentf("variable %q", name))
		qt.Assert(t, qt.IsNotNil(v.Func))
	}
	x, err := tv["os"].Func()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNotNil(x))
}

func TestStdin(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"-"})
	qt.Assert(t, qt.IsNil(err))
	c.Stdin = strings.NewReader("x: 42\n")

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"x\":42}\n"))
}

func TestStdinQualified(t *testing.T) {
	// A qualifier applies to stdin like to any other file.
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"json:", "-"})
	qt.Assert(t, qt.IsNil(err))
	c.Stdin = strings.NewReader(`{"y": 7}`)

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "{\"y\":7}\n"))
}

func TestOutYAML(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeExport, []string{"./pkg"})
	qt.Assert(t, qt.IsNil(err))
	c.Out = "yaml:"

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "a: 1\nb: 2\n"))
}

func TestOutFileWrite(t *testing.T) {
	// Output files are written with O_EXCL semantics unless forced.
	dir, err := os.MkdirTemp(".", "cli-test-")
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(func() { os.RemoveAll(dir) })
	target := filepath.Join(dir, "out.json")

	l := newLoader(t, baseFiles(), nil)
	run := func() cli.Result {
		c, err := cli.ParseArgs(cli.ModeExport, []string{"./pkg"})
		qt.Assert(t, qt.IsNil(err))
		c.Out = "json:" + target
		results, errs := runCmd(t, l, c)
		qt.Assert(t, qt.HasLen(results, 1))
		qt.Assert(t, qt.IsNil(errs[0]))
		return results[0]
	}

	res := run()
	qt.Assert(t, qt.Equals(res.Output.Name, target))
	qt.Assert(t, qt.IsNil(res.Output.Write(false)))
	data, err := os.ReadFile(target)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(data), "{\"a\":1,\"b\":2}\n"))

	// A second run must refuse to overwrite without force.
	res = run()
	qt.Assert(t, qt.ErrorMatches(res.Output.Write(false), ".*file already exists.*"))
	qt.Assert(t, qt.IsNil(res.Output.Write(true)))
	data, err = os.ReadFile(target)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(data), "{\"a\":1,\"b\":2}\n"))
}

func TestDefMode(t *testing.T) {
	files := baseFiles()
	files["work/defs/defs.cue"] = `package defs

#Schema: {
	name?: string
	count: int
}
x: #Schema & {count: 1}
`
	l := newLoader(t, files, nil)
	c, err := cli.ParseArgs(cli.ModeDef, []string{"./defs"})
	qt.Assert(t, qt.IsNil(err))

	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	got := output(t, results, errs)
	// Byte-identical to `cue def ./defs`.
	qt.Assert(t, qt.Equals(got, `package defs

#Schema: {
	name?: string
	count: int
}
x: #Schema & {count: 1}
`))
}

func TestEvalIncomplete(t *testing.T) {
	// eval renders incomplete values; export rejects them.
	files := baseFiles()
	files["work/inc/inc.cue"] = "package inc\n\na: int\n"
	l := newLoader(t, files, nil)

	c, err := cli.ParseArgs(cli.ModeEval, []string{"./inc"})
	qt.Assert(t, qt.IsNil(err))
	results, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(results, 1))
	qt.Assert(t, qt.Equals(output(t, results, errs), "a: int\n"))

	c, err = cli.ParseArgs(cli.ModeExport, []string{"./inc"})
	qt.Assert(t, qt.IsNil(err))
	_, errs = runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.ErrorMatches(errs[0], "(?s).*incomplete value.*"))
}

func TestSchemaExpression(t *testing.T) {
	files := baseFiles()
	files["work/sel/sel.cue"] = `package sel

#Item: {
	kind!: string
	n:     int
}
`
	l := newLoader(t, files, nil)
	c, err := cli.ParseArgs(cli.ModeVet, []string{"./sel", "kinds.yaml"})
	qt.Assert(t, qt.IsNil(err))
	c.Schemas = []string{"#Item"}

	_, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(errs, 2))
	qt.Assert(t, qt.IsNil(errs[0]))
	qt.Assert(t, qt.IsNil(errs[1]))

	// A schema the data does not satisfy reports per-document errors.
	c, err = cli.ParseArgs(cli.ModeVet, []string{"./sel", "data.json"})
	qt.Assert(t, qt.IsNil(err))
	c.Schemas = []string{"#Item"}
	_, errs = runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.IsNotNil(errs[0]))
}

func TestSourceInspectable(t *testing.T) {
	c, err := cli.ParseArgs(cli.ModeVet, []string{"schema.cue", "multi.yaml"})
	qt.Assert(t, qt.IsNil(err))
	src, err := c.Source()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(src.String(),
		`validate(decode("multi.yaml"), pkgFiles("schema.cue"), concrete)`))
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name    string
		mode    cli.Mode
		args    []string
		mut     func(*cli.Command)
		pattern string // matched against ParseArgs or Source error
	}{{
		name:    "UnknownQualifier",
		mode:    cli.ModeExport,
		args:    []string{"jsonx:", "x.json"},
		pattern: `unknown filetype tag "jsonx"`,
	}, {
		name:    "QualifierWithoutFile",
		mode:    cli.ModeExport,
		args:    []string{"json:"},
		pattern: `scoped qualifier "json:" without file`,
	}, {
		name:    "QualifierWithFileName",
		mode:    cli.ModeExport,
		args:    []string{"json:foo.data"},
		pattern: `cannot combine file type and file name; did you mean "json: foo.data"\?`,
	}, {
		name:    "TextAfterEllipsis",
		mode:    cli.ModeExport,
		args:    []string{"./.../x"},
		pattern: `pattern "\./\.\.\./x": text after \.\.\. is not supported`,
	}, {
		name:    "UnknownExtension",
		mode:    cli.ModeExport,
		args:    []string{"file.unknownext"},
		pattern: `unknown file extension for "file.unknownext"`,
	}, {
		name:    "VetDataWithoutSchema",
		mode:    cli.ModeVet,
		args:    []string{"data.json"},
		pattern: `data files specified without a schema`,
	}, {
		name: "WithContextWithoutPlacement",
		mode: cli.ModeExport,
		args: []string{"data.json"},
		mut: func(c *cli.Command) {
			c.WithContext = true
		},
		pattern: `the --with-context flag must be used with at least one of the --path, --list, or --files flags`,
	}, {
		name: "VetWithExpression",
		mode: cli.ModeVet,
		args: []string{"./pkg"},
		mut: func(c *cli.Command) {
			c.Expressions = []string{"a"}
		},
		pattern: `the -e/--expression flag is not supported in vet mode`,
	}, {
		name: "SchemaFlagWithPlacement",
		mode: cli.ModeExport,
		args: []string{"schema.cue", "data.json"},
		mut: func(c *cli.Command) {
			c.Schemas = []string{"a"}
			c.Path = []string{"x"}
		},
		pattern: `cannot combine the -d/--schema flag with the --path, --list, or --files flags`,
	}, {
		name:    "TooManyPackagesWithFiles",
		mode:    cli.ModeExport,
		args:    []string{"./pkg", "schema.cue", "data.json"},
		pattern: `too many packages defined \(2\) in combination with files`,
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := cli.ParseArgs(tc.mode, tc.args)
			if err == nil {
				if tc.mut != nil {
					tc.mut(c)
				}
				_, err = c.Source()
			}
			qt.Assert(t, qt.ErrorMatches(err, ".*"+tc.pattern+".*"))
		})
	}
}

func TestSchemaMarkedDataFile(t *testing.T) {
	// A data file marked "schema" constrains the value files.
	files := baseFiles()
	files["work/s.json"] = `{"x": "string-not-int"}`
	l := newLoader(t, files, nil)

	// s.json says x is a string; data.json has x: 1.
	c, err := cli.ParseArgs(cli.ModeVet, []string{"json+schema:", "s.json", "json:", "data.json"})
	qt.Assert(t, qt.IsNil(err))
	_, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.ErrorMatches(errs[0], "(?s).*conflicting values.*"))
}

func TestImportNotImplemented(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	c, err := cli.ParseArgs(cli.ModeImport, []string{"data.json"})
	qt.Assert(t, qt.IsNil(err))
	_, errs := runCmd(t, l, c)
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.ErrorMatches(errs[0], ".*import mode is not implemented yet.*"))
}
