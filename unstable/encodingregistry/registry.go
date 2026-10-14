// Copyright 2026 CUE Authors
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

// Package encodingregistry lets a Go program register file encodings
// that CUE does not ship with, so that CUE commands and APIs recognize
// and process files of those encodings exactly like built-in ones.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
//
// A registration couples a declarative record — the encoding's name,
// file extensions, and template-conforming file-type properties — with
// decoder and encoder constructors implemented in Go. After a
// successful [Register] call, file arguments using one of the declared
// extensions or the explicit "name:" qualifier resolve to the declared
// properties, and decoding (and, when an encoder is provided, encoding)
// works end to end:
//
//	func init() {
//		err := encodingregistry.Register(encodingregistry.Encoding{
//			Name:       "capnp",
//			Extensions: []string{".capnp"},
//			Binary:     true, // Cap'n Proto is a binary wire format
//			Info:       encodingregistry.FileInfo{Form: "data"},
//			NewDecoder: newCapnpDecoder,
//		})
//		...
//	}
//	// cue export data.capnp   (or: capnp: somefile)
//
// The registry is process-global and add-only: a registration that
// collides with a built-in encoding or a prior registration on name or
// extension fails with a [*ConflictError]; built-ins are never
// overridden. The registry freezes at its first use for file-type
// resolution or codec dispatch; a Register call after that fails with
// a [*LateRegistrationError]. Register from an init function, or in
// any case before the first use of the CUE API, as in the example
// above.
//
// [Register] validates the declarative record at registration time against the
// same internal template (types.cue) that defines the built-in file types.
// Latency-sensitive programs with fixed declarations may instead use
// [RegisterWithoutFullValidation] and run [Validate] over those declarations in a
// build-time test.
//
// The declarative properties describe capabilities used by file-type
// resolution and the surrounding CUE pipeline; they are not per-call codec
// options. Codec constructors receive the resolved [build.File], including
// its form and subsidiary tags. Keeping declarations separate from Go
// constructors also leaves room for a future CUE declaration or code
// generation layer once its binding to Go codec implementations is defined.
package encodingregistry

