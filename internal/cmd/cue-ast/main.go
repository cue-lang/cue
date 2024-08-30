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
	"log"
	"os"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/astinternal"
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), `
usage of cue-ast:

  cue-ast print [flags] [inputs]

    Write multi-line Go-like representations of CUE syntax trees.

      -omitempty    omit empty and invalid values

  cue-ast join [flags] [inputs]

    Join the input package instance as a single file.
    Joining multiple package instances is not supported yet.

See 'cue help inputs' as well.
`[1:])
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}
	name, args := os.Args[1], os.Args[2:]
	switch name {
	case "print":
		var cfg astinternal.DebugConfig
		flag.BoolVar(&cfg.OmitEmpty, "omitempty", false, "")
		// Note that DebugConfig also has a Filter func, but that doesn't lend itself well
		// to a command line flag. Perhaps we could provide some commonly used filters,
		// such as "positions only" or "skip positions".
		flag.CommandLine.Parse(args)

		// TODO: should we produce output in txtar form for the sake of
		// more clearly separating the AST for each file?
		// [ast.File.Filename] already has the full filename,
		// but as one of the first fields it's not a great separator.
		insts := load.Instances(flag.Args(), &load.Config{})
		for _, inst := range insts {
			if err := inst.Err; err != nil {
				log.Fatal(errors.Details(err, nil))
			}
			for _, file := range inst.Files {
				out := astinternal.AppendDebug(nil, file, cfg)
				os.Stdout.Write(out)
			}
		}
	case "join":
		// TODO: add a flag drop comments, which is useful when reducing bug reproducers.
		flag.CommandLine.Parse(args)

		var jointImports []*ast.ImportSpec
		var jointFields []ast.Decl
		insts := load.Instances(flag.Args(), &load.Config{})
		if len(insts) != 1 {
			log.Fatal("joining multiple instances is not possible yet")
		}
		inst := insts[0]
		if err := inst.Err; err != nil {
			log.Fatal(errors.Details(err, nil))
		}
		for _, file := range inst.Files {
			jointImports = slices.Concat(jointImports, file.Imports)

			fields := file.Decls[len(file.Preamble()):]
			jointFields = slices.Concat(jointFields, fields)
		}
		// TODO: we should sort and deduplicate imports.
		joint := &ast.File{Decls: slices.Concat([]ast.Decl{
			&ast.ImportDecl{Specs: jointImports},
		}, jointFields)}

		// Sanitize the resulting file so that, for example,
		// multiple packages imported as the same name avoid collisions.
		if err := astutil.Sanitize(joint); err != nil {
			log.Fatal(err)
		}

		out, err := format.Node(joint)
		if err != nil {
			log.Fatal(err)
		}
		os.Stdout.Write(out)
	default:
		flag.Usage()
	}
}
