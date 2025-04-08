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
	"sync"

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

// evalMu guards against concurrent execution of the CUE evaluator.
// See issue https://cuelang.org/issue/2733
var evalMu sync.Mutex

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
		b.Form == "" &&
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
			Docs:         true,
			Attributes:   true,
		}, nil
	}
	evalMu.Lock()
	defer evalMu.Unlock()
	typesInit()
	modeVal := lookup(typesValue, "modes", mode.String())
	fileVal := lookup(modeVal, "FileInfo")
	fileVal = fileVal.FillPath(cue.Path{}, b)

	if b.Encoding == "" {
		ext := lookup(modeVal, "extensions", fileExt(b.Filename))
		if ext.Exists() {
			fileVal = fileVal.Unify(ext)
		}
	}
	var errs errors.Error

	interpretation, _ := lookup(fileVal, "interpretation").String()
	if b.Form != "" {
		fileVal, errs = unifyWith(errs, fileVal, typesValue, "forms", string(b.Form))
		// may leave some encoding-dependent options open in data mode.
	} else if interpretation != "" {
		// always sets form=*schema
		fileVal, errs = unifyWith(errs, fileVal, typesValue, "interpretations", interpretation)
	}
	if interpretation == "" {
		s, err := lookup(fileVal, "encoding").String()
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
	v1 = v1.Unify(lookup(v2, field, value))
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
	evalMu.Lock()
	defer evalMu.Unlock()
	typesInit()

	qualifier := ""
	hasFiles := false

	emptyScope := true
	sc := &scope{}
	for i, s := range args {
		a := strings.Split(s, ":")
		switch {
		case len(a) == 1 || len(a[0]) == 1: // filename
			if s == "" {
				return nil, errors.Newf(token.NoPos, "empty file name")
			}
			if emptyScope && len(a) == 1 && strings.HasSuffix(a[0], ".cue") {
				// Handle majority case.
				f := *fileForCUE
				f.Filename = a[0]
				files = append(files, &f)
				hasFiles = true
				continue
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
			emptyScope = false
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
	// Quickly discard files which we aren't interested in.
	// These cases are very common when loading `./...` in a large repository.
	typesInit()
	if scope == "" && file != "-" {
		ext := fileExt(file)
		if ext == "" {
			return nil, errors.Newf(token.NoPos, "no encoding specified for file %q", file)
		}
		f, ok := fileForExt[ext]
		if !ok {
			return nil, errors.Newf(token.NoPos, "unknown file extension %s", ext)
		}
		if mode == Input {
			f1 := *f
			f1.Filename = file
			return &f1, nil
		}
	}
	sc, err := parseScope(scope)
	if err != nil {
		return nil, err
	}
	return toFile(mode, sc, file)
}

func hasEncoding(v cue.Value) bool {
	return lookup(v, "encoding").Exists()
}

func toFile(mode Mode, sc *scope, filename string) (*build.File, error) {
	modeVal := lookup(typesValue, "modes", mode.String())
	fileVal := lookup(modeVal, "FileInfo")

	for tagName := range sc.topLevel {
		info := lookup(typesValue, "tagInfo", tagName)
		if info.Exists() {
			fileVal = fileVal.Unify(info)
		} else {
			return nil, errors.Newf(token.NoPos, "unknown filetype %s", tagName)
		}
	}
	allowedSubsidiaryBool := lookup(fileVal, "boolTags")
	for tagName, val := range sc.subsidiaryBool {
		if !lookup(allowedSubsidiaryBool, tagName).Exists() {
			return nil, errors.Newf(token.NoPos, "tag %s is not allowed in this context", tagName)
		}
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("boolTags"), cue.Str(tagName)), val)
	}
	allowedSubsidiaryString := lookup(fileVal, "tags")
	for tagName, val := range sc.subsidiaryString {
		if !lookup(allowedSubsidiaryString, tagName).Exists() {
			return nil, errors.Newf(token.NoPos, "tag %s is not allowed in this context", tagName)
		}
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("tags"), cue.Str(tagName)), val)
	}
	if !hasEncoding(fileVal) {
		if filename == "-" {
			fileVal = fileVal.Unify(lookup(modeVal, "Default"))
		} else if ext := fileExt(filename); ext != "" {
			extFile := lookup(modeVal, "extensions", ext)
			if !extFile.Exists() {
				return nil, errors.Newf(token.NoPos, "unknown file extension %s", ext)
			}
			fileVal = fileVal.Unify(extFile)
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
		switch tagTypes[tagName] {
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
func fileExt(f string) string {
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

// lookup looks up the given string field path in v.
func lookup(v cue.Value, elems ...string) cue.Value {
	sels := make([]cue.Selector, len(elems))
	for i := range elems {
		sels[i] = cue.Str(elems[i])
	}
	return v.LookupPath(cue.MakePath(sels...))
}

// structFields returns an iterator over the names of all the regulat fields
// in v and their values.
func structFields(v cue.Value) iter.Seq2[string, cue.Value] {
	return func(yield func(string, cue.Value) bool) {
		if !v.Exists() {
			return
		}
		iter, err := v.Fields()
		if err != nil {
			return
		}
		for iter.Next() {
			if !yield(iter.Selector().Unquoted(), iter.Value()) {
				break
			}
		}
	}
}
