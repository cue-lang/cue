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
	"log"
	"path"
	"sort"

	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
)

var list = flag.String("list", "", "list all failed tests for a given evaluator version")

const testDir = "testdata/external"

func main() {
	tests, err := externaltest.ReadTestDir(testDir)
	if err != nil {
		log.Fatal(err)
	}
	flag.Parse()
	if *list != "" {
		listFailures(*list, tests)
	} else {
		fmt.Printf("v2:\n")
		showStats("v2", tests)
		fmt.Println()
		fmt.Printf("v3:\n")
		showStats("v3", tests)
	}
}

func showStats(version string, tests map[string][]*externaltest.Schema) {
	schemaOK := 0
	schemaTot := 0
	testOK := 0
	testTot := 0
	schemaOKTestOK := 0
	schemaOKTestTot := 0
	for _, schemas := range tests {
		for _, schema := range schemas {
			schemaTot++
			if schema.Skip[version] == "" {
				schemaOK++
			}
			for _, test := range schema.Tests {
				testTot++
				if test.Skip[version] == "" {
					testOK++
				}
				if schema.Skip[version] == "" {
					schemaOKTestTot++
					if test.Skip[version] == "" {
						schemaOKTestOK++
					}
				}
			}
		}
	}
	fmt.Printf("\tschema extract (pass / total): %d / %d = %.1f%%\n", schemaOK, schemaTot, percent(schemaOK, schemaTot))
	fmt.Printf("\ttests (pass / total): %d / %d = %.1f%%\n", testOK, testTot, percent(testOK, testTot))
	fmt.Printf("\ttests on extracted schemas (pass / total): %d / %d = %.1f%%\n", schemaOKTestOK, schemaOKTestTot, percent(schemaOKTestOK, schemaOKTestTot))
}

func listFailures(version string, tests map[string][]*externaltest.Schema) {
	for _, filename := range sortedKeys(tests) {
		schemas := tests[filename]
		for _, schema := range schemas {
			if schema.Skip[version] != "" {
				fmt.Printf("%s: schema fail (%s)\n", testdataPos(schema), schema.Description)
				continue
			}
			for _, test := range schema.Tests {
				if test.Skip[version] != "" {
					reason := "fail"
					if !test.Valid {
						reason = "unexpected success"
					}
					fmt.Printf("%s: %s (%s; %s)\n", testdataPos(test), reason, schema.Description, test.Description)
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

func percent(a, b int) float64 {
	return (float64(a) / float64(b)) * 100.0
}

func sortedKeys[T any](m map[string]T) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