import (
	"io"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// A Decoder decodes documents of a registered encoding into CUE
// syntax. Decode returns one expression per document, and io.EOF when
// no documents remain. Encodings that do not stream return one
// expression followed by io.EOF.
type Decoder = filetypes.Decoder

// An Encoder encodes concrete CUE syntax into a registered encoding.
// Encode is called once per document; Close flushes any buffered
// output but does not close the underlying writer.
type Encoder = filetypes.Encoder

// A ValueDecoder decodes documents of a registered encoding directly
// into cue.Value, preserving the full value lattice. Decode returns one
// value per document and io.EOF when no documents remain.
type ValueDecoder = encoding.ValueDecoder

// A ValueEncoder encodes cue.Value into a registered encoding,
// preserving the value lattice. Close flushes buffered output but does
// not close the underlying writer.
type ValueEncoder = encoding.ValueEncoder

// Mode identifies an overall mode CUE is being run in, for per-mode
// property overlays. The zero value is not a valid mode.
type Mode string

const (
	Input  Mode = "input"  // reading data or schemas, e.g. import, vet
	Export Mode = "export" // exporting data, e.g. export
	Def    Mode = "def"    // exporting schemas, e.g. def
	Eval   Mode = "eval"   // legacy evaluation mode, e.g. eval
)

// Aspect names a file-format capability. In [FileInfo.Aspects], an absent
// key inherits the value supplied by the selected form and mode; a present
// key explicitly refines that value to true or false.
type Aspect string

const (
	// AspectAttributes permits CUE attributes to be preserved or emitted.
	AspectAttributes Aspect = "attributes"
	// AspectConstraints permits non-concrete CUE constraints.
	AspectConstraints Aspect = "constraints"
	// AspectCycles permits cyclic references.
	AspectCycles Aspect = "cycles"
	// AspectData permits regular, non-definition fields.
	AspectData Aspect = "data"
	// AspectDefinitions permits definition fields.
	AspectDefinitions Aspect = "definitions"
	// AspectDocs permits documentation comments to be preserved or emitted.
	AspectDocs Aspect = "docs"
	// AspectIncomplete permits incomplete values.
	AspectIncomplete Aspect = "incomplete"
	// AspectImports permits imports to remain unexpanded.
	AspectImports Aspect = "imports"
	// AspectKeepDefaults permits default values to remain selected.
	AspectKeepDefaults Aspect = "keepDefaults"
	// AspectOptional permits optional fields.
	AspectOptional Aspect = "optional"
	// AspectReferences permits references to remain unresolved.
	AspectReferences Aspect = "references"
	// AspectStream permits multiple documents in one stream.
	AspectStream Aspect = "stream"
)

// FileInfo holds template-conforming file-type properties of a
// registration: how files of the encoding are interpreted and which
// language aspects they support.
type FileInfo struct {
	// Interpretation optionally names a built-in interpretation
	// applied to files of this encoding, such as "jsonschema".
	// Most encodings leave it empty.
	Interpretation string

	// Form names the default form files of this encoding take: one of
	// "data", "graph", "dag", "schema" or "final". The encoding
	// inherits the named form's aspect values and permitted refinements
	// exactly as built-in encodings do: schema may refine to final or
	// graph, graph may refine to dag or data, and dag may refine to data.
	// Data and final are concrete. If empty, only the mode defaults and
	// Aspects apply. In a per-mode FileInfo (see [Encoding.PerMode]), a
	// non-empty Form replaces the base form for that mode.
	Form string

	// Aspects holds capability overrides on top of the form and mode
	// values. Use the Aspect constants as keys. An absent key inherits;
	// a present key explicitly enables or disables that capability.
	// In a per-mode FileInfo (see [Encoding.PerMode]), a present key
	// replaces the base value for that key in that mode; absent keys
	// inherit the base Aspects.
	Aspects map[Aspect]bool
}

// A TagDecl declares a string-valued subsidiary tag, usable in file
// qualifiers as "name+tag=value:". A non-nil Default applies when the
// tag is not given explicitly. Defaults are constant: cascading
// defaults between tags are not supported for registered encodings.
type TagDecl struct {
	Default *string
}

// A BoolTagDecl declares a boolean subsidiary tag, usable in file
// qualifiers as "name+tag:" or "name+tag=false:". A non-nil Default
// applies when the tag is not given explicitly.
type BoolTagDecl struct {
	Default *bool
}

// Encoding describes one encoding registration: the declarative
// record plus the codec constructors.
type Encoding struct {
	// Name is the encoding's name and its explicit file qualifier,
	// e.g. "capnp" for "capnp: file.x". It must be non-empty.
	Name string

	// Extensions lists the file extensions claimed by the encoding,
	// including the leading dot, e.g. ".capnp". It may be empty, in
	// which case files are matched by explicit qualifier only.
	Extensions []string

	// Info holds the encoding's file-type properties, applied in
	// every mode. PerMode optionally overrides them for specific
	// modes: a field set in a mode's FileInfo replaces the base value
	// for that mode — Interpretation and Form wholesale, Aspects key
	// by key — and absent fields inherit from Info. Each mode's
	// composed result is validated independently, so a per-mode value
	// may select a form unrelated to the base (for example, a schema
	// encoding that exports as data), just as built-in encodings vary
	// form and aspects per mode.
	Info    FileInfo
	PerMode map[Mode]FileInfo

	// Binary declares that files of this encoding are binary: their
	// bytes are handed to NewDecoder verbatim, without the UTF-8/BOM
	// normalization applied to text encodings. Set it for wire formats
	// (protobuf, Cap'n Proto, msgpack, images, ...); leave it false for
	// UTF-8 text formats.
	Binary bool

	// Tags and BoolTags declare the subsidiary tags permitted for
	// this encoding.
	Tags     map[string]TagDecl
	BoolTags map[string]BoolTagDecl

	// NewDecoder returns a syntax-plane decoder reading documents of
	// this encoding from r. Exactly one decoder is required: either
	// NewDecoder or NewValueDecoder, not both.
	//
	// The build.File records the resolved file properties, including
	// any subsidiary tag values in f.Tags and f.BoolTags.
	NewDecoder func(f *build.File, r io.Reader) (Decoder, error)

	// NewEncoder returns a syntax-plane encoder writing documents of
	// this encoding to w. It is optional: when nil (and NewValueEncoder
	// is also nil), the encoding is decode-only and using it as an
	// output target fails with the same error an unencodable built-in
	// encoding produces. It is mutually exclusive with NewValueEncoder.
	NewEncoder func(f *build.File, w io.Writer) (Encoder, error)

	// NewValueDecoder returns a value-plane decoder that reads documents
	// directly into cue.Value. Syntax can express constraints, defaults,
	// definitions, and conjunctions, but rebuilding a value from syntax cannot
	// preserve evaluated graph identity, structure sharing, or original
	// conjunct provenance. The direct path preserves those properties. It is
	// mutually exclusive with NewDecoder; exactly one decoder must be set. The
	// decoder receives the *cue.Context to build values into.
	NewValueDecoder func(ctx *cue.Context, f *build.File, r io.Reader) (ValueDecoder, error)

	// NewValueEncoder returns a value-plane encoder that receives the
	// raw cue.Value, preserving the full lattice. Concreteness is
	// enforced only in modes that require it (the resolved form's
	// Incomplete aspect), mirroring the built-in CUE and wire encoders.
	// It is optional and mutually exclusive with NewEncoder.
	NewValueEncoder func(f *build.File, w io.Writer) (ValueEncoder, error)
}

// A ConflictError reports a registration refused because its name or
// one of its extensions is already taken — by a built-in encoding, a
// built-in tag, a prior registration, or a prior registration's
// subsidiary tag. Registration is add-only; conflicts are never
// resolved by overriding. The Owner field names the encoding that owns
// the colliding name or extension; for a tag collision (Tag == true)
// it names the encoding that declared the tag, and is empty for
// built-in tags, which no single encoding owns.
type ConflictError = filetypes.ConflictError

// A LateRegistrationError reports a registration that arrived after
// the registry was already used for file-type resolution or codec
// dispatch. Register earlier, typically from an init function.
type LateRegistrationError = filetypes.LateRegistrationError

// Register adds the encoding to the process-wide registry.
//
// It returns a [*ConflictError] if the encoding's name or one of its
// extensions is already registered, a [*LateRegistrationError] if
// file types have already been used in this process, and a validation
// error if the declarative record does not conform to the file-type
// template.
func Register(e Encoding) error {
	return register(e)
}

// RegisterWithoutFullValidation adds the encoding to the process-wide registry
// without unifying its declarative record against the types.cue
// file-type template. That template step guards against drift between
// this package's evaluator-free checks and the types.cue #FileInfo
// template; skipping it avoids compiling the template — the cost that
// dominates a cold [Register] call — and avoids keeping the compiled
// template's CUE context resident for the process lifetime. Register
// and RegisterWithoutFullValidation otherwise accept and reject declarations
// identically: the evaluator-free structural and composition checks
// (name, decoder, extensions, tags, and compatible
// interpretation/form/aspects) and the add-only, freeze-on-first-use,
// conflict, and codec machinery are shared. A first call may still
// perform the shared, one-time pure-Go materialization of generated
// built-in file-type data.
//
// RegisterWithoutFullValidation is intended for latency-sensitive binaries (for
// example a commercial build registering an encoding from an init
// function) whose registrations are fixed; call [Validate] on the same
// Encoding from a build-time test so that a malformed or drifted
// declaration fails the build rather than shipping.
//
// It returns the same [*ConflictError] and [*LateRegistrationError] as
// Register, plus any structural error, but performs no template
// validation.
func RegisterWithoutFullValidation(e Encoding) error {
	return registerWithoutFullValidation(e)
}

// Validate reports whether e's declarative record is well formed and
// free of conflicts with the built-in file types, without registering
// anything. It runs the structural and pure-Go composition checks
// shared by [Register] and [RegisterWithoutFullValidation], reports a collision
// with a built-in name or extension as a [*ConflictError], and unifies
// the base properties and every per-mode overlay against the types.cue
// template — the drift protection that RegisterWithoutFullValidation skips.
// Validate is side-effect-free and independent of registry state:
// conflicts with other runtime registrations are observable only at
// registration time. A consumer that registers with
// [RegisterWithoutFullValidation] should call Validate over the same
// declarations in a build-time test so a malformed or conflicting
// declaration fails the build rather than shipping.
func Validate(e Encoding) error {
	return validateDeclaration(e)
}
