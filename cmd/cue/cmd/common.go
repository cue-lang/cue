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

package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"
)

// Disallow
// - block comments
// - old-style field comprehensions
// - space separator syntax
const syntaxVersion = -1000 + 13

var defaultConfig = &load.Config{
	Context: build.NewContext(
		build.ParseFile(func(name string, src interface{}) (*ast.File, error) {
			return parser.ParseFile(name, src,
				parser.FromVersion(syntaxVersion),
				parser.ParseComments,
			)
		})),
}

var runtime = &cue.Runtime{}

var inTest = false

func mustParseFlags(t *testing.T, cmd *cobra.Command, flags ...string) {
	if err := cmd.ParseFlags(flags); err != nil {
		t.Fatal(err)
	}
}

func exitIfErr(cmd *Command, inst *cue.Instance, err error, fatal bool) {
	exitOnErr(cmd, err, fatal)
}

func getLang() language.Tag {
	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	loc = strings.Split(loc, ".")[0]
	return language.Make(loc)
}

func exitOnErr(cmd *Command, err error, fatal bool) {
	if err == nil {
		return
	}

	// Link x/text as our localizer.
	p := message.NewPrinter(getLang())
	format := func(w io.Writer, format string, args ...interface{}) {
		p.Fprintf(w, format, args...)
	}

	cwd, _ := os.Getwd()

	w := &bytes.Buffer{}
	errors.Print(w, err, &errors.Config{
		Format:  format,
		Cwd:     cwd,
		ToSlash: inTest,
	})

	b := w.Bytes()
	_, _ = cmd.Stderr().Write(b)
	if fatal {
		exit()
	}
}

func loadFromArgs(cmd *Command, args []string, cfg *load.Config) []*build.Instance {
	binst := load.Instances(args, cfg)
	if len(binst) == 0 {
		return nil
	}

	return binst
}

// A buildPlan defines what should be done based on command line
// arguments and flags.
//
// TODO: allow --merge/-m to mix in other packages.
type buildPlan struct {
	cmd   *Command
	insts []*build.Instance

	// If orphanFiles are mixed with CUE files and/or if placement flags are used,
	// the instance is also included in insts.
	forceOrphanProcessing bool
	orphanedData          []*build.File
	orphanedSchema        []*build.File
	orphanInstance        *build.Instance
	// imported files are files that were orphaned in the build instance, but
	// were placed in the instance by using one the --files, --list or --path
	// flags.
	imported []*ast.File

	expressions []ast.Expr // only evaluate these expressions within results
	schema      ast.Expr   // selects schema in instance for orphaned values

	encConfig *encoding.Config
	merge     []*build.Instance
}

func (b *buildPlan) instances() []*cue.Instance {
	if len(b.insts) == 0 {
		return nil
	}
	return buildInstances(b.cmd, b.insts)
}

func parseArgs(cmd *Command, args []string, cfg *load.Config) (p *buildPlan, err error) {
	if cfg == nil {
		cfg = defaultConfig
	}
	builds := loadFromArgs(cmd, args, cfg)
	if builds == nil {
		return nil, errors.Newf(token.NoPos, "invalid args")
	}
	decorateInstances(cmd, flagTags.StringArray(cmd), builds)

	p = &buildPlan{cmd: cmd, forceOrphanProcessing: cfg.DataFiles}

	if err := p.parseFlags(); err != nil {
		return nil, err
	}

	for _, b := range builds {
		var ok bool
		if b.User || p.forceOrphanProcessing {
			ok, err = p.placeOrphans(b)
			if err != nil {
				return nil, err
			}
		}
		if !b.User {
			p.insts = append(p.insts, b)
			continue
		}
		if len(b.BuildFiles) > 0 {
			p.insts = append(p.insts, b)
		}
		if ok {
			continue
		}

		if len(b.OrphanedFiles) > 0 {
			if p.orphanInstance != nil {
				return nil, errors.Newf(token.NoPos,
					"builds contain two file packages")
			}
			p.orphanInstance = b
		}

		for _, f := range b.OrphanedFiles {
			switch f.Interpretation {
			case build.JSONSchema, build.OpenAPI:
				p.orphanedSchema = append(p.orphanedSchema, f)
				continue
			}
			switch f.Encoding {
			case build.Protobuf:
				p.orphanedSchema = append(p.orphanedSchema, f)
			case build.YAML, build.JSON, build.Text:
				p.orphanedData = append(p.orphanedData, f)
			default:
				return nil, errors.Newf(token.NoPos,
					"unsupported encoding %q", f.Encoding)
			}
		}
	}

	return p, nil
}

func (b *buildPlan) parseFlags() (err error) {
	for _, e := range flagExpression.StringArray(b.cmd) {
		expr, err := parser.ParseExpr("--expression", e)
		if err != nil {
			return err
		}
		b.expressions = append(b.expressions, expr)
	}
	if s := flagSchema.String(b.cmd); s != "" {
		b.schema, err = parser.ParseExpr("--schema", s)
		if err != nil {
			return err
		}
	}
	b.encConfig = &encoding.Config{
		Stdin:     stdin,
		Stdout:    b.cmd.OutOrStdout(),
		ProtoPath: flagProtoPath.StringArray(b.cmd),
	}
	return nil
}

func (b *buildPlan) singleInstance() *cue.Instance {
	var p *build.Instance
	switch len(b.insts) {
	case 0:
		return nil
	case 1:
		p = b.insts[0]
	default:
		exitOnErr(b.cmd, errors.Newf(token.NoPos,
			"cannot combine data streaming with multiple instances"), true)
		return nil
	}
	return buildInstances(b.cmd, []*build.Instance{p})[0]
}

func buildInstances(cmd *Command, binst []*build.Instance) []*cue.Instance {
	// TODO:
	// If there are no files and User is true, then use those?
	// Always use all files in user mode?
	instances := cue.Build(binst)
	for _, inst := range instances {
		// TODO: consider merging errors of multiple files, but ensure
		// duplicates are removed.
		exitIfErr(cmd, inst, inst.Err, true)
	}

	if flagIgnore.Bool(cmd) {
		return instances
	}

	// TODO check errors after the fact in case of ignore.
	for _, inst := range instances {
		// TODO: consider merging errors of multiple files, but ensure
		// duplicates are removed.
		exitIfErr(cmd, inst, inst.Value().Validate(), !flagIgnore.Bool(cmd))
	}
	return instances
}

func buildToolInstances(cmd *Command, binst []*build.Instance) ([]*cue.Instance, error) {
	instances := cue.Build(binst)
	for _, inst := range instances {
		if inst.Err != nil {
			return nil, inst.Err
		}
	}

	// TODO check errors after the fact in case of ignore.
	for _, inst := range instances {
		if err := inst.Value().Validate(); err != nil {
			return nil, err
		}
	}
	return instances, nil
}

func buildTools(cmd *Command, tags, args []string) (*cue.Instance, error) {
	binst := loadFromArgs(cmd, args, &load.Config{Tools: true})
	if len(binst) == 0 {
		return nil, nil
	}
	included := map[string]bool{}

	ti := binst[0].Context().NewInstance(binst[0].Root, nil)
	for _, inst := range binst {
		for _, f := range inst.ToolCUEFiles {
			if file := inst.Abs(f); !included[file] {
				_ = ti.AddFile(file, nil)
				included[file] = true
			}
		}
	}
	decorateInstances(cmd, tags, append(binst, ti))

	insts, err := buildToolInstances(cmd, binst)
	if err != nil {
		return nil, err
	}

	inst := cue.Merge(insts...).Build(ti)
	return inst, inst.Err
}
