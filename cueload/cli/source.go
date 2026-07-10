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

package cli

// This file composes the cueload.Source denoted by a Command: the
// classification of files into schemas and values, schema selection
// (-d), placement (-l, --list, --with-context), merging (-m), per-doc
// validation (vet), and expression extraction (-e).

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/cuecodec"
	"cuelang.org/go/cueload"
)

// runState carries the per-run context that parts of the composed plan
// need at load time. Its loader is nil when the plan is composed via
// [Command.Source] rather than [Command.Run]; the parts that cannot
// work without one (dynamic label evaluation) report that when loaded.
type runState struct {
	l *cueload.Loader

	// mu guards the fields below it.
	mu       sync.Mutex
	emptyOK  bool
	empty    cue.Value
	emptyErr error
}

// emptyValue returns the top value in the runtime of the run's loader,
// used as the base for building placement structs and label scopes.
func (rs *runState) emptyValue(ctx context.Context) (cue.Value, error) {
	if rs.l == nil {
		return cue.Value{}, fmt.Errorf("operation requires a loader; use Command.Run")
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.emptyOK {
		f, err := parser.ParseFile("-", "_")
		if err != nil {
			rs.emptyErr = err
		} else {
			rs.empty, rs.emptyErr = rs.l.Build(ctx, f)
		}
		rs.emptyOK = true
	}
	return rs.empty, rs.emptyErr
}

// flagState holds the parsed forms of the command's flags.
type flagState struct {
	exprs       []ast.Expr
	schemaExprs []ast.Expr
	labels      []label
	merge       bool
	list        bool
	withContext bool
}

// placement reports whether any orphan-placement flag is in effect.
func (fl *flagState) placement() bool {
	return fl.list || len(fl.labels) > 0
}

// A label is one -l path element: either a static selector or a
// dynamic label expression to evaluate against each record.
type label struct {
	sel  cue.Selector // static; valid when expr is nil
	expr ast.Expr     // dynamic label expression
}

// compose builds the load plan for the command.
func (c *Command) compose(rs *runState) (cueload.Source, error) {
	fl, err := c.parseFlags()
	if err != nil {
		return cueload.Source{}, err
	}

	// Partition the file arguments: CUE files form an ad-hoc package;
	// data files split into schemas (explicitly marked) and values.
	var cueFiles []cueload.File
	var schemaFiles, valueFiles []cueload.File
	for _, fa := range c.files {
		f, err := c.inputFile(fa)
		if err != nil {
			return cueload.Source{}, err
		}
		switch {
		case fa.spec.codec == "cue":
			cueFiles = append(cueFiles, f)
		case fa.spec.form == "schema":
			schemaFiles = append(schemaFiles, f)
		default:
			valueFiles = append(valueFiles, f)
		}
	}
	// Data files marked as schemas act as plain values when no value
	// files accompany them (there is nothing for them to be a schema
	// of).
	if len(valueFiles) == 0 {
		valueFiles, schemaFiles = schemaFiles, nil
	}

	var pkgSrcs []cueload.Source
	for _, pat := range c.pkgPatterns {
		pkgSrcs = append(pkgSrcs, cueload.Pkg(pat))
	}
	if len(cueFiles) > 0 {
		pkgSrcs = append(pkgSrcs, cueload.PkgFiles(cueFiles...))
	}
	if len(pkgSrcs) == 0 && len(valueFiles) == 0 {
		// No arguments at all: load the package in the current
		// directory.
		pkgSrcs = append(pkgSrcs, cueload.Pkg("."))
	}

	if len(valueFiles) == 0 {
		return c.composePackages(rs, fl, pkgSrcs)
	}
	return c.composeWithData(rs, fl, pkgSrcs, schemaFiles, valueFiles)
}

// composePackages builds the plan when only packages (and CUE files)
// are named: a stream of package values, optionally placed, validated
// (vet), and projected through -e expressions.
func (c *Command) composePackages(rs *runState, fl *flagState, pkgSrcs []cueload.Source) (cueload.Source, error) {
	if len(fl.schemaExprs) > 0 {
		return cueload.Source{}, fmt.Errorf("the -d/--schema flag requires non-CUE input files")
	}
	stream := concatSrcs(pkgSrcs)
	if len(fl.labels) > 0 {
		// The value-placement analogue of cmd/cue: wrap each result in
		// the struct described by -l. As in cmd/cue, --list has no
		// effect without data files.
		stream = c.placeStream(rs, fl, stream)
	}
	if c.Mode == ModeVet {
		return cueload.Validate(stream, topSource(), cueload.Concrete(true)), nil
	}
	return applyExprs(stream, fl.exprs), nil
}

// composeWithData builds the plan when data (value) files are present:
// the single package (or the schema-marked data files) acts as the
// schema, and the value documents stream against it, merged or placed
// as requested.
func (c *Command) composeWithData(rs *runState, fl *flagState, pkgSrcs []cueload.Source, schemaFiles, valueFiles []cueload.File) (cueload.Source, error) {
	if len(pkgSrcs) > 1 {
		return cueload.Source{}, fmt.Errorf("too many packages defined (%d) in combination with files", len(pkgSrcs))
	}

	// The schema: the loaded package, if any, unified with the decoded
	// schema files.
	schemaParts := append([]cueload.Source{}, pkgSrcs...)
	for _, f := range schemaFiles {
		schemaParts = append(schemaParts, cueload.Decode(f))
	}
	var schema *cueload.Source
	if len(schemaParts) > 0 {
		s := unifySrcs(schemaParts)
		schema = &s
	}

	// Each -d expression selects a schema within the schema source;
	// data must validate against all of them, so they unify.
	if len(fl.schemaExprs) > 0 {
		if schema == nil {
			return cueload.Source{}, fmt.Errorf("the -d/--schema flag specified without a schema")
		}
		sels := make([]cueload.Source, len(fl.schemaExprs))
		for i, e := range fl.schemaExprs {
			sels[i] = cueload.Eval(*schema, e)
		}
		s := unifySrcs(sels)
		schema = &s
	}

	if c.Mode == ModeVet {
		if schema == nil {
			return cueload.Source{}, fmt.Errorf("data files specified without a schema")
		}
		values, err := c.placedValues(rs, fl, valueFiles)
		if err != nil {
			return cueload.Source{}, err
		}
		// Data files are validated one document at a time, always for
		// concreteness (cue vet semantics for data).
		return cueload.Validate(concatSrcs(values), *schema, cueload.Concrete(true)), nil
	}

	var stream cueload.Source
	switch {
	case len(fl.schemaExprs) > 0:
		// -d streams the documents, each unified with the selected
		// schema (placement conflicts with -d and was rejected above).
		var decodes []cueload.Source
		for _, f := range valueFiles {
			decodes = append(decodes, cueload.Decode(f))
		}
		stream = cueload.Unify(concatSrcs(decodes), *schema)

	case fl.merge || fl.placement():
		// Merge everything — the package, the schema files, and the
		// (placed) value documents — into a single value.
		elems := append([]cueload.Source{}, pkgSrcs...)
		for _, f := range schemaFiles {
			elems = append(elems, cueload.Decode(f))
		}
		placed, err := c.placedValues(rs, fl, valueFiles)
		if err != nil {
			return cueload.Source{}, err
		}
		elems = append(elems, placed...)
		stream = foldUnify(rs, elems)

	default:
		// No merging: stream the documents, each unified with the
		// schema when there is one.
		var decodes []cueload.Source
		for _, f := range valueFiles {
			decodes = append(decodes, cueload.Decode(f))
		}
		stream = concatSrcs(decodes)
		if schema != nil {
			stream = cueload.Unify(stream, *schema)
		}
	}
	return applyExprs(stream, fl.exprs), nil
}

// placedValues returns the sources denoting the (placed) documents of
// the value files: the documents wrapped per the -l labels, or
// collected into a list under them for --list.
func (c *Command) placedValues(rs *runState, fl *flagState, valueFiles []cueload.File) ([]cueload.Source, error) {
	decodes := make([]cueload.Source, len(valueFiles))
	for i, f := range valueFiles {
		decodes[i] = cueload.Decode(f)
	}
	static, path, err := staticPath(fl.labels)
	if err != nil {
		return nil, err
	}
	switch {
	case fl.list:
		// All documents of all files collect into one list. (cmd/cue
		// import builds one list per input file; for the value modes a
		// single list is the useful semantics.)
		if !static {
			return nil, fmt.Errorf("dynamic -l/--path label expressions cannot be combined with --list yet")
		}
		src := cueload.AsList(concatSrcs(decodes))
		if len(fl.labels) > 0 {
			src = cueload.At(src, path)
		}
		return []cueload.Source{src}, nil

	case len(fl.labels) == 0:
		return decodes, nil

	case static:
		return []cueload.Source{cueload.At(concatSrcs(decodes), path)}, nil

	default:
		// Dynamic labels are evaluated per document, with the
		// document's file identity in scope for --with-context.
		placed := make([]cueload.Source, len(valueFiles))
		for i, f := range valueFiles {
			placed[i] = cueload.Map(decodes[i], c.filePlacer(rs, fl, f))
		}
		return placed, nil
	}
}

// placeStream wraps each value of a package stream per the -l labels
// (the placeValue analogue of cmd/cue). Dynamic labels resolve against
// the value itself.
func (c *Command) placeStream(rs *runState, fl *flagState, stream cueload.Source) cueload.Source {
	static, path, err := staticPath(fl.labels)
	if err == nil && static {
		return cueload.At(stream, path)
	}
	return cueload.Map(stream, func(ctx context.Context, v cue.Value) (cue.Value, error) {
		empty, err := rs.emptyValue(ctx)
		if err != nil {
			return cue.Value{}, err
		}
		sels, err := resolveLabels(ctx, rs, v, fl.labels)
		if err != nil {
			return cue.Value{}, err
		}
		return empty.FillPath(cue.MakePath(sels...), v), nil
	})
}

// filePlacer returns the Map function that places each document of f
// under the -l labels, evaluating dynamic label expressions against
// each record. With --with-context the expressions see a
// {data, filename, index, recordCount} scope instead of the record
// itself.
//
// recordCount requires knowing the total number of documents in the
// file before the first is placed, so the file is decoded once more,
// in full, to count them: the streamed documents are effectively
// buffered. The closure tracks the document index as the stream
// advances; it resets when the stream is restarted (for example by a
// second -e expression), so a plan with --with-context must not be
// loaded concurrently.
func (c *Command) filePlacer(rs *runState, fl *flagState, f cueload.File) func(context.Context, cue.Value) (cue.Value, error) {
	index := 0
	recordCount := -1
	return func(ctx context.Context, v cue.Value) (cue.Value, error) {
		empty, err := rs.emptyValue(ctx)
		if err != nil {
			return cue.Value{}, err
		}
		scope := v
		if fl.withContext {
			if recordCount < 0 {
				n := 0
				for _, err := range rs.l.Decode(ctx, f) {
					if err != nil {
						return cue.Value{}, err
					}
					n++
				}
				recordCount = n
			}
			if index >= recordCount {
				// The stream restarted.
				index = 0
			}
			scope = empty.
				FillPath(cue.MakePath(cue.Str("data")), v).
				FillPath(cue.MakePath(cue.Str("filename")), f.Name).
				FillPath(cue.MakePath(cue.Str("index")), index).
				FillPath(cue.MakePath(cue.Str("recordCount")), recordCount)
		}
		sels, err := resolveLabels(ctx, rs, scope, fl.labels)
		if err != nil {
			return cue.Value{}, err
		}
		index++
		return empty.FillPath(cue.MakePath(sels...), v), nil
	}
}

// resolveLabels converts the -l labels to selectors, evaluating dynamic
// label expressions in the given scope.
func resolveLabels(ctx context.Context, rs *runState, scope cue.Value, labels []label) ([]cue.Selector, error) {
	sels := make([]cue.Selector, 0, len(labels))
	for _, lb := range labels {
		if lb.expr == nil {
			sels = append(sels, lb.sel)
			continue
		}
		if rs.l == nil {
			return nil, fmt.Errorf("dynamic -l/--path label expressions require Command.Run")
		}
		lv, err := rs.l.LoadValue(ctx, cueload.Eval(cueload.Value(scope), lb.expr))
		if err != nil {
			return nil, fmt.Errorf("error evaluating label %s: %v", exprString(lb.expr), err)
		}
		s, err := lv.AsString(ctx)
		if err != nil {
			return nil, fmt.Errorf("error evaluating label %s: %v", exprString(lb.expr), err)
		}
		sels = append(sels, cue.Str(s))
	}
	return sels, nil
}

// staticPath reports whether all labels are static and, if so, the path
// they denote.
func staticPath(labels []label) (bool, cue.Path, error) {
	sels := make([]cue.Selector, 0, len(labels))
	for _, lb := range labels {
		if lb.expr != nil {
			return false, cue.Path{}, nil
		}
		sels = append(sels, lb.sel)
	}
	return true, cue.MakePath(sels...), nil
}

// parseFlags parses and validates the command's flag fields.
func (c *Command) parseFlags() (*flagState, error) {
	fl := &flagState{
		list:        c.List,
		withContext: c.WithContext,
	}
	if c.Mode == ModeVet {
		if len(c.Expressions) > 0 {
			return nil, fmt.Errorf("the -e/--expression flag is not supported in vet mode")
		}
		if c.Out != "" {
			return nil, fmt.Errorf("the --out flag is not supported in vet mode")
		}
	}
	for _, e := range c.Expressions {
		x, err := parser.ParseExpr("--expression", e)
		if err != nil {
			return nil, err
		}
		fl.exprs = append(fl.exprs, x)
	}
	for _, s := range c.Schemas {
		x, err := parser.ParseExpr("--schema", s)
		if err != nil {
			return nil, err
		}
		fl.schemaExprs = append(fl.schemaExprs, x)
	}
	for _, s := range c.Path {
		labels, err := parsePathFlag(s)
		if err != nil {
			return nil, err
		}
		fl.labels = append(fl.labels, labels...)
	}
	if c.PerFile && c.Mode != ModeImport {
		return nil, fmt.Errorf("the --files flag is only supported in import mode")
	}
	if c.FileFilter != "" {
		return nil, fmt.Errorf("the -n/--name flag is not supported yet")
	}
	if !fl.placement() && !c.PerFile {
		if fl.withContext {
			return nil, fmt.Errorf("the --with-context flag must be used with at least one of the --path, --list, or --files flags")
		}
	} else if len(fl.schemaExprs) > 0 {
		return nil, fmt.Errorf("cannot combine the -d/--schema flag with the --path, --list, or --files flags")
	}
	switch {
	case c.Mode == ModeVet:
		// vet never merges: data files are validated per document.
		fl.merge = false
	case c.Merge != nil:
		fl.merge = *c.Merge
	default:
		fl.merge = true
	}
	return fl, nil
}

// parsePathFlag parses one -l argument: a label expression such as
// "foo" or "strings.ToLower(bar)", or a full path such as
// `foo: "\(bar)":`.
//
// As in cmd/cue, an argument that parses as an expression is a dynamic
// label evaluated against each record — including a bare identifier,
// which refers to a field of the record. Only the full path form
// ("foo:") and string literals denote labels statically.
func parsePathFlag(str string) ([]label, error) {
	x, err := parser.ParseExpr("--path", str)
	if err != nil {
		labels, err2 := parseFullPath(str)
		if err2 != nil {
			return nil, fmt.Errorf(`labels must be expressions (-l foo -l 'strings.ToLower(bar)') or full paths (-l '"foo": "\(strings.ToLower(bar))":') : %v`, err2)
		}
		return labels, nil
	}
	if lit, ok := x.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		// Evaluating a string literal is the identity; make it static
		// so that plans with only literal labels need no loader.
		return []label{{sel: cue.Str(mustUnquote(lit))}}, nil
	}
	return []label{{expr: x}}, nil
}

