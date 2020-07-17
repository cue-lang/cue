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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// Disallow
// - block comments
// - old-style field comprehensions
// - space separator syntax
const syntaxVersion = -1000 + 100*2 + 1

var requestedVersion = os.Getenv("CUE_SYNTAX_OVERRIDE")

var defaultConfig = config{
	loadCfg: &load.Config{
		ParseFile: func(name string, src interface{}) (*ast.File, error) {
			version := syntaxVersion
			if requestedVersion != "" {
				switch {
				case strings.HasPrefix(requestedVersion, "v0.1"):
					version = -1000 + 100
				}
			}
			return parser.ParseFile(name, src,
				parser.FromVersion(version),
				parser.ParseComments,
			)
		},
	},
}

var runtime = &cue.Runtime{}

var inTest = false

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

	cfg *config

	// If orphanFiles are mixed with CUE files and/or if placement flags are used,
	// the instance is also included in insts.
	importing      bool
	mergeData      bool // do not merge individual data files.
	orphaned       []*build.File
	orphanInstance *build.Instance
	// imported files are files that were orphaned in the build instance, but
	// were placed in the instance by using one the --files, --list or --path
	// flags.
	imported []*ast.File

	expressions []ast.Expr // only evaluate these expressions within results
	schema      ast.Expr   // selects schema in instance for orphaned values

	// outFile defines the file to output to. Default is CUE stdout.
	outFile *build.File

	encConfig *encoding.Config
	merge     []*build.Instance
}

// instances iterates either over a list of instances, or a list of
// data files. In the latter case, there must be either 0 or 1 other
// instance, with which the data instance may be merged.
func (b *buildPlan) instances() iterator {
	var i iterator
	if len(b.orphaned) == 0 {
		i = &instanceIterator{a: buildInstances(b.cmd, b.insts), i: -1}
	} else {
		i = newStreamingIterator(b)
	}
	if len(b.expressions) > 0 {
		return &expressionIter{
			iter: i,
			expr: b.expressions,
			i:    len(b.expressions),
		}
	}
	return i
}

type iterator interface {
	scan() bool
	instance() *cue.Instance
	file() *ast.File // may return nil
	err() error
	close()
	id() string
}

type instanceIterator struct {
	a []*cue.Instance
	i int
	e error
}

func (i *instanceIterator) scan() bool {
	i.i++
	return i.i < len(i.a) && i.e == nil
}

func (i *instanceIterator) close()                  {}
func (i *instanceIterator) err() error              { return i.e }
func (i *instanceIterator) instance() *cue.Instance { return i.a[i.i] }
func (i *instanceIterator) file() *ast.File         { return nil }
func (i *instanceIterator) id() string              { return i.a[i.i].Dir }

type streamingIterator struct {
	r    *cue.Runtime
	inst *cue.Instance
	base cue.Value
	b    *buildPlan
	cfg  *encoding.Config
	a    []*build.File
	dec  *encoding.Decoder
	i    *cue.Instance
	f    *ast.File
	e    error
}

func newStreamingIterator(b *buildPlan) *streamingIterator {
	i := &streamingIterator{
		cfg: b.encConfig,
		a:   b.orphaned,
		b:   b,
	}

	// TODO: use orphanedSchema
	switch len(b.insts) {
	case 0:
		i.r = &cue.Runtime{}
	case 1:
		p := b.insts[0]
		inst := buildInstances(b.cmd, []*build.Instance{p})[0]
		if inst.Err != nil {
			return &streamingIterator{e: inst.Err}
		}
		i.r = internal.GetRuntime(inst).(*cue.Runtime)
		if b.schema == nil {
			i.base = inst.Value()
		} else {
			i.base = inst.Eval(b.schema)
			if err := i.base.Err(); err != nil {
				return &streamingIterator{e: err}
			}
		}
	default:
		return &streamingIterator{e: errors.Newf(token.NoPos,
			"cannot combine data streaming with multiple instances")}
	}

	return i
}

