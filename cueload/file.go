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
	"io"

	"cuelang.org/go/cue/ast"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/cuecodec"
)

// A File names an input file and how to decode it. Content comes from
// exactly one of Data, Open, or — when both are nil — the loader's
// filesystem, opened by Name relative to the loader's base directory.
type File struct {
	// Name is the file's name, used for display, position information,
	// and (absent literal content) opening the file.
	Name string

	// Type describes the file's format. The zero value infers it from
	// Name's extension via the loader's codec set.
	Type cuecodec.FileType

	// Data holds literal file content.
	Data []byte

	// Open, if non-nil, provides the content. It may be called more than
	// once, since a Source describing the file can be loaded repeatedly;
	// one-shot inputs such as stdin should be read into Data first (the
	// cli package does this for "-").
	Open func() (io.ReadCloser, error)
}

// A Doc is one decoded document of a file, as produced by
// [Loader.Decode].
type Doc struct {
	// File is the file the document was decoded from.
	File File

	// Index is the position of the document within the file, starting
	// at 0. Single-document formats always yield index 0.
	Index int

	// Syntax is the decoded document.
	Syntax *ast.File

	// loader is the loader that decoded the document.
	loader *Loader
}

// Value builds the document's value in the runtime of the loader that
// decoded it.
func (d Doc) Value(ctx context.Context) (cue.Value, error) {
	if d.loader == nil {
		return cue.Value{}, fmt.Errorf("cueload: Doc was not produced by a Loader")
	}
	v, err := d.loader.Build(ctx, d.Syntax)
	if err != nil {
		return cue.Value{}, err
	}
	d.loader.recordOrigin(v, Origin{File: d.File, Index: d.Index})
	return v, nil
}

// Origin describes where a value yielded by [Loader.Load] came from.
// Exactly one of Package and File is meaningful.
//
// Combinators preserve origins: Unify's results carry the origin of its
// plural operand; At, Lookup, Eval, and Map pass origins through; AsList
// yields a synthetic value with no origin.
type Origin struct {
	// Package is the package the value was built from, if any.
	Package *Package

	// File is the file the value was decoded from, if any, and Index is
	// the document index within it.
	File  File
	Index int
}
