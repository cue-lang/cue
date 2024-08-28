//go:build ignore

// This command prints a summary of which external tests are passing and failing.
package main

import (
	"flag"
	"fmt"
	"log"
	"path"
	"sort"

	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
)

var list = flag.Bool("list", false, "list all failed tests")

func main() {
	tests, err := externaltest.ReadTestDir("testdata/external")
	if err != nil {
		log.Fatal(err)
	}
	flag.Parse()
	if *list {
		listFailures(tests)
	} else {
		showStats(tests)
	}
}

func showStats(tests map[string][]*externaltest.Schema) {
	schemaOK := 0
	schemaTot := 0
	testOK := 0
	testTot := 0
	schemaOKTestOK := 0
	schemaOKTestTot := 0
	for _, schemas := range tests {
		for _, schema := range schemas {
			schemaTot++
			if schema.Skip == "" {
				schemaOK++
			}
			for _, test := range schema.Tests {
				testTot++
				if test.Skip == "" {
					testOK++
				}
				if schema.Skip == "" {
					schemaOKTestTot++
					if test.Skip == "" {
						schemaOKTestOK++
					}
				}
			}
		}
	}
	fmt.Printf("schema extract (pass / total): %d / %d = %.1f%%\n", schemaOK, schemaTot, percent(schemaOK, schemaTot))
	fmt.Printf("tests (pass / total): %d / %d = %.1f%%\n", testOK, testTot, percent(testOK, testTot))
	fmt.Printf("tests on extracted schemas (pass / total): %d / %d = %.1f%%\n", schemaOKTestOK, schemaOKTestTot, percent(schemaOKTestOK, schemaOKTestTot))
}

func listFailures(tests map[string][]*externaltest.Schema) {
	for _, filename := range sortedKeys(tests) {
		schemas := tests[filename]
		filename = path.Join("testdata/external", filename)
		for _, schema := range schemas {
			if schema.Skip != "" {
				fmt.Printf("%s: schema fail (%s)\n", filename, schema.Description)
				continue
			}
			for _, test := range schema.Tests {
				if test.Skip != "" {
					reason := "fail"
					if !test.Valid {
						reason = "unexpected success"
					}
					fmt.Printf("%s: %s (%s; %s)\n", filename, reason, schema.Description, test.Description)
				}
			}
		}
	}
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
