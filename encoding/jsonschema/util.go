// Copyright 2024 CUE Authors
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

package jsonschema

import (
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema/internal/structbuilder"
)

// TODO a bunch of stuff in this file is potentially suitable
// for more general use. Consider moving some of it
// to the cue package.

func pathConcat(p1, p2 cue.Path) cue.Path {
	sels1, sels2 := p1.Selectors(), p2.Selectors()
	if len(sels1) == 0 {
		return p2
	}
	if len(sels2) == 0 {
		return p1
	}
	return cue.MakePath(slices.Concat(sels1, sels2)...)
}

func labelsToCUEPath(labels []ast.Label) (cue.Path, error) {
	sels := make([]cue.Selector, len(labels))
	for i, label := range labels {
		// Note: we can't use cue.Label because that doesn't
		// allow hidden fields.
		sels[i] = structbuilder.SelectorForLabel(label)
	}
	path := cue.MakePath(sels...)
	if err := path.Err(); err != nil {
		return cue.Path{}, err
	}
	return path, nil
}

func mustCUEPathToJSONPointer(p cue.Path) string {
	ptr, err := json.PointerFromCUEPath(p)
	if err != nil {
		panic(err)
	}
	return string(ptr)
}

// relPath returns the path to v relative to root,
// which must be a direct ancestor of v.
func relPath(v, root cue.Value) cue.Path {
	rootPath := root.Path().Selectors()
	vPath := v.Path().Selectors()
	if !sliceHasPrefix(vPath, rootPath) {
		panic("value is not inside root")
	}
	return cue.MakePath(vPath[len(rootPath):]...)
}

func sliceHasPrefix[E comparable](s1, s2 []E) bool {
	if len(s2) > len(s1) {
		return false
	}
	return slices.Equal(s1[:len(s2)], s2)
}
