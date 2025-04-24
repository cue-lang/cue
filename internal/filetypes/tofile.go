// Copyright  CUE Authors
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
	"fmt"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"github.com/google/go-cmp/cmp"
)

// evalMu guards against concurrent execution of the CUE evaluator.
// See issue https://cuelang.org/issue/2733
var evalMu sync.Mutex

//go:generate go run -tags bootstrap ./generate.go

func toFile(mode Mode, sc *scope, filename string) (*build.File, error) {
	f0, err0 := toFileGenerated(mode, sc, filename)
	f1, err1 := toFileOrig(mode, sc, filename)
	if (err0 != nil) != (err1 != nil) {
		panic(fmt.Errorf("toFile discrepancy on error return; mode %v; scope %v; filename %v:\nold: %v\nnew: %v", mode, sc, filename, err1, err0))
	} else if diff := cmp.Diff(f0, f1); diff != "" {
		panic(fmt.Errorf("toFile result discrepancy; mode %v; scope %v; filename %v:\n%s", mode, sc, filename, diff))
	}

	return f0, err0
}

func toFileOrig(mode Mode, sc *scope, filename string) (*build.File, error) {
	fileVal := cuecontext.New().CompileString("{}")
	for tagName := range sc.topLevel {
		info := lookup(typesValue, "tagInfo", tagName)
		if info.Exists() {
			fileVal = fileVal.Unify(info)
		} else {
			return nil, errors.Newf(token.NoPos, "unknown filetype %s", tagName)
		}
	}
	modeVal := lookup(typesValue, "modes", mode.String())
	fileVal = fileVal.Unify(lookup(modeVal, "FileInfo"))
	return toFile1(modeVal, fileVal, filename, sc)
}

func toFile1(modeVal, fileVal cue.Value, filename string, sc *scope) (*build.File, error) {
	if !lookup(fileVal, "encoding").Exists() {
		if ext := fileExt(filename); ext != "" {
			extFile := lookup(modeVal, "extensions", ext)
			if !extFile.Exists() {
				return nil, errors.Newf(token.NoPos, "unknown file extension %s", ext)
			}
			fileVal = fileVal.Unify(extFile)
		} else {
			return nil, errors.Newf(token.NoPos, "no encoding specified for file %q", filename)
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

	// Note that the filename is only filled in the Go value, and not the CUE value.
	// This makes no difference to the logic, but saves a non-trivial amount of evaluator work.
	f := &build.File{Filename: filename}
	if err := fileVal.Decode(f); err != nil {
		return nil, errors.Wrapf(err, token.NoPos,
			"could not determine file type")
	}
	return f, nil
}

// FromFile returns detailed file info for a given build file. It ignores b.Tags and
// b.BoolTags, instead assuming that any tag handling has already been processed
// by [ParseArgs] or similar.
// The b.Encoding field must be non-empty.
func FromFile(b *build.File, mode Mode) (*FileInfo, error) {
	fi0, err0 := fromFileGenerated(b, mode)
	fi1, err1 := fromFileOrig(b, mode)
	if (err0 != nil) != (err1 != nil) {
		panic(fmt.Errorf("toFile discrepancy on error return; mode %v; file %#v:\nold: %v\nnew: %v", mode, b, err1, err0))
	} else if diff := cmp.Diff(fi1, fi0); diff != "" {
		panic(fmt.Errorf("toFile result discrepancy; mode %v; file %#v\n%s", mode, b, diff))
	}

	return fi0, err0
}

func fromFileOrig(b *build.File, mode Mode) (*FileInfo, error) {
	evalMu.Lock()
	defer evalMu.Unlock()
	typesInit()
	modeVal := lookup(typesValue, "modes", mode.String())
	fileVal := lookup(modeVal, "FileInfo")
	if b.Encoding != "" {
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("encoding")), b.Encoding)
	}
	if b.Interpretation != "" {
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("interpretation")), b.Interpretation)
	}
	if b.Form != "" {
		fileVal = fileVal.FillPath(cue.MakePath(cue.Str("form")), b.Form)
	}
	if b.Encoding == "" {
		return nil, errors.Newf(token.NoPos, "no encoding specified")
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
	fi.Filename = b.Filename
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

// lookup looks up the given string field path in v.
func lookup(v cue.Value, elems ...string) cue.Value {
	sels := make([]cue.Selector, len(elems))
	for i := range elems {
		sels[i] = cue.Str(elems[i])
	}
	return v.LookupPath(cue.MakePath(sels...))
}
