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

// cue-ast-print parses a CUE file and prints its syntax tree, for example:
//
//	cue-ast-print file.cue
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/astinternal"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: cue-ast-print [flags] [file.cue]\n")
		flag.PrintDefaults()
	}

	var cfg astinternal.DebugConfig
	flag.BoolVar(&cfg.OmitEmpty, "omitempty", false, "omit empty and invalid values")
	// Note that DebugConfig also has a Filter func, but that doesn't lend itself well
	// to a command line flag. Perhaps we could provide some commonly used filters,
	// such as "positions only" or "skip positions".
	flag.Parse()

	var filename string
	var src any
	switch flag.NArg() {
	case 0:
		filename = "<stdin>"
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		src = data
	case 1:
		filename = flag.Arg(0)
	default:
		flag.Usage()
	}
	file, err := parser.ParseFile(filename, src, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}
	out := astinternal.AppendDebug(nil, file, cfg)
	os.Stdout.Write(out)
}
