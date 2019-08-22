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

// Package encoding provides support for managing data format files supported
// by CUE.
package encoding // import "cuelang.org/go/cue/encoding"

import "strings"

// Encoding represents a data encoding.
type Encoding struct {
	name string
}

// Name returns a lowercase name of an encoding. This is conventionally the most
// common file extension in lower case.
func (e *Encoding) Name() string {
	return e.name
}

// All returns all known encodings.
func All() []*Encoding {
	return []*Encoding{jsonEnc, yamlEnc, protodefEnc}
}

// MapExtension returns the likely encoding for a given file extension.
func MapExtension(ext string) *Encoding {
	return extensions[strings.ToLower(ext)]
}

var (
	cueEnc      = &Encoding{name: "cue"}
	jsonEnc     = &Encoding{name: "json"}
	yamlEnc     = &Encoding{name: "yaml"}
	protodefEnc = &Encoding{name: "protobuf"}
)

// extensions maps a file extension to a Kind.
var extensions = map[string]*Encoding{
	".cue":    cueEnc,
	".json":   jsonEnc,
	".jsonl":  jsonEnc,
	".ndjson": jsonEnc,
	".yaml":   yamlEnc,
	".yml":    yamlEnc,
	".proto":  protodefEnc,
}