// fullPathLabel converts one label of a full -l path to a label:
// identifiers and string literals are static, anything else is
// dynamic.
func fullPathLabel(x ast.Expr) (label, error) {
	switch x := x.(type) {
	case *ast.Ident:
		if !ast.IsValidIdent(x.Name) {
			return label{}, fmt.Errorf("invalid label %q", x.Name)
		}
		if strings.HasPrefix(x.Name, "#") {
			return label{sel: cue.Def(x.Name)}, nil
		}
		return label{sel: cue.Str(x.Name)}, nil
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return label{}, fmt.Errorf("invalid label %s", x.Value)
		}
		s, err := literal.Unquote(x.Value)
		if err != nil {
			return label{}, fmt.Errorf("invalid label %s: %v", x.Value, err)
		}
		return label{sel: cue.Str(s)}, nil
	default:
		return label{expr: x}, nil
	}
}

// mustUnquote unquotes a string literal that the parser accepted;
// a literal with an interpolation never reaches here (it parses as
// *ast.Interpolation).
func mustUnquote(lit *ast.BasicLit) string {
	s, err := literal.Unquote(lit.Value)
	if err != nil {
		panic(err)
	}
	return s
}

// parseFullPath parses a -l argument in full path form, such as
// `foo: "\(bar)":`, into its sequence of labels. Ported from cmd/cue.
func parseFullPath(exprs string) ([]label, error) {
	f, err := parser.ParseFile("--path", exprs+"_")
	if err != nil {
		return nil, fmt.Errorf("parser error in path %q: %v", exprs, err)
	}
	if len(f.Decls) != 1 {
		return nil, fmt.Errorf("path flag must be a space-separated sequence of labels")
	}
	var out []label
	for d := f.Decls[0]; ; {
		field, ok := d.(*ast.Field)
		if !ok {
			// This should never happen.
			return nil, fmt.Errorf("%q is not a sequence of labels", exprs)
		}

		switch x := field.Label.(type) {
		case *ast.Ident, *ast.BasicLit:
			lb, err := fullPathLabel(x.(ast.Expr))
			if err != nil {
				return nil, err
			}
			out = append(out, lb)
		case ast.Expr:
			out = append(out, label{expr: x})
		default:
			return nil, fmt.Errorf("unsupported label type %T", x)
		}

		v, ok := field.Value.(*ast.StructLit)
		if !ok {
			break
		}
		if len(v.Elts) != 1 {
			return nil, fmt.Errorf("path value may not contain a struct")
		}
		d = v.Elts[0]
	}
	return out, nil
}

