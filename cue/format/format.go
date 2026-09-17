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

// Package format implements standard formatting of CUE configurations.
package format

// TODO: this package is in need of a rewrite. When doing so, the API should
// allow for reformatting an AST, without actually writing bytes.
//
// In essence, formatting determines the relative spacing to tokens. It should
// be possible to have an abstract implementation providing such information
// that can be used to either format or update an AST in a single walk.

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"text/tabwriter"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/pretty"
)

// An Option sets behavior of the formatter.
type Option func(c *config)

// Simplify allows the formatter to simplify output, such as removing
// unnecessary quotes.
func Simplify() Option {
	return func(c *config) { c.simplify = true }
}

// Version specifies the CUE language version that [Source] parses its input
// at, which decides what syntax is accepted; from v0.17.0 on, for instance,
// commas between list elements on separate lines may be omitted. It has no
// effect on the formatted output. An empty version string means the current
// language version. The version must be a valid semantic version string as
// checked by [semver.IsValid].
func Version(v string) Option {
	return func(c *config) { c.languageVersion = v }
}

// Indent specifies the string emitted for one level of indentation.
// The empty string disables indentation entirely; common choices are
// "\t" for tabs and a fixed run of spaces for space-based indentation.
// The default is "\t".
//
// This option is only honored by the formatv2 formatter, so using it
// selects that formatter even when the formatv2 experiment is disabled.
func Indent(s string) Option {
	return func(c *config) {
		c.forceV2 = true
		c.indent = &s
	}
}

// IndentWidth specifies the visual column width of one level of
// indentation. The formatv2 formatter uses it for its line-breaking
// heuristics; the pre-v2 formatter uses it as the tab width for
// aligning columns and, with [TabIndent] set to false, as the number
// of spaces in one level of indentation.
//
// When it is left unset, the width is derived from the indentation
// string: zero when there is no indentation, four for a tab, and the
// number of characters otherwise. The pre-v2 formatter defaults to
// eight.
func IndentWidth(n int) Option {
	return func(c *config) { c.indentWidth = n }
}

// IndentPrefix specifies a number of indentation levels to emit at
// the start of every line, in addition to the indentation of the
// code itself.
//
// This option is only honored by the formatv2 formatter. Unlike the
// options that formatv2 introduced, it does not select that formatter,
// because it predates it: the pre-v2 formatter ignores it, as it
// always has.
func IndentPrefix(n int) Option {
	return func(c *config) { c.indentPrefix = n }
}

// LineWidth specifies the target line width that the line-breaking
// heuristics aim to stay within. It defaults to 120.
//
// This option is only honored by the formatv2 formatter, so using it
// selects that formatter even when the formatv2 experiment is disabled.
func LineWidth(n int) Option {
	return func(c *config) {
		c.forceV2 = true
		c.lineWidth = n
	}
}

// KeepRelPos specifies that the relative positions of nodes, comments
// and blank lines are kept as they are found in the AST rather than
// being adjusted to CUE's conventional layout, as described by
// [ASTStyle].RelPos. The simplifications implied by [Simplify] are
// still applied when that option is given; without it, the AST is
// formatted entirely unchanged.
//
// This option is only honored by the formatv2 formatter, so using it
// selects that formatter even when the formatv2 experiment is disabled.
func KeepRelPos() Option {
	return func(c *config) {
		c.forceV2 = true
		c.keepRelPos = true
	}
}

// Compact formats the node as compact CUE: the authored line layout and
// all the comments are discarded, so the formatter lays the node out
// with its width-driven heuristics alone rather than reproducing an
// authored layout. This option also makes the target line width effectively
// unbounded, so the only line breaks are the ones the syntax requires,
// such as those inside multi-line strings; specify [LineWidth] as a later
// option to wrap the result at a given width instead.
//
// This option is only honored by the formatv2 formatter, so using it
// selects that formatter even when the formatv2 experiment is disabled.
func Compact() Option {
	return func(c *config) {
		c.forceV2 = true
		c.compact = true
		c.lineWidth = math.MaxInt
	}
}

// UseSpaces sets the indentation width. Despite its name, it does not
// by itself cause spaces to be used for indentation; see [TabIndent].
//
// Deprecated: use [IndentWidth], and [Indent] to indent with spaces.
//
//go:fix inline
func UseSpaces(tabwidth int) Option { return IndentWidth(tabwidth) }

// TabIndent specifies whether to use tabs for indentation. When
// false, one level of indentation is a run of spaces as wide as the
// indentation width (see [IndentWidth]).
//
// This option is set to true by default.
//
// Deprecated: use [Indent] with "\t" or a run of spaces.
func TabIndent(indent bool) Option {
	return func(c *config) { c.spaceIndent = !indent }
}

// TODO: other options:
//
// const (
// 	RawFormat Mode = 1 << iota // do not use a tabwriter; if set, UseSpaces is ignored
// 	TabIndent                  // use tabs for indentation independent of UseSpaces
// 	UseSpaces                  // use spaces instead of tabs for alignment
// 	SourcePos                  // emit //line comments to preserve original source positions
// )

