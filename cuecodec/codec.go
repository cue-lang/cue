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

// Package cuecodec defines the interfaces connecting file formats to the
// CUE loader, and provides the default lightweight formats (cue, json,
// yaml). Heavier formats live in subpackages (cuecodec/toml,
// cuecodec/protobuf, ...) and are added to a [Set] explicitly, so
// programs pay only for the codecs they import.
package cuecodec

import (
	"context"
	"io"
	"iter"

	"cuelang.org/go/cue/ast"
)

// A Codec identifies a file format.
type Codec interface {
	// Name returns the format's name, as used in file qualifiers:
	// "json", "yaml", "toml", ...
	Name() string

	// Extensions returns the file extensions (with leading dot) that
	// select this format by default.
	Extensions() []string
}

// A Decoder is a Codec that can decode files into CUE syntax.
type Decoder interface {
	Codec

	// NewDecoder decodes r, yielding one *ast.File per document:
	// exactly one for single-document formats, several for
	// multi-document formats such as YAML streams and JSON Lines.
	//
	// A nil opts is equivalent to a zero DecodeOptions. On the first
	// decoding error the sequence yields a single (nil, err) pair and
	// stops.
	NewDecoder(r io.Reader, opts *DecodeOptions) iter.Seq2[*ast.File, error]
}

// An Encoder is a Codec that can encode CUE syntax.
type Encoder interface {
	Codec

	// NewEncoder returns a stream encoder writing to w. Formats without
	// a stream form report an error on the second Write.
	NewEncoder(w io.Writer, opts *EncodeOptions) (EncodeStream, error)
}

// An EncodeStream writes a sequence of documents in an encoded form.
type EncodeStream interface {
	// Write encodes f and writes it to the underlying writer. A
	// single-document format reports an error if Write is called more
	// than once.
	//
	// TODO(v2): once cue/v2 exists, add a value-based entry point
	// (Write of a cue.Value) alongside this syntax-based one.
	Write(ctx context.Context, f *ast.File) error

	// Close finishes the stream, flushing any pending output.
	Close() error
}

// DecodeOptions configures decoding.
type DecodeOptions struct {
	// Filename is used for position information.
	Filename string

	// TODO(v2): a Schema field (a cue.Value) guiding schema-directed
	// formats such as textproto and protobuf JSON.

	// Options holds codec-specific options (see each codec's
	// constructors).
	Options []Option
}

// EncodeOptions configures encoding.
type EncodeOptions struct {
	// Concrete requires encoded values to be fully specified. Formats
	// that cannot express incomplete values (json, yaml) imply it.
	Concrete bool

	// TODO(v2): a Schema field (a cue.Value) guiding schema-directed
	// formats.

	// PkgName is the package name for generated CUE output.
	PkgName string

	// Options holds codec-specific options.
	Options []Option
}

// An Option is a codec-specific option, created by constructors in the
// codec's package (for example cuelang.org/go/cuecodec/toml.Indent).
//
// Options are opaque to cuecodec itself; each codec interprets the
// options it recognizes and ignores the rest. A codec that defines
// options declares an option type in its own package and embeds
// [OptionBase] in it so that the type satisfies Option across package
// boundaries.
type Option interface {
	codecOption()
}

// OptionBase is embedded in a codec-specific option type to make it
// satisfy [Option]. It carries no data of its own.
type OptionBase struct{}

func (OptionBase) codecOption() {}

// TODO(v2): reintroduce the Interpreter and SyntaxInterpreter
// interfaces (semantic transformations applied to decoded input, such
// as interpreting decoded JSON as JSON Schema) and the FileType
// Interpreter field once the cue/v2 value API exists; interpreters
// operate on decoded values.

// A FileType selects how a file is decoded or encoded.
type FileType struct {
	// Codec names the format; empty infers it from the file extension.
	Codec string

	// TODO(v2): an Interpreter field naming an interpretation to apply
	// after decoding.

	// Options holds codec-specific options.
	Options []Option
}
