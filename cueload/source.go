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

package cueload

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	cue "cuelang.org/go/cue/v2"
)

// A Source is an immutable description of how to produce a finite stream
// of CUE values. Sources perform no work themselves: a [Loader]
// interprets them. Most sources denote exactly one value; [Pkg] with a
// wildcard pattern and [Decode] of a multi-document file denote several.
//
// The String method renders the source as an inspectable plan.
type Source struct {
	op *sourceOp
}

// srcKind enumerates the description nodes a Source can be built from.
type srcKind uint8

const (
	srcInvalid srcKind = iota
	srcPkg
	srcPkgFiles
	srcDecode
	srcValue
	srcSyntax
	srcGo
	srcUnify
	srcConcat
	srcAsList
	srcAt
	srcLookup
	srcEval
	srcValidate
	srcMap
)

// A sourceOp is one node in the immutable description of a Source.
type sourceOp struct {
	kind    srcKind
	pattern string      // srcPkg
	files   []File      // srcPkgFiles, srcDecode (one file)
	value   cue.Value   // srcValue
	syntax  *ast.File   // srcSyntax
	goValue any         // srcGo
	srcs    []*sourceOp // srcUnify, srcConcat
	src     *sourceOp   // srcAsList, srcAt, srcLookup, srcEval, srcValidate (data), srcMap
	schema  *sourceOp   // srcValidate
	path    cue.Path    // srcAt, srcLookup
	expr    ast.Expr    // srcEval
	vopts   validateOptions
	mapFunc func(context.Context, cue.Value) (cue.Value, error)
}

// Pkg denotes the packages matched by pattern: an import path, a
// relative directory, or a "./..." wildcard. One value per package.
func Pkg(pattern string) Source {
	return Source{op: &sourceOp{kind: srcPkg, pattern: pattern}}
}

// PkgFiles denotes the ad-hoc package assembled from the given CUE files
// (the command-line-arguments case). Imports resolve against the
// enclosing module as usual. One value.
func PkgFiles(files ...File) Source {
	return Source{op: &sourceOp{kind: srcPkgFiles, files: files}}
}

// Decode denotes the documents of a data file: one value per document.
// It is the algebra view of [Loader.Decode].
func Decode(f File) Source {
	return Source{op: &sourceOp{kind: srcDecode, files: []File{f}}}
}

// Value lifts an existing value. One value.
func Value(v cue.Value) Source {
	return Source{op: &sourceOp{kind: srcValue, value: v}}
}

// Syntax lifts parsed CUE syntax. One value.
func Syntax(f *ast.File) Source {
	return Source{op: &sourceOp{kind: srcSyntax, syntax: f}}
}

// Go lifts a Go value, converted as by cue.Value.FillPath. One value.
func Go(x any) Source {
	return Source{op: &sourceOp{kind: srcGo, goValue: x}}
}

// Unify combines sources by unification. At most one operand may denote
// more than one value; the others are broadcast across it, and the
// result has its cardinality. This is how a schema constrains every
// document of a stream, and how data files merge into a package.
func Unify(srcs ...Source) Source {
	return Source{op: &sourceOp{kind: srcUnify, srcs: sourceOps(srcs)}}
}

// Concat concatenates the streams denoted by srcs, in order.
func Concat(srcs ...Source) Source {
	return Source{op: &sourceOp{kind: srcConcat, srcs: sourceOps(srcs)}}
}

// AsList collects all values of src into a single CUE list. One value.
func AsList(src Source) Source {
	return Source{op: &sourceOp{kind: srcAsList, src: src.op}}
}

// At places each value of src under path, so that a value v becomes
// {<path>: v}.
func At(src Source, path cue.Path) Source {
	return Source{op: &sourceOp{kind: srcAt, src: src.op, path: path}}
}

// Lookup selects path within each value of src.
func Lookup(src Source, path cue.Path) Source {
	return Source{op: &sourceOp{kind: srcLookup, src: src.op, path: path}}
}

