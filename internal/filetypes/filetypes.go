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

package filetypes

import (
	"iter"
	"path/filepath"
	"strconv"
	"strings"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// Mode indicate the base mode of operation and indicates a different set of
// defaults.
type Mode int

const (
	Input Mode = iota // The default
	Export
	Def
	Eval
)

func (m Mode) String() string {
	switch m {
	default:
		return "input"
	case Eval:
		return "eval"
	case Export:
		return "export"
	case Def:
		return "def"
	}
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

// ParseArgs converts a sequence of command line arguments representing
// files into a sequence of build file specifications.
//
// The arguments are of the form
//
//	file* (spec: file+)*
//
// where file is a filename and spec is itself of the form
//
//	tag[=value]('+'tag[=value])*
//
// A file type spec applies to all its following files and until a next spec
// is found.
//
// Examples:
//
//	json: foo.data bar.data json+schema: bar.schema
func ParseArgs(args []string) (files []*build.File, err error) {
	evalMu.Lock()
	defer evalMu.Unlock()
	typesInit()

	qualifier := ""
	hasFiles := false

	sc := &scope{}
	for i, s := range args {
		a := strings.Split(s, ":")
		switch {
		case len(a) == 1 || len(a[0]) == 1: // filename
			if s == "" {
				return nil, errors.Newf(token.NoPos, "empty file name")
			}
			f, err := toFile(Input, sc, s)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
			hasFiles = true

		case len(a) > 2 || a[0] == "":
			return nil, errors.Newf(token.NoPos,
				"unsupported file name %q: may not have ':'", s)

		case a[1] != "":
			return nil, errors.Newf(token.NoPos, "cannot combine scope with file")

		default: // scope
			switch {
			case i == len(args)-1:
				qualifier = a[0]
				fallthrough
			case qualifier != "" && !hasFiles:
				return nil, errors.Newf(token.NoPos, "scoped qualifier %q without file", qualifier+":")
			}
			sc, err = parseScope(a[0])
			if err != nil {
				return nil, err
			}
			qualifier = a[0]
			hasFiles = false
		}
	}

	return files, nil
}

// DefaultTagsForInterpretation returns any tags that would be set by default
// in the given interpretation in the given mode.
func DefaultTagsForInterpretation(interp build.Interpretation, mode Mode) map[string]bool {
	if interp == "" {
		return nil
	}
	evalMu.Lock()
	defer evalMu.Unlock()
	// TODO this could be done once only.

	// This should never fail if called with a legitimate build.Interpretation constant.
	f, err := toFile(mode, &scope{
		topLevel: map[string]bool{
			string(interp): true,
		},
	}, "-")
	if err != nil {
		panic(err)
	}
	return f.BoolTags
}

// ParseFile parses a single-argument file specifier, such as when a file is
// passed to a command line argument.
//
// Example:
//
//	cue eval -o yaml:foo.data
func ParseFile(s string, mode Mode) (*build.File, error) {
	scope := ""
	file := s

	if p := strings.LastIndexByte(s, ':'); p >= 0 {
		scope = s[:p]
		file = s[p+1:]
		if scope == "" {
			return nil, errors.Newf(token.NoPos, "unsupported file name %q: may not have ':", s)
		}
	}

	if file == "" {
		if s != "" {
			return nil, errors.Newf(token.NoPos, "empty file name in %q", s)
		}
		return nil, errors.Newf(token.NoPos, "empty file name")
	}

	return ParseFileAndType(file, scope, mode)
}

// ParseFileAndType parses a file and type combo.
func ParseFileAndType(file, scope string, mode Mode) (*build.File, error) {
	evalMu.Lock()
	defer evalMu.Unlock()
	typesInit()
	sc, err := parseScope(scope)
	if err != nil {
		return nil, err
	}
	return toFile(mode, sc, file)
}

// scope holds attributes that influence encoding and decoding.
// Together with the mode and the file name, they determine
// a number of properties of the encoding process.
type scope struct {
	topLevel         map[string]bool
	subsidiaryBool   map[string]bool
	subsidiaryString map[string]string
}

func parseScope(scopeStr string) (*scope, error) {
	if scopeStr == "" {
		return &scope{}, nil
	}
	sc := scope{
		topLevel:         make(map[string]bool),
		subsidiaryBool:   make(map[string]bool),
		subsidiaryString: make(map[string]string),
	}
	for _, tag := range strings.Split(scopeStr, "+") {
		tagName, tagVal, hasValue := strings.Cut(tag, "=")
		switch tagTypeOf(tagName) {
		case tagTopLevel:
			if hasValue {
				return nil, errors.Newf(token.NoPos, "cannot specify value for tag %q", tagName)
			}
			sc.topLevel[tagName] = true
		case tagSubsidiaryBool:
			if hasValue {
				t, err := strconv.ParseBool(tagVal)
				if err != nil {
					return nil, errors.Newf(token.NoPos, "invalid boolean value for tag %q", tagName)
				}
				sc.subsidiaryBool[tagName] = t
			} else {
				sc.subsidiaryBool[tagName] = true
			}
		case tagSubsidiaryString:
			if !hasValue {
				return nil, errors.Newf(token.NoPos, "tag %q must have value (%s=<value>)", tagName, tagName)
			}
			sc.subsidiaryString[tagName] = tagVal
		default:
			return nil, errors.Newf(token.NoPos, "unknown filetype %s", tagName)
		}
	}
	return &sc, nil
}

// fileExt is like filepath.Ext except we don't treat file names starting with "." as having an extension
// unless there's also another . in the name.
//
// It also treats "-" as a special case, so we treat stdin/stdout as
// a regular file.
func fileExt(f string) string {
	if f == "-" {
		return "-"
	}
	e := filepath.Ext(f)
	if e == "" || e == filepath.Base(f) {
		return ""
	}
	return e
}

func seqConcat[T any](iters ...iter.Seq[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, it := range iters {
			for x := range it {
				if !yield(x) {
					return
				}
			}
		}
	}
}