// Node formats node in canonical cue fmt style and writes the result to dst.
//
// The node type must be [*ast.File], [][ast.Decl], [ast.Expr],
// [ast.Decl], or [ast.Spec]. Imports are not sorted for nodes
// representing partial source files (for instance, if the node is not
// an *ast.File).
//
// The AST is never mutated; see [NodeInPlace] for a variant that may.
//
// If the AST is incorrect, a partial result may be returned with an error.
func Node(n ast.Node, opt ...Option) ([]byte, error) {
	return formatNode(n, opt, false)
}

// NodeInPlace is like [Node] except that it allows the AST tree
// to be mutated in place, avoiding a potentially expensive copy
// operation. The node must not be used concurrently by other
// goroutines, and its contents are unspecified afterwards.
func NodeInPlace(n ast.Node, opt ...Option) ([]byte, error) {
	return formatNode(n, opt, true)
}

func formatNode(node ast.Node, opt []Option, canMutate bool) ([]byte, error) {
	cueexperiment.Init()

	cfg := newConfig(opt)
	if !cfg.formatV2() {
		if cfg.simplify && !canMutate {
			node = ast.Clone(node) // Ensure immutability of the AST.
		}
		return cfg.fprint(node)
	}

	scfg := cfg.style()
	if !canMutate && scfg.WouldMutate(node) {
		node = ast.Clone(node) // Ensure immutability of the AST.
	}
	scfg.Apply(node)
	return cfg.v2().Node(node)
}

// Source formats src in canonical cue fmt style and returns the result or an
// (I/O or syntax) error. src is expected to be a syntactically correct CUE
// source file, or a list of CUE declarations or statements.
//
// If src is a partial source file, the leading and trailing space of src is
// applied to the result (such that it has the same leading and trailing space
// as src), and the result is indented by the same amount as the first line of
// src containing code. Imports are not sorted for partial source files.
//
// Caution: Tools relying on consistent formatting based on the installed
// version of cue (for instance, such as for presubmit checks) should execute
// that cue binary instead of calling Source.
func Source(b []byte, opt ...Option) ([]byte, error) {
	cueexperiment.Init()

	cfg := newConfig(opt)

	// Parse at the configured language version, so that syntax which only
	// some versions accept is parsed the way the caller means it.
	parseOpts := []parser.Option{parser.ParseComments, parser.Version(cfg.languageVersion)}
	f, err := parser.ParseFile("", b, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("parse: %s", err)
	}

	if !cfg.formatV2() {
		return cfg.fprint(f)
	}

	// The AST is our own, so there is no need to preserve it.
	cfg.style().Apply(f)
	return cfg.v2().Node(f)
}

type config struct {
	keepRelPos      bool // default: false
	compact         bool // default: false
	simplify        bool // default: false
	languageVersion string

	// indent holds the indentation string set by [Indent], or nil when
	// it is to be derived from spaceIndent and indentWidth.
	indent *string
	// spaceIndent records the deprecated TabIndent(false): when indent
	// is nil, one level of indentation is indentWidth spaces rather
	// than a tab.
	spaceIndent  bool
	indentWidth  int // 0 means unset; the v1 formatter defaults to 8, v2 to 4
	indentPrefix int // default: 0
	lineWidth    int // 0 means unset; the v2 formatter defaults to 120

	// forceV2 records that an option only the v2 formatter implements
	// was used, which selects that formatter for this call even when
	// the formatv2 experiment is explicitly disabled.
	forceV2 bool
}

// style builds the [ASTStyle] that drives the v2 pre-pass.
func (cfg *config) style() ASTStyle {
	return ASTStyle{
		// The house-style layout hints would undo the compaction, so
		// they are withheld for a compact layout.
		RelPos:         !cfg.keepRelPos && !cfg.compact,
		InlineStructs:  cfg.simplify,
		Labels:         cfg.simplify,
		Ellipsis:       cfg.simplify,
		ClearPositions: cfg.compact,
		ClearComments:  cfg.compact,
	}
}

// formatV2 reports whether this call uses the v2 formatter.
func (cfg *config) formatV2() bool {
	return cueexperiment.Flags.FormatV2 || cfg.forceV2
}

func newConfig(opt []Option) *config {
	cfg := &config{}
	for _, o := range opt {
		o(cfg)
	}
	return cfg
}

// indentWidthOr returns the configured indentation width, or def if it
// was left unset. The pre-v2 and v2 formatters resolve different
// defaults so that the v1 formatter stays consistent with
// release-branch.v0.17 (8) without changing the formatv2 default (4).
func (cfg *config) indentWidthOr(def int) int {
	if cfg.indentWidth == 0 {
		return def
	}
	return cfg.indentWidth
}

