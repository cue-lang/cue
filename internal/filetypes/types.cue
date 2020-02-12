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
// It is used by gen.go to compile the instance into Go data, which is then
// used by the rest of the package to determine settings.
//
// There

// A File corresponds to a Go build.File.
File :: {
	filename:        string
	encoding:        Encoding
	interpretation?: Interpretation
	form?:           Form
	tags?: {[string]: string}
}

// A FileInfo defines how a file is encoded and interpreted.
FileInfo :: {
	File

	// Settings
	data:       *true | false
	references: *true | false
	cycles:     *true | false

	definitions:  bool
	optional:     bool
	constraints:  bool
	keepDefaults: bool
	incomplete:   bool
	imports:      bool
	stream:       bool
	docs:         bool
	attributes:   true | *false
}

// modes sets defaults for different operational modes.
//
// These templates are intended to be unified in at the root of this
// configuration.
modes: _

// input defines modes for input, such as import, eval, vet or def.
// In input mode, settings flags are interpreted as what is allowed to occur
// in the input. The default settings, therefore, tend to be permissive.
modes: input: {
	FileInfo :: x, x = {
		docs: *true | false
	}
	encodings: cue: {
		*forms.schema | _
	}
}

modes: export: {
	FileInfo :: x, x = {
		docs: true | *false
	}
	encodings: cue: {
		*forms.data | _
	}
}

modes: def: {
	FileInfo :: x, x = {
		docs: *true | false
	}
	encodings: cue: {
		*forms.schema | _
	}
}

// Extension maps file extensions to default file properties.
extensions: {
	"":        _
	".cue":    tags.cue
	".json":   tags.json
	".jsonl":  tags.jsonl
	".ldjson": tags.jsonl
	".ndjson": tags.jsonl
	".yaml":   tags.yaml
	".yml":    tags.yaml
	".txt":    tags.text
	".go":     tags.go
	".proto":  tags.proto
	// TODO: jsonseq,
	// ".textproto": tags.textpb
	// ".pb":        tags.binpb
}

// A Encoding indicates a file format for representing a program.
Encoding :: !="" // | error("no encoding specified")

// An Interpretation determines how a certain program should be interpreted.
// For instance, data may be interpreted as describing a schema, which itself
// can be converted to a CUE schema.
Interpretation :: string

Form :: string

file: FileInfo & {

	filename: "foo.json"
	form:     "schema"
}

// tags maps command line tags to file properties.
tags: {
	schema: form: "schema"
	final: form:  "final"
	graph: form:  "graph"
	dag: form:    "dag"
	data: form:   "data"

	cue: encoding: "cue"

	json: encoding: "json"
	json: *{
		form: *"" | "data"
	} | {
		form: *"schema" | "final"

		interpretation: *"jsonschema" | _
	}

	jsonl: encoding: "jsonl"
	yaml: encoding:  "yaml"
	proto: encoding: "proto"
	// "textpb": encodings.textproto
	// "binpb":  encodings.binproto
	text: {
		encoding: "text"
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
		tags: lang: string
	}

	jsonschema: interpretation: "jsonschema"
	openapi: interpretation:    "openapi"
}

// forms defines schema for all forms. It does not include the form ID.
forms: [Name=string]: FileInfo

forms: "": _

forms: schema: {
	form:   *"schema" | "final" | "graph"
	stream: true | *false

	incomplete:   *true | false
	definitions:  *true | false
	optional:     *true | false
	constraints:  *true | false
	keepDefaults: *true | false
	imports:      *true | false
	optional:     *true | false
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

// encodings: "": error("no encoding specified")

encodings: cue: {
	stream: false
}

encodings: json: {
	forms.data
	stream: *false | true
	docs:   false
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

encodings: toml: {
	forms.data
	stream: false
}

encodings: proto: {
	forms.schema
	encoding: "proto"
}

// encodings: textproto: {
//  forms.DataEncoding
//  encoding: "textproto"
// }

// encodings: binproto: {
//  forms.DataEncoding
//  encoding: "binproto"
// }

encodings: code: {
	forms.schema
	stream: false
}

interpretations: [Name=string]: FileInfo

interpretations: "": _

interpretations: jsonschema: {
	forms.schema
	encoding: *"yaml" | _
}

interpretations: openapi: {
	forms.schema
	encoding: *"yaml" | _
}
