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

//go:generate go run gen.go

package filetypes

import (
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
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
)

func (m Mode) String() string {
	switch m {
	default:
		return "input"
	case Export:
		return "export"
	case Def:
		return "def"
	}
}

// FileInfo defines the parsing plan for a file.
type FileInfo struct {
	*build.File
	// Filename string `json:"filename"`

	// Tags map[string]string `json:"tags"` // code=go

	Definitions bool `json:"definitions"`
	Data        bool `json:"data"`
	Optional    bool `json:"optional"`
	Constraints bool `json:"constraints"`
	References  bool `json:"references"`
	Cycles      bool `json:"cycles"`
	Imports     bool `json:"imports"`
	Stream      bool `json:"stream"`

	Docs       bool `json:"docs"`
	Attributes bool `json:"attributes"`
}

// FromFile return detailed file info for a given build file.
// Encoding must be specified.
func FromFile(b *build.File, mode Mode) (*FileInfo, error) {
	i := cuegenInstance.Value()
	i = i.Unify(i.Lookup("modes", mode.String()))
	v := i.LookupDef("FileInfo")
	v = v.Fill(b)

	if b.Encoding == "" {
		ext := i.Lookup("extensions", filepath.Ext(b.Filename))
		if ext.Exists() {
			v = v.Unify(ext)
		}
	}

	if s, _ := v.Lookup("interpretation").String(); s != "" {
		v = v.Unify(i.Lookup("interpretations", s))
	} else {
		s, err := v.Lookup("encoding").String()
		if err != nil {
			return nil, err
		}
		v = v.Unify(i.Lookup("encodings", s))

	}
	if b.Form != "" {
		v = v.Unify(i.Lookup("forms", string(b.Form)))
	}

	fi := &FileInfo{}
	if err := v.Decode(fi); err != nil {
		return nil, err
	}
	return fi, nil
}

// ParseArgs converts a sequence of command line arguments representing
// files into a sequence of build file specifications.
//
// The arguments are of the form
//
//     file* (spec: file+)*
//
// where file is a filename and spec is itself of the form
//
//     tag[=value]('+'tag[=value])*
//
// A file type spec applies to all its following files and until a next spec
// is found.
//
// Examples:
//     json: foo.data bar.data json+schema: bar.schema
//
func ParseArgs(args []string) (files []*build.File, err error) {
	v := parseType("", Input)

	qualifier := ""
	hasFiles := false

	for i, s := range args {
		a := strings.Split(s, ":")
		switch {
		case len(a) == 1: // filename
			f, err := toFile(v, s)
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
			v = parseType(a[0], Input)
			qualifier = a[0]
			hasFiles = false
		}
	}

	return files, nil
}

// ParseFile parses a single-argument file specifier, such as when a file is
// passed to a command line argument.
//
// Example:
//   cue eval -o yaml:foo.data
//
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
		return nil, errors.Newf(token.NoPos, "empty file name in %q", s)
	}

	return toFile(parseType(scope, mode), file)
}

func toFile(v cue.Value, filename string) (*build.File, error) {
	v = v.Fill(filename, "filename")
	if len(filename) > 1 { // omit "" and -
		if s, _ := v.Lookup("encoding").String(); s == "" {
			v = v.Unify(cuegenInstance.Lookup("extensions", filepath.Ext(filename)))
		}
	}
	f := &build.File{}
	if err := v.Decode(&f); err != nil {
		return nil, err
	}
	return f, nil
}

func parseType(s string, mode Mode) cue.Value {
	i := cuegenInstance.Value()
	i = i.Unify(i.Lookup("modes", "def"))
	v := i.LookupDef("File")

	if s != "" {
		for _, t := range strings.Split(s, "+") {
			if p := strings.IndexByte(t, '='); p >= 0 {
				v = v.Fill(t[p+1:], "tags", t[:p])
			} else {
				v = v.Unify(i.Lookup("tags", t))
			}
		}
	}

	return v
}
