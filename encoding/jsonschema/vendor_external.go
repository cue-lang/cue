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

// This command copies external JSON Schema tests into the local
// repository. It tries to maintain existing test-skip information
// to avoid unintentional regressions.

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
)

const (
	testRepo = "https://github.com/json-schema-org/JSON-Schema-Test-Suite.git"
	testDir  = "testdata/external"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: vendor-external commit\n")
		os.Exit(2)
	}
	log.SetFlags(log.Lshortfile | log.Ltime | log.Lmicroseconds)
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}
	if err := doVendor(flag.Arg(0)); err != nil {
		log.Fatal(err)
	}
}

func doVendor(commit string) error {
	// Fetch a commit from GitHub via their archive ZIP endpoint, which is a lot faster
	// than git cloning just to retrieve a single commit's files.
	// See: https://docs.github.com/en/rest/repos/contents?apiVersion=2022-11-28#download-a-repository-archive-zip
	zipURL := fmt.Sprintf("https://github.com/json-schema-org/JSON-Schema-Test-Suite/archive/%s.zip", commit)
	log.Printf("fetching %s", zipURL)
	resp, err := http.Get(zipURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	log.Printf("reading old test data")
	oldTests, err := externaltest.ReadTestDir(testDir)
	if err != nil && !errors.Is(err, externaltest.ErrNotFound) {
		return err
	}

	log.Printf("copying files to %s", testDir)
	testSubdir := filepath.Join(testDir, "tests")
	if err := os.RemoveAll(testSubdir); err != nil {
		return err
	}
	zipr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return err
	}
	// Note that GitHub produces archives with a top-level directory representing
	// the name of the repository and the version which was retrieved.
	fsys, err := fs.Sub(zipr, fmt.Sprintf("JSON-Schema-Test-Suite-%s/tests", commit))
	if err != nil {
		return err
	}
	err = fs.WalkDir(fsys, ".", func(filename string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Exclude draft-next (too new) and draft3 (too old).
		if d.IsDir() && (filename == "draft-next" || filename == "draft3") {
			return fs.SkipDir
		}
		// Exclude symlinks and directories
		if !d.Type().IsRegular() {
			return nil
		}
		if !strings.HasSuffix(filename, ".json") {
			return nil
		}
		if err := os.MkdirAll(filepath.Join(testSubdir, path.Dir(filename)), 0o777); err != nil {
			return err
		}
		data, err := fs.ReadFile(fsys, filename)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(testSubdir, filename), data, 0o666); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Read the test data back that we've just written and attempt
	// to populate skip data from the original test data.
	// As indexes are not necessarily stable (new test cases
	// might be inserted in the middle of an array), we try
	// first to look up the skip info by JSON data, and then
	// by test description.
	byJSON := make(map[skipKey]externaltest.Skip)
	byDescription := make(map[skipKey]externaltest.Skip)

	for filename, schemas := range oldTests {
		for _, schema := range schemas {
			byJSON[skipKey{filename, string(schema.Schema), ""}] = schema.Skip
			byDescription[skipKey{filename, schema.Description, ""}] = schema.Skip
			for _, test := range schema.Tests {
				byJSON[skipKey{filename, string(schema.Schema), string(test.Data)}] = test.Skip
				byDescription[skipKey{filename, schema.Description, test.Description}] = schema.Skip
			}
		}
	}

	newTests, err := externaltest.ReadTestDir(testDir)
	if err != nil {
		return err
	}

	for filename, schemas := range newTests {
		for _, schema := range schemas {
			skip, ok := byJSON[skipKey{filename, string(schema.Schema), ""}]
			if !ok {
				skip, _ = byDescription[skipKey{filename, schema.Description, ""}]
			}
			schema.Skip = skip
			for _, test := range schema.Tests {
				skip, ok := byJSON[skipKey{filename, string(schema.Schema), string(test.Data)}]
				if !ok {
					skip, _ = byDescription[skipKey{filename, schema.Description, test.Description}]
				}
				test.Skip = skip
			}
		}
	}
	if err := externaltest.WriteTestDir(testDir, newTests); err != nil {
		return err
	}
	log.Printf("finished")
	return nil
}

type skipKey struct {
	filename string
	schema   string
	test     string
}
