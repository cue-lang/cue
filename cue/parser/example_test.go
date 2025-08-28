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

package parser_test

import (
	"fmt"

	"cuelang.org/go/cue/parser"
)

func ExampleParseFile() {
	// Parse some CUE source but stop after processing the imports.
	f, err := parser.ParseFile("example.cue", `
		import "math"

		foo: 1
		bar: "baz"
	`, parser.ImportsOnly)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Print the imports from the file's AST.
	for spec := range f.ImportSpecs() {
		fmt.Println(spec.Path.Value)
	}
	// Output:
	// "math"
}
