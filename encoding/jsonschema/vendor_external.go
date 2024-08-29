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
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
)

const (
	testRepo = "git@github.com:json-schema-org/JSON-Schema-Test-Suite"
	testDir  = "testdata/external"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: vendor-external commit\n")
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}
	if err := doVendor(flag.Arg(0)); err != nil {
		log.Fatal(err)
	}
}

func doVendor(commit string) error {
	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	logf("cloning %s", testRepo)
	if err := runCmd(tmpDir, "git", "clone", "-q", testRepo, "."); err != nil {
		return err
	}
	logf("checking out commit %s", commit)
	if err := runCmd(tmpDir, "git", "checkout", "-q", commit); err != nil {
		return err
	}
	logf("reading old test data")
	oldTests, err := externaltest.ReadTestDir(testDir)
	if err != nil && !errors.Is(err, externaltest.ErrNotFound) {
		return err
	}
	logf("copying files to %s", testDir)

	testSubdir := filepath.Join(testDir, "tests")
	if err := os.RemoveAll(testSubdir); err != nil {
		return err
	}
	fsys := os.DirFS(filepath.Join(tmpDir, "tests"))
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

	// Read the test data back that we've just written and attempt
	// to populate skip data from the original test data.
	// As indexes are not necessarily stable (new test cases
	// might be inserted in the middle of an array), we try
	// first to look up the skip info by JSON data, and then
	// by test description.
	byJSON := make(map[skipKey]string)
	byDescription := make(map[skipKey]string)

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
	return err
}

type skipKey struct {
	filename string
	schema   string
	test     string
}

func runCmd(dir string, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func logf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(f, a...))
}
