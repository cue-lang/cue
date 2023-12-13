// Copyright 2023 The CUE Authors
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

//go:build ignore

// A tiny tool to DebugPrint a CUE file, for example:
//
//	go run print_tool.go -- file.cue
package main

import (
	"flag"
	"os"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		// We could support multiple arguments or stdin if useful.
		panic("expecting exactly one argument")
	}
	file, err := parser.ParseFile(args[0], nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	ast.DebugPrint(os.Stdout, file)
}
