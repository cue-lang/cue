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

package astinternal_test

import (
	"path"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetxtar"

	"github.com/go-quicktest/qt"
)

var ptrPat = regexp.MustCompile(`0x[0-9a-z]+`)

func TestDebugPrint(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "testdata",
		Name: "debugprint",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		includePointers := t.HasTag("includePointers")
		for _, file := range t.Archive.Files {
			if strings.HasPrefix(file.Name, "out/") {
				continue
			}

			f, err := parser.ParseFile(file.Name, file.Data, parser.ParseComments)
			qt.Assert(t, qt.IsNil(err))

			// The full syntax tree, as printed by default.
			// We enable IncludeNodeRefs because it only adds information
			// that would not otherwise be present.
			// The syntax tree does not contain any maps, so
			// the generated reference names should be deterministic.
			full := astinternal.AppendDebug(nil, f, astinternal.DebugConfig{
				IncludeNodeRefs: true,
				IncludePointers: includePointers,
			})
			if includePointers {
				full = ptrPat.ReplaceAll(full, []byte("XXXX"))
			}
			t.Writer(file.Name).Write(full)

			// A syntax tree which omits any empty values,
			// and is only interested in showing string fields.
			// We allow ast.Nodes and slices to not stop too early.
			typNode := reflect.TypeFor[ast.Node]()
			strings := astinternal.AppendDebug(nil, f, astinternal.DebugConfig{
				OmitEmpty: true,
				Filter: func(v reflect.Value) bool {
					if v.Type().Implements(typNode) {
						return true
					}
					switch v.Kind() {
					case reflect.Slice, reflect.String:
						return true
					default:
						return false
					}
				},
			})
			t.Writer(path.Join(file.Name, "omitempty-strings")).Write(strings)
		}
	})
}
