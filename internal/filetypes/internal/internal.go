// Copyright 2025 CUE Authors
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

// Package internal holds some internal parts of the filetypes package
// that need to be shared between the code generator and the package proper.
package internal

import "cuelang.org/go/cue/build"

type ErrorKind int

const (
	ErrNoError ErrorKind = iota
	ErrUnknownFileExtension
	ErrCouldNotDetermineFileType
	ErrNoEncodingSpecified
	NumErrorKinds
)

type Aspects uint64

const (
	Definitions Aspects = 1 << iota
	Data
	Optional
	Constraints
	References
	Cycles
	KeepDefaults
	Incomplete
	Imports
	Stream
	Docs
	Attributes

	AllAspects Aspects = (1 << iota) - 1
)

func (f FileInfo) Aspects() Aspects {
	return 0 |
		when(f.Definitions, Definitions) |
		when(f.Data, Data) |
		when(f.Optional, Optional) |
		when(f.Constraints, Constraints) |
		when(f.References, References) |
		when(f.Cycles, Cycles) |
		when(f.KeepDefaults, KeepDefaults) |
		when(f.Incomplete, Incomplete) |
		when(f.Imports, Imports) |
		when(f.Stream, Stream) |
		when(f.Docs, Docs) |
		when(f.Attributes, Attributes)
}

func (f *FileInfo) SetAspects(a Aspects) {
	f.Definitions = (a & Definitions) != 0
	f.Data = (a & Data) != 0
	f.Optional = (a & Optional) != 0
	f.Constraints = (a & Constraints) != 0
	f.References = (a & References) != 0
	f.Cycles = (a & Cycles) != 0
	f.KeepDefaults = (a & KeepDefaults) != 0
	f.Incomplete = (a & Incomplete) != 0
	f.Imports = (a & Imports) != 0
	f.Stream = (a & Stream) != 0
	f.Docs = (a & Docs) != 0
	f.Attributes = (a & Attributes) != 0
}

func when[T ~uint64](b bool, mask T) T {
	if b {
		return mask
	}
	return 0
}

// FileInfo defines the parsing plan for a file.
type FileInfo struct {
	Filename       string               `json:"filename"`
	Encoding       build.Encoding       `json:"encoding,omitempty"`
	Interpretation build.Interpretation `json:"interpretation,omitempty"`
	Form           build.Form           `json:"form,omitempty"`

	Definitions  bool `json:"definitions"`  // include/allow definition fields
	Data         bool `json:"data"`         // include/allow regular fields
	Optional     bool `json:"optional"`     // include/allow definition fields
	Constraints  bool `json:"constraints"`  // include/allow constraints
	References   bool `json:"references"`   // don't resolve/allow references
	Cycles       bool `json:"cycles"`       // cycles are permitted
	KeepDefaults bool `json:"keepDefaults"` // select/allow default values
	Incomplete   bool `json:"incomplete"`   // permit incomplete values
	Imports      bool `json:"imports"`      // don't expand/allow imports
	Stream       bool `json:"stream"`       // permit streaming
	Docs         bool `json:"docs"`         // show/allow docs
	Attributes   bool `json:"attributes"`   // include/allow attributes
}
