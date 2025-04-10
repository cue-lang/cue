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
	_ "embed"
	"fmt"
	"iter"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
)

//go:embed types.cue
var typesCUE string

var (
	typesValue cue.Value
	fileForExt map[string]*build.File
	fileForCUE *build.File
	tagTypes   map[string]tagType
)

type tagType int

const (
	tagUnknown tagType = iota
	tagTopLevel
	tagSubsidiaryBool
	tagSubsidiaryString
)

var typesInit = sync.OnceFunc(func() {
	ctx := cuecontext.New()
	typesValue = ctx.CompileString(typesCUE, cue.Filename("types.cue"))
	if err := typesValue.Err(); err != nil {
		panic(err)
	}
	// Reading a file in input mode with a non-explicit scope is a very
	// common operation, so cache the build.File value for all
	// the known file extensions.
	if err := typesValue.LookupPath(cue.MakePath(cue.Str("fileForExtVanilla"))).Decode(&fileForExt); err != nil {
		panic(err)
	}
	fileForCUE = fileForExt[".cue"]
	// Check invariants assumed by FromFile
	if fileForCUE.Form != "" || fileForCUE.Interpretation != "" || fileForCUE.Encoding != build.CUE {
		panic(fmt.Errorf("unexpected value for CUE file type: %#v", fileForCUE))
	}
	tagTypes = make(map[string]tagType)
	setType := func(name string, typ tagType) {
		if otherTyp, ok := tagTypes[name]; ok && typ != otherTyp {
			panic("tag redefinition")
		}
		tagTypes[name] = typ
	}
	addSubsidiary := func(v cue.Value) {
		for tagName := range structFields(lookup(v, "boolTags")) {
			setType(tagName, tagSubsidiaryBool)
		}
		for tagName := range structFields(lookup(v, "tags")) {
			setType(tagName, tagSubsidiaryString)
		}
	}
	for tagName, v := range structFields(lookup(typesValue, "tagInfo")) {
		setType(tagName, tagTopLevel)
		addSubsidiary(v)
	}
	addSubsidiary(lookup(typesValue, "interpretations"))
	addSubsidiary(lookup(typesValue, "forms"))
})

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
