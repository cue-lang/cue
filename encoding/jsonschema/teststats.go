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

//go:build ignore

// This command prints a summary of which external tests are passing and failing.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"slices"

	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
)

const testDir = "testdata/external"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: teststats version\n")
		fmt.Fprintf(os.Stderr, "\nList all failed tests for the given evaluator version (e.g. v2 or v3)\n")
		os.Exit(2)
	}
	flag.Parse()
	tests, err := externaltest.ReadTestDir(testDir)
	if err != nil {
		log.Fatal(err)
	}
	if flag.NArg() != 1 {
		flag.Usage()
	}
	listFailures(os.Stdout, flag.Arg(0), tests)
}

func listFailures(outw io.Writer, version string, tests map[string][]*externaltest.Schema) {
	for _, filename := range sortedKeys(tests) {
		schemas := tests[filename]
		for _, schema := range schemas {
			if schema.Skip[version] != "" {
				fmt.Fprintf(outw, "%s: schema fail (%s)\n", testdataPos(schema), schema.Description)
				continue
			}
			for _, test := range schema.Tests {
				if test.Skip[version] != "" {
					reason := "fail"
					if !test.Valid {
						reason = "unexpected success"
					}
					fmt.Fprintf(outw, "%s: %s (%s; %s)\n", testdataPos(test), reason, schema.Description, test.Description)
				}
			}
		}
	}
}

type positioner interface {
	Pos() token.Pos
}

func testdataPos(p positioner) token.Position {
	pp := p.Pos().Position()
	pp.Filename = path.Join(testDir, pp.Filename)
	return pp
}

func sortedKeys[T any](m map[string]T) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}