// inputFile converts a classified file argument to a cueload.File,
// reading standard input eagerly so the resulting Source can be loaded
// repeatedly.
func (c *Command) inputFile(fa fileArg) (cueload.File, error) {
	f := cueload.File{
		Name: fa.name,
		Type: cuecodec.FileType{Codec: fa.spec.codec},
	}
	if fa.name == "-" {
		data, err := c.readStdin()
		if err != nil {
			return cueload.File{}, err
		}
		f.Data = data
	}
	return f, nil
}

// applyExprs projects each value of src through the -e expressions.
// With several expressions the whole stream is replayed once per
// expression, so for a plural stream the grouping differs from cmd/cue
// (which yields all expressions of a value before moving on).
func applyExprs(src cueload.Source, exprs []ast.Expr) cueload.Source {
	switch len(exprs) {
	case 0:
		return src
	case 1:
		return cueload.Eval(src, exprs[0])
	}
	out := make([]cueload.Source, len(exprs))
	for i, e := range exprs {
		out[i] = cueload.Eval(src, e)
	}
	return cueload.Concat(out...)
}

// foldUnify merges every value of the given sources into a single
// value: the stream is collected into a list (making any per-value
// error structural, as merging cannot skip an element) and its elements
// unified. This is how -m/--merge combines a package with all the
// documents of its data files.
//
// TODO(cueload): a first-class fold combinator (say UnifyAll) would
// express this directly and keep the plan more legible.
func foldUnify(rs *runState, elems []cueload.Source) cueload.Source {
	all := concatSrcs(elems)
	return cueload.Map(cueload.AsList(all), func(ctx context.Context, v cue.Value) (cue.Value, error) {
		if err := v.Err(ctx); err != nil {
			return cue.Value{}, err
		}
		var out cue.Value
		n := 0
		for _, e := range v.Items(ctx) {
			if n == 0 {
				out = e
			} else {
				out = out.Unify(e)
			}
			n++
		}
		if n == 0 {
			// Nothing to merge (for example an empty JSON Lines file):
			// the merged configuration is empty.
			return rs.emptyValue(ctx)
		}
		return out, nil
	})
}

// concatSrcs concatenates sources, avoiding a needless wrapper for a
// single one.
func concatSrcs(srcs []cueload.Source) cueload.Source {
	if len(srcs) == 1 {
		return srcs[0]
	}
	return cueload.Concat(srcs...)
}

// unifySrcs unifies sources, avoiding a needless wrapper for a single
// one.
func unifySrcs(srcs []cueload.Source) cueload.Source {
	if len(srcs) == 1 {
		return srcs[0]
	}
	return cueload.Unify(srcs...)
}

// topSource returns a source denoting the top value (_), used as the
// trivial schema when vetting packages.
func topSource() cueload.Source {
	f, err := parser.ParseFile("-", "_")
	if err != nil {
		// Cannot happen: the source is a constant.
		panic(err)
	}
	return cueload.Syntax(f)
}

// exprString renders an expression for an error message.
func exprString(x ast.Expr) string {
	if data, err := format.Node(x); err == nil {
		return string(data)
	}
	return "<expression>"
}
