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
	*build.File

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

// TODO(mvdan): the funcs below make use of typesValue concurrently,
// even though we clearly document that cue.Values are not safe for concurrent use.
// It seems to be OK in practice, as otherwise we would run into `go test -race` failures.

// FromFile return detailed file info for a given build file.
// Encoding must be specified.
// TODO: mode should probably not be necessary here.
func FromFile(b *build.File, mode Mode) (*FileInfo, error) {
	// Handle common case. This allows certain test cases to be analyzed in
	// isolation without interference from evaluating these files.
	if mode == Input &&
		b.Encoding == build.CUE &&
		b.Form == build.Schema &&
		b.Interpretation == "" {
		return &FileInfo{
			File: b,

			Definitions:  true,
			Data:         true,
			Optional:     true,
			Constraints:  true,
			References:   true,
			Cycles:       true,
			KeepDefaults: true,
			Incomplete:   true,
			Imports:      true,
			Stream:       true,
			Docs:         true,
			Attributes:   true,
		}, nil
	}

	typesInit()
	modeVal := typesValue.LookupPath(cue.MakePath(cue.Str("modes"), cue.Str(mode.String())))
	fileVal := modeVal.LookupPath(cue.MakePath(cue.Str("FileInfo")))
	fileVal = fileVal.FillPath(cue.Path{}, b)

	if b.Encoding == "" {
		ext := modeVal.LookupPath(cue.MakePath(cue.Str("extensions"), cue.Str(fileExt(b.Filename))))
		if ext.Exists() {
			fileVal = fileVal.Unify(ext)
		}
	}
	var errs errors.Error

	interpretation, _ := fileVal.LookupPath(cue.MakePath(cue.Str("interpretation"))).String()
	if b.Form != "" {
		fileVal, errs = unifyWith(errs, fileVal, typesValue, "forms", string(b.Form))
		// may leave some encoding-dependent options open in data mode.
	} else if interpretation != "" {
		// always sets schema form.
		fileVal, errs = unifyWith(errs, fileVal, typesValue, "interpretations", interpretation)
	}
	if interpretation == "" {
		s, err := fileVal.LookupPath(cue.MakePath(cue.Str("encoding"))).String()
		if err != nil {
			return nil, err
		}
		fileVal, errs = unifyWith(errs, fileVal, modeVal, "encodings", s)
	}

	fi := &FileInfo{}
	if err := fileVal.Decode(fi); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "could not parse arguments")
	}
	return fi, errs
}

// unifyWith returns the equivalent of `v1 & v2[field][value]`.
func unifyWith(errs errors.Error, v1, v2 cue.Value, field, value string) (cue.Value, errors.Error) {
	v1 = v1.Unify(v2.LookupPath(cue.MakePath(cue.Str(field), cue.Str(value))))
	if err := v1.Err(); err != nil {
		errs = errors.Append(errs,
			errors.Newf(token.NoPos, "unknown %s %s", field, value))
	}
	return v1, errs
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
	typesInit()
	var modeVal, fileVal cue.Value

	qualifier := ""
	hasFiles := false

	for i, s := range args {
		a := strings.Split(s, ":")
		switch {
		case len(a) == 1 || len(a[0]) == 1: // filename
			if !fileVal.Exists() {
				if len(a) == 1 && strings.HasSuffix(a[0], ".cue") {
					// Handle majority case.
					files = append(files, &build.File{
						Filename: a[0],
						Encoding: build.CUE,
						Form:     build.Schema,
					})
					hasFiles = true
					continue
				}

				// The CUE command works just fine without this (how?),
				// but the API tests require this for some reason.
				//
				// This is almost certainly wrong, and in the wrong place.
				//
				// TODO(aram): why do we need this here?
				if len(a) == 1 && strings.HasSuffix(a[0], ".wasm") {
					continue
				}

				modeVal, fileVal, err = parseType("", Input)
				if err != nil {
					return nil, err
				}
			}
			if s == "" {
				return nil, errors.Newf(token.NoPos, "empty file name")
			}
			f, err := toFile(modeVal, fileVal, s)
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
			modeVal, fileVal, err = parseType(a[0], Input)
			if err != nil {
				return nil, err
			}
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
	// Quickly discard files which we aren't interested in.
	// These cases are very common when loading `./...` in a large repository.
	typesInit()
	if scope == "" {
		ext := fileExt(file)
		if file == "-" {
			// not handled here
		} else if ext == "" {
			return nil, errors.Newf(token.NoPos, "no encoding specified for file %q", file)
		} else if !knownExtensions[ext] {
			return nil, errors.Newf(token.NoPos, "unknown file extension %s", ext)
		}
	}
	modeVal, fileVal, err := parseType(scope, mode)
	if err != nil {
		return nil, err
	}
	return toFile(modeVal, fileVal, file)
}

func hasEncoding(v cue.Value) bool {
	enc := v.LookupPath(cue.MakePath(cue.Str("encoding")))
	d, _ := enc.Default()
	return d.IsConcrete()
}

func toFile(modeVal, fileVal cue.Value, filename string) (*build.File, error) {
	if !hasEncoding(fileVal) {
		if filename == "-" {
			fileVal = fileVal.Unify(modeVal.LookupPath(cue.MakePath(cue.Str("Default"))))
		} else if ext := fileExt(filename); ext != "" {
			extFile := modeVal.LookupPath(cue.MakePath(cue.Str("extensions"), cue.Str(ext)))
			fileVal = fileVal.Unify(extFile)
			if err := fileVal.Err(); err != nil {
				return nil, errors.Newf(token.NoPos, "unknown file extension %s", ext)
			}
		} else {
			return nil, errors.Newf(token.NoPos, "no encoding specified for file %q", filename)
		}
	}

	// Note that the filename is only filled in the Go value, and not the CUE value.
	// This makes no difference to the logic, but saves a non-trivial amount of evaluator work.
	f := &build.File{Filename: filename}
	if err := fileVal.Decode(&f); err != nil {
		return nil, errors.Wrapf(err, token.NoPos,
			"could not determine file type")
	}
	return f, nil
}

func parseType(scope string, mode Mode) (modeVal, fileVal cue.Value, _ error) {
	modeVal = typesValue.LookupPath(cue.MakePath(cue.Str("modes"), cue.Str(mode.String())))
	fileVal = modeVal.LookupPath(cue.MakePath(cue.Str("File")))

	if scope != "" {
		for _, tag := range strings.Split(scope, "+") {
			tagName, tagVal, ok := strings.Cut(tag, "=")
			if ok {
				fileVal = fileVal.FillPath(cue.MakePath(cue.Str("tags"), cue.Str(tagName)), tagVal)
			} else {
				info := typesValue.LookupPath(cue.MakePath(cue.Str("tags"), cue.Str(tag)))
				if !info.Exists() {
					return cue.Value{}, cue.Value{}, errors.Newf(token.NoPos, "unknown filetype %s", tag)
				}
				fileVal = fileVal.Unify(info)
			}
		}
	}

	return modeVal, fileVal, nil
}

// fileExt is like filepath.Ext except we don't treat file names starting with "." as having an extension
// unless there's also another . in the name.
func fileExt(f string) string {
	e := filepath.Ext(f)
	if e == "" || e == filepath.Base(f) {
		return ""
	}
	return e
}