func (i *streamingIterator) file() *ast.File         { return i.f }
func (i *streamingIterator) instance() *cue.Instance { return i.i }

func (i *streamingIterator) id() string {
	if i.inst != nil {
		return i.inst.Dir
	}
	return ""
}

func (i *streamingIterator) scan() bool {
	if i.e != nil {
		return false
	}

	// advance to next value
	if i.dec != nil && !i.dec.Done() {
		i.dec.Next()
	}

	// advance to next stream if necessary
	for i.dec == nil || i.dec.Done() {
		if i.dec != nil {
			i.dec.Close()
			i.dec = nil
		}
		if len(i.a) == 0 {
			return false
		}

		i.dec = encoding.NewDecoder(i.a[0], i.cfg)
		if i.e = i.dec.Err(); i.e != nil {
			return false
		}
		i.a = i.a[1:]
	}

	// compose value
	i.f = i.dec.File()
	inst, err := i.r.CompileFile(i.f)
	if err != nil {
		i.e = err
		return false
	}
	i.i = inst
	if i.base.Exists() {
		i.e = i.base.Err()
		if i.e == nil {
			i.i, i.e = i.i.Fill(i.base)
			i.i.DisplayName = internal.DebugStr(i.b.schema)
			if inst.DisplayName != "" {
				i.i.DisplayName = fmt.Sprintf("%s|%s", inst.DisplayName, i.i.DisplayName)
			}
		}
		i.f = nil
	}
	return i.e == nil
}

func (i *streamingIterator) close() {
	if i.dec != nil {
		i.dec.Close()
		i.dec = nil
	}
}

func (i *streamingIterator) err() error {
	if i.dec != nil {
		if err := i.dec.Err(); err != nil {
			return err
		}
	}
	return i.e
}

type expressionIter struct {
	iter iterator
	expr []ast.Expr
	i    int
}

func (i *expressionIter) err() error { return i.iter.err() }
func (i *expressionIter) close()     { i.iter.close() }
func (i *expressionIter) id() string { return i.iter.id() }

func (i *expressionIter) scan() bool {
	i.i++
	if i.i < len(i.expr) {
		return true
	}
	if !i.iter.scan() {
		return false
	}
	i.i = 0
	return true
}

func (i *expressionIter) file() *ast.File { return nil }

func (i *expressionIter) instance() *cue.Instance {
	if len(i.expr) == 0 {
		return i.iter.instance()
	}
	inst := i.iter.instance()
	v := i.iter.instance().Eval(i.expr[i.i])
	ni := internal.MakeInstance(v).(*cue.Instance)
	ni.DisplayName = fmt.Sprintf("%s|%s", inst.DisplayName, i.expr[i.i])
	return ni
}

type config struct {
	outMode filetypes.Mode

	fileFilter     string
	interpretation build.Interpretation

	noMerge bool // do not merge individual data files.

	loadCfg *load.Config
}