// Eval evaluates expr in the scope of each value of src, yielding the
// expression's value (the -e semantics of cmd/cue).
//
// Unlike most combinators, Eval forces each input value when loaded,
// because the expression is resolved within the evaluated result.
func Eval(src Source, expr ast.Expr) Source {
	return Source{op: &sourceOp{kind: srcEval, src: src.op, expr: expr}}
}

// Validate checks each value of data against schema, which must denote a
// single value: the data value is unified with the schema and validated
// according to opts (by default, for the absence of errors; optionally
// for concreteness). It yields the checked data values, so validation
// composes with further combinators. Validation errors flow through the
// stream per value without ending it.
func Validate(data, schema Source, opts ...ValidateOption) Source {
	op := &sourceOp{kind: srcValidate, src: data.op, schema: schema.op}
	for _, o := range opts {
		o(&op.vopts)
	}
	return Source{op: op}
}

// ValidateOption configures Validate.
type ValidateOption func(*validateOptions)

type validateOptions struct {
	concrete bool
}

// Concrete requires validated values to be concrete (cue vet -c).
func Concrete(c bool) ValidateOption {
	return func(o *validateOptions) {
		o.concrete = c
	}
}

// Map transforms each value of src with a Go function — the escape hatch
// for logic the algebra does not cover, such as computing placement
// labels from each record.
func Map(src Source, f func(context.Context, cue.Value) (cue.Value, error)) Source {
	return Source{op: &sourceOp{kind: srcMap, src: src.op, mapFunc: f}}
}

func sourceOps(srcs []Source) []*sourceOp {
	ops := make([]*sourceOp, len(srcs))
	for i, s := range srcs {
		ops[i] = s.op
	}
	return ops
}

// String renders the source description for debugging ("the load plan").
func (s Source) String() string {
	if s.op == nil {
		return "<invalid>"
	}
	return s.op.String()
}

func (op *sourceOp) String() string {
	if op == nil {
		return "<invalid>"
	}
	switch op.kind {
	case srcPkg:
		return fmt.Sprintf("pkg(%q)", op.pattern)
	case srcPkgFiles:
		names := make([]string, len(op.files))
		for i, f := range op.files {
			names[i] = strconv.Quote(f.Name)
		}
		return fmt.Sprintf("pkgFiles(%s)", strings.Join(names, ", "))
	case srcDecode:
		return fmt.Sprintf("decode(%q)", op.files[0].Name)
	case srcValue:
		return fmt.Sprintf("value(%s)", op.value)
	case srcSyntax:
		name := ""
		if op.syntax != nil {
			name = op.syntax.Filename
		}
		return fmt.Sprintf("syntax(%q)", name)
	case srcGo:
		return fmt.Sprintf("go(%T)", op.goValue)
	case srcUnify:
		return fmt.Sprintf("unify(%s)", opList(op.srcs))
	case srcConcat:
		return fmt.Sprintf("concat(%s)", opList(op.srcs))
	case srcAsList:
		return fmt.Sprintf("list(%s)", op.src)
	case srcAt:
		return fmt.Sprintf("at(%s, %s)", op.src, op.path)
	case srcLookup:
		return fmt.Sprintf("lookup(%s, %s)", op.src, op.path)
	case srcEval:
		expr := "<expr>"
		if data, err := format.Node(op.expr); err == nil {
			expr = string(data)
		}
		return fmt.Sprintf("eval(%s, %s)", op.src, expr)
	case srcValidate:
		s := fmt.Sprintf("validate(%s, %s", op.src, op.schema)
		if op.vopts.concrete {
			s += ", concrete"
		}
		return s + ")"
	case srcMap:
		return fmt.Sprintf("map(%s)", op.src)
	}
	return "<invalid>"
}

func opList(ops []*sourceOp) string {
	parts := make([]string, len(ops))
	for i, op := range ops {
		parts[i] = op.String()
	}
	return strings.Join(parts, ", ")
}
