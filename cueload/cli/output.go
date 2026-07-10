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

// This file implements the output side of a command: parsing the
// --out/-o specification, rendering values through a cuecodec encoder,
// and writing output files with cmd/cue's do-not-overwrite semantics.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"cuelang.org/go/cue/ast"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/cuecodec"
)

// An OutputFile is an encoded output ready to be written.
type OutputFile struct {
	// Name is the target file name, or "-" for standard output.
	Name string

	// Data is the encoded content: one encoded document, including any
	// separator the format needs before a non-initial document, so that
	// the outputs of one run concatenate into a well-formed stream.
	Data []byte

	// target links the output files of one run that share a named
	// target, so that the first write creates the file and later
	// writes append to it.
	target *outputTarget
}

// An outputTarget tracks the write state of one named output file
// across the results of a run.
type outputTarget struct {
	// mu guards the field below it.
	mu      sync.Mutex
	created bool
}

// Write writes the output file, refusing to overwrite an existing file
// unless force is set, matching cmd/cue semantics. Within one run,
// results sharing a named target append after the first write. A Name
// of "-" writes to standard output.
func (f *OutputFile) Write(force bool) error {
	if f.Name == "" || f.Name == "-" {
		_, err := os.Stdout.Write(f.Data)
		return err
	}
	t := f.target
	if t == nil {
		t = &outputTarget{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	mode := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	switch {
	case t.created:
		mode = os.O_WRONLY | os.O_APPEND
	case force:
		// Swap O_EXCL for O_TRUNC to allow replacing an entire
		// existing file.
		mode = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	g, err := os.OpenFile(f.Name, mode, 0o666)
	if errors.Is(err, fs.ErrExist) {
		// If the file already exists but is not regular (a device, a
		// pipe), writing to it is not an overwrite; allow it.
		if st, serr := os.Stat(f.Name); serr == nil && !st.Mode().IsRegular() {
			g, err = os.OpenFile(f.Name, os.O_WRONLY, 0o666)
		} else {
			return fmt.Errorf("error writing %q: %w", f.Name, fs.ErrExist)
		}
	}
	if err != nil {
		return err
	}
	t.created = true
	_, werr := g.Write(f.Data)
	if cerr := g.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// An outputSpec is the parsed form of the --out/-o flag together with
// the mode's output defaults.
type outputSpec struct {
	name      string
	codecName string
	encoder   cuecodec.Encoder

	// form selects how values render: "data" (concrete, defaults
	// taken), "final" (defaults taken, incomplete allowed), or
	// "schema" (definitions, optional fields and docs preserved).
	form string
}

// outputSpec parses the command's output specification, applying the
// mode defaults: export writes JSON to stdout, eval and def write CUE.
func (c *Command) outputSpec() (*outputSpec, error) {
	var spec fileSpec
	name := "-"
	if c.Out != "" {
		scope, file, found := cutScope(c.Out)
		switch {
		case found && scope == "":
			return nil, fmt.Errorf("empty filetype prefix in %q", c.Out)
		case found:
			var err error
			spec, err = parseQualifier(scope)
			if err != nil {
				return nil, err
			}
			if file != "" {
				name = file
			}
		default:
			// No qualifier separator: accept a bare filetype ("yaml")
			// or a file name ("out.yaml").
			if sp, err := parseQualifier(c.Out); err == nil {
				spec = sp
			} else {
				name = c.Out
			}
		}
	}
	if spec.codec == "" {
		if name == "-" {
			if c.Mode == ModeExport {
				spec.codec = "json"
			} else {
				spec.codec = "cue"
			}
		} else {
			codec, ok := defaultCodecs.ByExtension(fileExt(name))
			if !ok {
				return nil, fmt.Errorf("unknown file extension for output file %q", name)
			}
			spec.codec = codec.Name()
		}
	}

	form := spec.form
	if form == "" {
		switch c.Mode {
		case ModeExport:
			form = "data"
		case ModeEval:
			form = "final"
		default:
			form = "schema"
		}
	}
	switch form {
	case "graph", "dag":
		// Approximated: the graph and dag forms render like data.
		form = "data"
	}
	if spec.codec != "cue" {
		// Data formats cannot express incomplete values.
		form = "data"
	}

	elem, ok := defaultCodecs.Lookup(spec.codec)
	if !ok {
		return nil, fmt.Errorf("unknown codec %q", spec.codec)
	}
	enc, ok := elem.(cuecodec.Encoder)
	if !ok {
		return nil, fmt.Errorf("file type %q does not support encoding", spec.codec)
	}
	return &outputSpec{
		name:      name,
		codecName: spec.codec,
		encoder:   enc,
		form:      form,
	}, nil
}

// concrete reports whether the output form requires concrete values.
func (s *outputSpec) concrete() bool {
	return s.form == "data"
}

// syntaxOptions returns the rendering options for the output form.
func (s *outputSpec) syntaxOptions() []cue.Option {
	switch s.form {
	case "data":
		return []cue.Option{cue.Concrete(true), cue.Final()}
	case "final":
		return []cue.Option{cue.Final()}
	default: // schema
		return []cue.Option{cue.Docs(true)}
	}
}

// An outputEncoder renders the values of one run through a single
// encode stream, so that multi-document formats separate documents
// correctly, and slices the encoded bytes per value.
type outputEncoder struct {
	spec    *outputSpec
	pkgName string
	buf     bytes.Buffer
	stream  cuecodec.EncodeStream
	target  *outputTarget
}

func newOutputEncoder(spec *outputSpec, pkgName string) (*outputEncoder, error) {
	e := &outputEncoder{
		spec:    spec,
		pkgName: pkgName,
		target:  &outputTarget{},
	}
	stream, err := spec.encoder.NewEncoder(&e.buf, &cuecodec.EncodeOptions{
		Concrete: spec.concrete(),
		PkgName:  pkgName,
	})
	if err != nil {
		return nil, err
	}
	e.stream = stream
	return e, nil
}

// encode renders one value as an output file.
func (e *outputEncoder) encode(ctx context.Context, v cue.Value) (*OutputFile, error) {
	if e.spec.concrete() {
		if err := v.Validate(ctx, cue.Concrete(true)); err != nil {
			return nil, err
		}
	}
	node := v.Syntax(ctx, e.spec.syntaxOptions()...)
	if node == nil {
		if err := v.Err(ctx); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("cannot render value")
	}
	f := toFile(node)
	if e.spec.codecName == "cue" && e.pkgName != "" {
		// The cuecodec CUE encoder does not (yet) honor
		// EncodeOptions.PkgName, so apply -p here.
		setPackage(f, e.pkgName)
	}
	start := e.buf.Len()
	if err := e.stream.Write(ctx, f); err != nil {
		return nil, err
	}
	return &OutputFile{
		Name:   e.spec.name,
		Data:   bytes.Clone(e.buf.Bytes()[start:]),
		target: e.target,
	}, nil
}

func (e *outputEncoder) close() error {
	return e.stream.Close()
}

// toFile wraps a rendered syntax node as an *ast.File for encoding: a
// struct literal becomes the file's top-level declarations; any other
// expression is embedded.
func toFile(node ast.Node) *ast.File {
	switch n := node.(type) {
	case *ast.File:
		return n
	case *ast.StructLit:
		f := &ast.File{Decls: n.Elts}
		ast.SetComments(f, ast.Comments(n))
		return f
	case ast.Expr:
		return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: n}}}
	}
	return &ast.File{}
}

// setPackage ensures the file declares the given package name, unless
// it already declares one.
func setPackage(f *ast.File, name string) {
	k := 0
	for _, d := range f.Decls {
		switch d.(type) {
		case *ast.CommentGroup, *ast.Attribute:
			k++
			continue
		case *ast.Package:
			return
		}
		break
	}
	decls := make([]ast.Decl, 0, len(f.Decls)+1)
	decls = append(decls, f.Decls[:k]...)
	decls = append(decls, &ast.Package{Name: ast.NewIdent(name)})
	decls = append(decls, f.Decls[k:]...)
	f.Decls = decls
}