func parseArgs(cmd *Command, args []string, cfg *config) (p *buildPlan, err error) {
	if cfg == nil {
		cfg = &defaultConfig
	}
	if cfg.loadCfg == nil {
		cfg.loadCfg = defaultConfig.loadCfg
	}
	cfg.loadCfg.Stdin = cmd.InOrStdin()

	p = &buildPlan{cfg: cfg, cmd: cmd, importing: cfg.loadCfg.DataFiles}

	if err := p.parseFlags(); err != nil {
		return nil, err
	}

	builds := loadFromArgs(cmd, args, cfg.loadCfg)
	if builds == nil {
		return nil, errors.Newf(token.NoPos, "invalid args")
	}
	decorateInstances(cmd, flagInject.StringArray(cmd), builds)

	for _, b := range builds {
		if b.Err != nil {
			return nil, b.Err
		}
		var ok bool
		if b.User || p.importing {
			ok, err = p.placeOrphans(b)
			if err != nil {
				return nil, err
			}
		}
		if !b.User {
			p.insts = append(p.insts, b)
			continue
		}
		addedUser := false
		if len(b.BuildFiles) > 0 {
			addedUser = true
			p.insts = append(p.insts, b)
		}
		if ok {
			continue
		}

		if len(b.OrphanedFiles) == 0 {
			continue
		}

		if p.orphanInstance != nil {
			return nil, errors.Newf(token.NoPos,
				"builds contain two file packages")
		}
		p.orphanInstance = b
		p.encConfig.Stream = true

		for _, f := range b.OrphanedFiles {
			switch f.Encoding {
			case build.Protobuf, build.YAML, build.JSON, build.Text:
			default:
				return nil, errors.Newf(token.NoPos,
					"unsupported encoding %q", f.Encoding)
			}
		}

		// TODO: this processing could probably be delayed, or at least
		// simplified. The main reason to do this here is to allow interpreting
		// the --schema/-d flag, while allowing to use this for OpenAPI and
		// JSON Schema in auto-detect mode.
		buildFiles := []*build.File{}
		for _, f := range b.OrphanedFiles {
			d := encoding.NewDecoder(f, p.encConfig)
			for ; !d.Done(); d.Next() {
				file := d.File()
				sub := &build.File{
					Filename:       d.Filename(),
					Encoding:       f.Encoding,
					Interpretation: d.Interpretation(),
					Form:           f.Form,
					Tags:           f.Tags,
					Source:         file,
				}
				if (!p.mergeData || p.schema != nil) && d.Interpretation() == "" {
					switch sub.Encoding {
					case build.YAML, build.JSON, build.Text:
						p.orphaned = append(p.orphaned, sub)
						continue
					}
				}
				buildFiles = append(buildFiles, sub)
				if err := b.AddSyntax(file); err != nil {
					return nil, err
				}
			}
			if err := d.Err(); err != nil {
				return nil, err
			}
		}

		if !addedUser && len(b.Files) > 0 {
			p.insts = append(p.insts, b)
		} else if len(p.orphaned) == 0 {
			// Instance with only a single build: just print the file.
			p.orphaned = append(p.orphaned, buildFiles...)
		}
	}

	if len(p.insts) > 1 && p.schema != nil {
		return nil, errors.Newf(token.NoPos,
			"cannot use --schema/-d flag more than one schema")
	}

	if len(p.expressions) > 1 {
		p.encConfig.Stream = true
	}
	return p, nil
}

func (b *buildPlan) parseFlags() (err error) {
	b.mergeData = !b.cfg.noMerge && flagMerge.Bool(b.cmd)

	out := flagOut.String(b.cmd)
	outFile := flagOutFile.String(b.cmd)

	if strings.Contains(out, ":") && strings.Contains(outFile, ":") {
		return errors.Newf(token.NoPos,
			"cannot specify qualifier in both --out and --outfile")
	}
	if outFile == "" {
		outFile = "-"
	}
	if out != "" {
		outFile = out + ":" + outFile
	}
	b.outFile, err = filetypes.ParseFile(outFile, b.cfg.outMode)
	if err != nil {
		return err
	}

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
	if s := flagGlob.String(b.cmd); s != "" {
		// Set a default file filter to only include json and yaml files
		b.cfg.fileFilter = s
	}
	b.encConfig = &encoding.Config{
		Mode:      b.cfg.outMode,
		Stdin:     b.cmd.InOrStdin(),
		Stdout:    b.cmd.OutOrStdout(),
		ProtoPath: flagProtoPath.StringArray(b.cmd),
		AllErrors: flagAllErrors.Bool(b.cmd),
		PkgName:   flagPackage.String(b.cmd),
		Strict:    flagStrict.Bool(b.cmd),
	}
	return nil
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

func shortFile(root string, f *build.File) string {
	dir, _ := filepath.Rel(root, f.Filename)
	if dir == "" {
		return f.Filename
	}
	if !filepath.IsAbs(dir) {
		dir = "." + string(filepath.Separator) + dir
	}
	return dir
}
