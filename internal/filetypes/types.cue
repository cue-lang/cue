// Copyright 2020 CUE Authors
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

package build

// This file describes how various cross-cutting modes influence default
// settings.
//
// It is used by types.go to compile a cue.Value, which is then
// used by the rest of the package to determine settings.

// A File corresponds to a Go build.File.
#File: {
	filename?:       string // only filled in FromFile, but not in ParseFile
	encoding!:       #Encoding
	interpretation?: #Interpretation
	form?:           #Form
	// Note: tags includes values for non-boolean tags only.
	tags?: [string]:     string
	boolTags?: [string]: bool
}

// A FileInfo defines how a file is encoded and interpreted.
#FileInfo: {
	#File

	// For each of these fields it is explained what a true value means
	// for encoding/decoding.

	data:          *true | false // include/allow regular fields
	references:    *true | false // don't resolve/allow references
	cycles:        *true | false // cycles are permitted
	definitions?:  bool          // include/allow definition fields
	optional?:     bool          // include/allow definition fields
	constraints?:  bool          // include/allow constraints
	keepDefaults?: bool          // select/allow default values
	incomplete?:   bool          // permit incomplete values
	imports?:      bool          // don't expand/allow imports
	stream?:       bool          // permit streaming
	docs?:         bool          // show/allow docs
	attributes?:   bool          // include/allow attributes
}

// fileForExtVanilla holds the extensions supported in
// input mode with scope="" - the most common form
// of file type to evaluate.
//
// It's also used as a source of truth for all known file
// extensions as all modes define attributes for
// all file extensions. If that ever changed, we'd need
// to change this.
fileForExtVanilla: modes.input.extensions

// #Mode represents an overall mode that CUE is being run in
// (e.g. eval, export).
#Mode: {
	// FileInfo holds the base file information for this mode.
	// This will be unified with information derived from the
	// file extension and any filetype tags explicitly provided.
	FileInfo!: #FileInfo

	// extensions holds the set of file extensions that are defined
	// for this mode.
	extensions!: [_]: #FileInfo

	// encodings holds the set of encodings defined for this mode.
	encodings!: [_]: #FileInfo
}

// modes sets defaults for different operational modes.
// The key corresponds to the Go internal/filetypes.Mode type.
modes: [string]: #Mode

// input defines modes for input, such as import, eval, vet or def.
// In input mode, settings flags are interpreted as what is allowed to occur
// in the input. The default settings, therefore, tend to be permissive.
modes: input: {
	// FileInfo holds a value that's unified with the file value.
	FileInfo: {
		docs:       *true | false
		attributes: *true | false
	}
	// encodings is unified to the file value when interpretation is empty.
	encodings: cue: {
		*forms.schema | _
	}
	extensions: "-": encoding:           *"cue" | _
	extensions: ".json": interpretation: *"auto" | _
	extensions: ".yaml": interpretation: *"auto" | _
	extensions: ".yml": interpretation:  *"auto" | _
	extensions: ".toml": interpretation: *"auto" | _
	extensions: ".xml": interpretation:  *"auto" | _
}

modes: export: {
	FileInfo: {
		docs:       true | *false
		attributes: true | *false
	}
	encodings: cue: forms.data
	extensions: "-": encoding: *"json" | _
}

// eval is a legacy mode
modes: eval: {
	FileInfo: {
		docs:       true | *false
		attributes: true | *false
	}
	encodings: cue: forms.final
	extensions: "-": encoding: *"cue" | _
}

modes: def: {
	FileInfo: {
		docs:       *true | false
		attributes: *true | false
	}
	encodings: cue: forms.schema
	extensions: "-": encoding: *"cue" | _
}

// A Encoding indicates a file format for representing a program.
#Encoding: !="" // | error("no encoding specified")

// An Interpretation determines how a certain program should be interpreted.
// For instance, data may be interpreted as describing a schema, which itself
// can be converted to a CUE schema.
// This corresponds to the Go cue/build.Interpretation type.
#Interpretation: string

// A Form specifies the form in which a program should be represented.
// It corresponds to the Go cue/build.Form type.
#Form: string

all: {
	for m in modes {
		for name, _ in m.encodings {
			encodings: (name): true
		}
		for name, _ in m.extensions {
			extensions: (name): true
		}
	}
	for name, _ in interpretations {
		interpretations: (name): true
	}
	for name, _ in forms {
		forms: (name): true
	}
}

