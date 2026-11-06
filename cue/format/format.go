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
//
// [Node] and [Source] print CUE in its conventional style.
// To rewrite an AST in that style without printing it, see [ASTStyle].
package format

import (
	"fmt"
	"math"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/pretty"
)

// An Option sets behavior of the formatter.
type Option func(c *config)

// Simplify allows the formatter to simplify output, such as removing
// unnecessary quotes. It applies the [ASTStyle.Labels],
// [ASTStyle.InlineStructs], and [ASTStyle.Ellipsis] rewrites before printing.
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
func Indent(s string) Option {
	return func(c *config) { c.indent = &s }
}

// IndentWidth specifies the visual column width of one level of
// indentation, which the line-breaking heuristics use.
//
// When it is left unset, the width is derived from the indentation
// string set by [Indent]: zero when there is no indentation, four for
// a tab, and the number of characters otherwise.
func IndentWidth(n int) Option {
	return func(c *config) { c.indentWidth = n }
}

// IndentPrefix specifies a number of indentation levels to emit at
// the start of every line, in addition to the indentation of the
// code itself.
func IndentPrefix(n int) Option {
	return func(c *config) { c.indentPrefix = n }
}

// LineWidth specifies the target line width that the line-breaking
// heuristics aim to stay within. It defaults to 120.
func LineWidth(n int) Option {
	return func(c *config) { c.lineWidth = n }
}

// KeepRelPos specifies that the relative positions of nodes, comments
// and blank lines are kept as they are found in the AST rather than
// being adjusted to CUE's conventional layout, as described by
// [ASTStyle.RelPos]. The simplifications implied by [Simplify] are
// still applied when that option is given; without it, the AST is
// formatted entirely unchanged.
func KeepRelPos() Option {
	return func(c *config) { c.keepRelPos = true }
}

// Compact formats the node as compact CUE: the authored line layout and
// all the comments are discarded, so the formatter lays the node out
// with its width-driven heuristics alone rather than reproducing an
// authored layout. This option also makes the target line width effectively
// unbounded, so the only line breaks are the ones the syntax requires,
// such as those inside multi-line strings; specify [LineWidth] as a later
// option to wrap the result at a given width instead.
//
// It applies the [ASTStyle.ClearPositions] and [ASTStyle.ClearComments]
// rewrites before printing.
func Compact() Option {
	return func(c *config) {
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

// Node formats node in canonical cue fmt style and returns the result.
//
// The node type must be [*ast.File], [ast.Expr], or [ast.Decl].
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
	cfg := newConfig(opt)
	scfg := cfg.style()
	if !canMutate && scfg.WouldMutate(node) {
		node = ast.Clone(node) // Ensure immutability of the AST.
	}
	scfg.Apply(node)
	return cfg.pretty().Node(node)
}

// Source formats src in canonical cue fmt style and returns the result or an
// (I/O or syntax) error. src is expected to be a syntactically correct CUE
// source file, or a list of CUE declarations or statements.
//
// Caution: Tools relying on consistent formatting based on the installed
// version of cue (for instance, such as for presubmit checks) should execute
// that cue binary instead of calling Source.
func Source(b []byte, opt ...Option) ([]byte, error) {
	cfg := newConfig(opt)

	// Parse at the configured language version, so that syntax which only
	// some versions accept is parsed the way the caller means it.
	parseOpts := []parser.Option{parser.ParseComments, parser.Version(cfg.languageVersion)}
	f, err := parser.ParseFile("", b, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("parse: %s", err)
	}

	// The AST is our own, so there is no need to preserve it.
	cfg.style().Apply(f)
	return cfg.pretty().Node(f)
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
	indentWidth  int // 0 means unset; derived from the indentation string
	indentPrefix int // default: 0
	lineWidth    int // 0 means unset; the default is 120
}

// style builds the [ASTStyle] that is applied before printing.
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

func newConfig(opt []Option) *config {
	cfg := &config{}
	for _, o := range opt {
		o(cfg)
	}
	return cfg
}

func (cfg *config) pretty() *pretty.Config {
	pcfg := &pretty.Config{
		IndentWidth: cfg.indentWidth,
		Width:       cfg.lineWidth,
	}
	switch {
	case cfg.indent != nil:
		pcfg.Indent = *cfg.indent
	case cfg.spaceIndent:
		width := cfg.indentWidth
		if width == 0 {
			width = 4
		}
		pcfg.Indent = strings.Repeat(" ", width)
	default:
		pcfg.Indent = "\t"
	}
	pcfg.Prefix = strings.Repeat(pcfg.Indent, cfg.indentPrefix)
	return pcfg
}