func (cfg *config) v2() *pretty.Config {
	cfgV2 := &pretty.Config{
		IndentWidth: cfg.indentWidth,
		Width:       cfg.lineWidth,
	}
	switch {
	case cfg.indent != nil:
		cfgV2.Indent = *cfg.indent
	case cfg.spaceIndent:
		cfgV2.Indent = strings.Repeat(" ", cfg.indentWidthOr(4))
	default:
		cfgV2.Indent = "\t"
	}
	cfgV2.Prefix = strings.Repeat(cfgV2.Indent, cfg.indentPrefix)
	return cfgV2
}

// Config defines the output of Fprint.
func (cfg *config) fprint(node any) (out []byte, err error) {
	var p printer
	p.init(cfg)
	if err = printNode(node, &p); err != nil {
		return p.output, err
	}

	twmode := tabwriter.StripEscape | tabwriter.TabIndent | tabwriter.DiscardEmptyColumns

	tabWidth := cfg.indentWidthOr(8)
	buf := &bytes.Buffer{}
	tw := tabwriter.NewWriter(buf, 0, tabWidth, 1, ' ', twmode)

	// write printer result via tabwriter/trimmer to output
	if _, err = tw.Write(p.output); err != nil {
		return
	}

	err = tw.Flush()
	if err != nil {
		return buf.Bytes(), err
	}

	b := buf.Bytes()
	if cfg.spaceIndent {
		b = bytes.ReplaceAll(b, []byte{'\t'}, bytes.Repeat([]byte{' '}, tabWidth))
	}
	return b, nil
}

// A formatter walks an [ast.Node], interspersed with comments and spacing
// directives, in the order that they would occur in printed form.
type formatter struct {
	*printer

	stack    []frame
	current  frame
	nestExpr int
}

func newFormatter(p *printer) *formatter {
	f := &formatter{
		printer: p,
		current: frame{
			settings: settings{
				nodeSep:   newline,
				parentSep: newline,
			},
		},
	}
	return f
}

type whiteSpace int

const (
	_ whiteSpace = 0

	// write a space, or disallow it
	blank whiteSpace = 1 << iota
	vtab             // column marker
	noblank

	nooverride

	comma      // print a comma, unless trailcomma overrides it
	trailcomma // print a trailing comma unless closed on same line
	declcomma  // write a comma when not at the end of line (same as struct field separator)

	newline    // write a line in a table
	formfeed   // next line is not part of the table
	newsection // add two newlines

	indent   // request indent an extra level after the next newline
	unindent // unindent a level after the next newline
	indented // element was indented.
)

type frame struct {
	cg  []*ast.CommentGroup
	pos int8

	settings
}

type settings struct {
	// separator is blank if the current node spans a single line and newline
	// otherwise.
	nodeSep   whiteSpace
	parentSep whiteSpace
	override  whiteSpace
}

// suppress spurious linter warning: field is actually used.
func init() {
	s := settings{}
	_ = s.override
}

func (f *formatter) print(a ...any) {
	for _, x := range a {
		f.Print(x)
		switch x.(type) {
		case string, token.Token: // , *ast.BasicLit, *ast.Ident:
			f.current.pos++
		}
	}
}

func (f *formatter) formfeed() whiteSpace {
	if f.current.nodeSep == blank {
		return blank
	}
	return formfeed
}

func (f *formatter) onOneLine(node ast.Node) bool {
	a := node.Pos()
	b := node.End()
	if a.IsValid() && b.IsValid() {
		return f.lineFor(a) == f.lineFor(b)
	}
	// TODO: walk and look at relative positions to determine the same?
	return false
}

func (f *formatter) before(node ast.Node) bool {
	f.stack = append(f.stack, f.current)
	f.current = frame{settings: f.current.settings}
	f.current.parentSep = f.current.nodeSep

	if node != nil {
		s, ok := node.(*ast.StructLit)
		if ok && len(s.Elts) <= 1 && f.current.nodeSep != blank && f.onOneLine(node) {
			f.current.nodeSep = blank
		}
		f.current.cg = ast.Comments(node)
		f.visitComments(f.current.pos)
		return true
	}
	return false
}

func (f *formatter) after(node ast.Node) {
	f.visitComments(127)
	p := len(f.stack) - 1
	f.current = f.stack[p]
	f.stack = f.stack[:p]
	f.current.pos++
	f.visitComments(f.current.pos)
}

func (f *formatter) visitComments(until int8) {
	c := &f.current

	printed := false
	for ; len(c.cg) > 0 && c.cg[0].Position <= until; c.cg = c.cg[1:] {
		if printed {
			f.Print(newsection)
		}
		printed = true
		f.printComment(c.cg[0])
	}
}

func (f *formatter) printComment(cg *ast.CommentGroup) {
	f.Print(cg)

	if cg.Doc && len(f.output) > 0 {
		f.Print(newline)
	}
	for _, c := range cg.List {
		if f.pos.Column > 1 {
			// Vertically align inline comments.
			f.Print(vtab)
		}
		f.Print(c.Slash)
		f.Print(c)
		f.printingComment = true
		f.Print(newline)
		if cg.Doc {
			f.Print(nooverride)
		}
	}
}
