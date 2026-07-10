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

// Package cli translates cmd/cue-style command lines — package patterns,
// file arguments with qualifiers such as "json:" and "cue+schema:", and
// the input-shaping flags — into cueload sources, and executes them. It
// is the layer cmd/cue itself is built on, made public so other tools
// can offer the same loading UX (see https://cuelang.org/issue/1410).
//
// This package owns everything command-shaped: the file-qualifier
// grammar, per-mode defaults (which output encoding, which form, which
// files count as schemas), orphan-file classification, -l placement with
// dynamic labels, --list assembly, per-document validation, stdin
// handling, and output-target parsing. It contains no logic that is not
// expressible via the public cueload, cuecodec, and cue APIs.
package cli

import (
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"
	"strings"

	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/cueload"
)

// Mode selects a per-command defaults profile, corresponding to the
// cmd/cue command in whose style arguments are interpreted.
type Mode int

const (
	ModeEval Mode = iota
	ModeExport
	ModeDef
	ModeVet
	ModeImport
)

// String returns the name of the cmd/cue command the mode corresponds
// to.
func (m Mode) String() string {
	switch m {
	case ModeEval:
		return "eval"
	case ModeExport:
		return "export"
	case ModeDef:
		return "def"
	case ModeVet:
		return "vet"
	case ModeImport:
		return "import"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// A Command describes a cmd/cue-style invocation: its arguments and the
// input-shaping flags. Field names follow the flags they represent.
//
// A Command is created by [ParseArgs]; the flag fields may be set
// afterwards, before calling [Command.Source] or [Command.Run].
type Command struct {
	// Mode selects the defaults profile.
	Mode Mode

	// Args holds the non-flag arguments: package patterns, file names,
	// qualifiers ("json:"), and "-" for standard input. It is set by
	// [ParseArgs] and is informational; modifying it after ParseArgs
	// has no effect.
	Args []string

	Expressions []string // -e: expressions to extract from each result
	Path        []string // -l: placement path elements (may be label expressions)
	Schemas     []string // -d: schema expressions to validate against

	List        bool  // --list: collect streamed documents into a list
	PerFile     bool  // --files: place each file separately (import)
	WithContext bool  // --with-context: expose {data, filename, index, recordCount} to -l expressions
	Merge       *bool // --merge: unify data files into the package (nil: mode default)

	// Tags (-t) holds tag values ("key=value") and build tags ("key").
	// InjectVars (-T) enables the standard tag variables (now, cwd,
	// os, ...). Both configure the loader, not the load plan: apply
	// them to a [cueload.Config] with [Command.ApplyToConfig] before
	// creating the loader passed to Run.
	Tags       []string
	InjectVars bool

	Out         string // --out/-o: output specification, e.g. "yaml:out.yaml"
	Force       bool   // -f: overwrite existing output files
	PackageName string // -p: package name for generated CUE (output naming only)
	FileFilter  string // -n: regexp filtering data files loaded from directories

	// Stdin substitutes for os.Stdin when an argument is "-".
	Stdin io.Reader

	// Fields below are set by ParseArgs.

	// pkgPatterns holds the arguments classified as package patterns.
	pkgPatterns []string
	// files holds the arguments classified as files, with their
	// resolved filetype qualifiers.
	files []fileArg

	// stdinRead, stdinData and stdinErr cache the one-shot read of
	// standard input, so that a Source can be loaded repeatedly.
	stdinRead bool
	stdinData []byte
	stdinErr  error
}

// ParseArgs parses non-flag arguments in cmd/cue syntax, classifying
// package patterns and qualified file arguments, and returns the
// resulting Command with mode defaults applied.
//
// The file arguments follow the grammar
//
//	file* (spec: file+)*
//
// where spec is of the form tag('+'tag)*, such as "json:" or
// "cue+schema:". A spec applies to the files after it, until the next
// spec. Arguments that look like package patterns (".", "./foo",
// "foo.com/bar", "foo.com/bar@v1", extension-less paths) are packages
// until the first bare spec is seen; after that, everything is a file.
func ParseArgs(mode Mode, args []string) (*Command, error) {
	c := &Command{Mode: mode, Args: slices.Clone(args)}
	fileScope := false
	var fileTokens []string
	for _, arg := range args {
		switch {
		case isScopeQualifier(arg):
			fileScope = true
			fileTokens = append(fileTokens, arg)
		case !fileScope && isPackage(arg):
			if err := checkPattern(arg); err != nil {
				return nil, err
			}
			c.pkgPatterns = append(c.pkgPatterns, arg)
		default:
			fileTokens = append(fileTokens, arg)
		}
	}
	files, err := parseFileArgs(fileTokens)
	if err != nil {
		return nil, err
	}
	c.files = files
	return c, nil
}

// Source returns the load plan the command denotes: an inspectable
// cueload.Source combining its packages, data files, schemas, placement,
// and expressions.
//
// Dynamic -l label expressions need a loader to evaluate labels against
// each record; a source obtained from this method reports an error for
// them when loaded directly. Use [Command.Run], which binds the loader.
func (c *Command) Source() (cueload.Source, error) {
	return c.compose(&runState{})
}

// Run executes the command against l, yielding one Result per output
// value. Per-value errors (a failing validation, a broken package, a
// non-concrete value under export) flow through the stream without
// terminating it; structural errors (bad flags, an unknown codec) end
// it. Results are not written anywhere: for modes with an output
// encoding each Result carries an [OutputFile], written on request via
// [OutputFile.Write].
func (c *Command) Run(ctx context.Context, l *cueload.Loader) iter.Seq2[Result, error] {
	return func(yield func(Result, error) bool) {
		if l == nil {
			yield(Result{}, fmt.Errorf("cli: Run requires a loader"))
			return
		}
		if c.Mode == ModeImport {
			yield(Result{}, fmt.Errorf("cli: import mode is not implemented yet"))
			return
		}
		src, err := c.compose(&runState{l: l})
		if err != nil {
			yield(Result{}, err)
			return
		}
		var enc *outputEncoder
		if c.Mode != ModeVet {
			spec, err := c.outputSpec()
			if err != nil {
				yield(Result{}, err)
				return
			}
			enc, err = newOutputEncoder(spec, c.PackageName)
			if err != nil {
				yield(Result{}, err)
				return
			}
		}
		for v, verr := range l.Load(ctx, src) {
			res := Result{Value: v}
			if o, ok := l.OriginOf(v); ok {
				res.Origin = o
			}
			if verr != nil {
				if !yield(res, verr) {
					return
				}
				continue
			}
			if enc != nil {
				out, err := enc.encode(ctx, v)
				if err != nil {
					if !yield(res, err) {
						return
					}
					continue
				}
				res.Output = out
			}
			if !yield(res, nil) {
				return
			}
		}
		if enc != nil {
			if err := enc.close(); err != nil {
				yield(Result{}, err)
			}
		}
	}
}

// ApplyToConfig applies the command's loader-shaping flags — the -t
// tags and -T tag variables — to a loader configuration. Call it before
// creating the loader passed to [Command.Run].
//
// Unlike cue/load, cueload deliberately un-conflates the two meanings
// of -t: an entry of the form "key=value" provides the value for
// @tag(key) attributes (Config.Tags), while a bare "key" both enables
// the build tag for @if(key) attributes (Config.BuildTags) and selects
// the shorthand of that name declared by @tag(...,short=...) attributes
// (a Tags entry with an empty value).
func (c *Command) ApplyToConfig(cfg *cueload.Config) {
	setTag := func(name, value string) {
		if cfg.Tags == nil {
			cfg.Tags = make(map[string]string)
		}
		cfg.Tags[name] = value
	}
	for _, t := range c.Tags {
		if name, value, ok := strings.Cut(t, "="); ok {
			setTag(name, value)
		} else {
			cfg.BuildTags = append(cfg.BuildTags, t)
			setTag(t, "")
		}
	}
	if c.InjectVars {
		cfg.TagVars = DefaultTagVars()
	}
}

// A Result is one output of a command: a value together with its origin
// and, for modes with an output encoding, the encoded output file.
type Result struct {
	// Value is the resulting value, when the command produces values.
	Value cue.Value

	// Origin identifies the input that produced the value, when there
	// is exactly one (values assembled from several inputs, such as a
	// merged package, have no origin).
	Origin cueload.Origin

	// Output is the encoded output, for modes with an output encoding
	// (eval, export, def). It is nil for vet and for values that
	// failed with a per-value error.
	Output *OutputFile
}

// readStdin reads standard input at most once per command, caching the
// result so that the composed Source can be loaded repeatedly.
func (c *Command) readStdin() ([]byte, error) {
	if !c.stdinRead {
		r := c.Stdin
		if r == nil {
			r = os.Stdin
		}
		c.stdinData, c.stdinErr = io.ReadAll(r)
		c.stdinRead = true
	}
	return c.stdinData, c.stdinErr
}