modes: [string]: {
	// extensions maps a file extension to its associated default file properties.
	extensions: {
		".cue":       tagInfo.cue
		".json":      tagInfo.json
		".jsonl":     tagInfo.jsonl
		".ldjson":    tagInfo.jsonl
		".ndjson":    tagInfo.jsonl
		".yaml":      tagInfo.yaml
		".yml":       tagInfo.yaml
		".toml":      tagInfo.toml
		".xml":       tagInfo.xml
		".txt":       tagInfo.text
		".go":        tagInfo.go
		".wasm":      tagInfo.binary
		".proto":     tagInfo.proto
		".textproto": tagInfo.textproto
		".textpb":    tagInfo.textproto // perhaps also pbtxt

		// TODO: jsonseq,
		// ".pb":        tagInfo.binpb // binarypb
	}

	// encodings: "": error("no encoding specified")

	encodings: cue: {
		stream: false
	}

	encodings: json: {
		forms.data
		stream:     *false | true
		docs:       false
		attributes: false
	}

	encodings: yaml: {
		forms.graph
		stream: false | *true
	}

	encodings: jsonl: {
		forms.data
		stream: true
	}

	encodings: text: {
		forms.data
		stream: false
	}

	encodings: binary: {
		forms.data
		stream: false
	}

	encodings: toml: {
		forms.data
		stream: false
	}

	encodings: xml: {
		forms.data
		stream: false
	}

	encodings: proto: {
		forms.schema
		encoding: "proto"
	}

	encodings: textproto: {
		forms.data
		encoding: "textproto"
		stream:   false
	}

	encodings: binarypb: {
		forms.data
		encoding: "binarypb"
		stream:   false
	}

	encodings: code: {
		forms.schema
		stream: false
	}
}

// forms defines schema for all forms. It does not include the form ID.
forms: [Name=string]: #FileInfo

forms: schema: {
	form: *"schema" | "final" | "graph"

	stream:       true | *false
	incomplete:   *true | false
	definitions:  *true | false
	optional:     *true | false
	constraints:  *true | false
	keepDefaults: *true | false
	imports:      *true | false
}

forms: final: {
	form: "final"
	forms.schema

	keepDefaults: false
	optional:     false
}

forms: graph: {
	form: *"graph" | "dag" | "data"
	data: true

	incomplete:   false
	definitions:  false
	optional:     false
	constraints:  false
	keepDefaults: false
	imports:      false
}

forms: dag: {
	form: !="graph"
	forms.graph

	cycles: false
}

forms: data: {
	form: !="dag"
	forms.dag

	constraints: false
	references:  false
	cycles:      false
	imports:     false
	optional:    false
}

interpretations: [Name=string]: #FileInfo & {
	interpretation: Name
}

interpretations: auto: forms.schema

interpretations: jsonschema: {
	forms.schema
	encoding: *"json" | _
	boolTags: {
		strict:         *false | bool
		strictKeywords: *strict | bool
		// TODO: enable strictFeatures by default? (see https://cuelang.org/issue/3923).
		strictFeatures: *strict | bool
	}
}

interpretations: openapi: {
	forms.schema
	encoding: *"json" | _
	boolTags: {
		strict:         *false | bool
		strictKeywords: *strict | bool
		strictFeatures: *strict | bool
	}
}

interpretations: pb: {
	forms.data
	stream: true
}

tagInfo: [_]: #FileInfo

// tagInfo maps command line tags to file properties.
tagInfo: {
	schema: form: "schema"
	graph: form:  "graph"
	dag: form:    "dag"
	data: form:   "data"

	cue: encoding:   "cue"
	json: encoding:  "json"
	jsonl: encoding: "jsonl"
	yaml: encoding:  "yaml"
	toml: encoding:  "toml"
	xml: {
		encoding: "xml"
		boolTags: {
			// We implement XML variants as boolean tags, such that "koala" is not accessible
			// as a top-level filetype, and can only be used via "xml+koala".
			// These effectively behave like a "one of", but we enforce this via the Go code
			// so that we can provide the users with good error messages.
			koala: *false | bool
		}
	}
	proto: encoding:     "proto"
	textproto: encoding: "textproto"
	// "binpb":  encodings.binproto

	// pb is used either to indicate binary encoding, or to indicate
	pb: *{
		encoding:       "binarypb"
		interpretation: ""
	} | {
		encoding:       !="binarypb"
		interpretation: "pb"
	}

	text: {
		encoding: "text"
		form:     "data"
	}
	binary: {
		encoding: "binary"
		form:     "data"
	}
	go: {
		encoding:       "code"
		interpretation: ""
		tags: lang: "go"
	}
	code: {
		encoding:       "code"
		interpretation: ""
		tags: lang: *"" | string
	}
	auto: interpretations.auto & {
		encoding: *"json" | string
	}
	jsonschema: interpretations.jsonschema
	openapi:    interpretations.openapi
}
