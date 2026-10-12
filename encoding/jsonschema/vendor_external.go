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
	stdjson "encoding/json"
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
	"slices"
	"strings"

	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/sync/errgroup"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema/internal/externaltest"
)

const (
	testRepo = "https://github.com/json-schema-org/JSON-Schema-Test-Suite.git"
	testDir  = "testdata/external"

	// stampFile records which upstream commit the vendored data came from,
	// letting repeated runs finish without touching the network. See [stamp].
	stampFile = testDir + "/vendored.txt"
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
	// The upstream commit is pinned, so an earlier run at the same commit has
	// already produced exactly what we would produce again. Detect that via the
	// stamp file and stop, as the work below costs a network round trip plus a
	// rewrite of every vendored file.
	if upToDate(commit) {
		log.Printf("already vendored at commit %s", commit)
		return nil
	}

	// Reading the old test data and fetching a zip file for the upstream data can be done in parallel.
	// This is useful as each operation takes hundreds of milliseconds.
	g := new(errgroup.Group)
	var oldTests map[string][]*externaltest.Schema
	g.Go(func() error {
		log.Printf("reading old test data")
		var err error
		oldTests, err = externaltest.ReadTestDir(testDir)
		if err != nil && !errors.Is(err, externaltest.ErrNotFound) {
			return err
		}
		return normalizeTests(oldTests)
	})
	var fsys fs.FS
	g.Go(func() error {
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
		zipr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
		if err != nil {
			return err
		}
		// Note that GitHub produces archives with a top-level directory representing
		// the name of the repository and the version which was retrieved.
		fsys, err = fs.Sub(zipr, fmt.Sprintf("JSON-Schema-Test-Suite-%s/tests", commit))
		return err
	})
	if err := g.Wait(); err != nil {
		return err
	}

	log.Printf("copying files to %s", testDir)
	testSubdir := filepath.Join(testDir, "tests")
	if err := os.RemoveAll(testSubdir); err != nil {
		return err
	}
	if err := fs.WalkDir(fsys, ".", func(filename string, d fs.DirEntry, err error) error {
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
	}); err != nil {
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
	if err := normalizeTests(newTests); err != nil {
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
	stampData, err := stamp(commit)
	if err != nil {
		return err
	}
	if err := os.WriteFile(stampFile, stampData, 0o666); err != nil {
		return err
	}
	log.Printf("finished")
	return nil
}

// normalizeTests rewrites the JSON in the canonical form in which CUE encodes it,
// so that the old and new tests can be matched by their JSON,
// and re-vendoring the same tests leaves them unchanged.
func normalizeTests(tests map[string][]*externaltest.Schema) error {
	ctx := cuecontext.New()
	var err error
	for filename, schemas := range tests {
		for _, schema := range schemas {
			if schema.Schema, err = normalizeJSON(ctx, schema.Schema); err != nil {
				return fmt.Errorf("%s: %v", filename, err)
			}
			for _, test := range schema.Tests {
				if test.Data, err = normalizeJSON(ctx, test.Data); err != nil {
					return fmt.Errorf("%s: %v", filename, err)
				}
			}
		}
	}
	return nil
}

func normalizeJSON(ctx *cue.Context, data stdjson.RawMessage) (stdjson.RawMessage, error) {
	expr, err := json.Extract("", data)
	if err != nil {
		return nil, err
	}
	data, err = ctx.BuildExpr(expr).MarshalJSON()
	if err != nil {
		return nil, err
	}
	// See https://github.com/cue-lang/cue/issues/3398
	return bytes.ReplaceAll(data, []byte("\ufeff"), []byte(`\ufeff`)), nil
}

// upToDate reports whether [stampFile] already describes the vendored test
// data for the given commit. Any failure to tell means vendoring afresh.
func upToDate(commit string) bool {
	got, err := os.ReadFile(stampFile)
	if err != nil {
		return false
	}
	want, err := stamp(commit)
	if err != nil {
		return false
	}
	return bytes.Equal(got, want)
}

// stamp returns the contents that [stampFile] should have for the given commit
// and the test data currently on disk. The hash covers the upstream fields we
// model, with our own skip annotations stripped, so that recording skips via
// CUE_UPDATE=1 does not make the vendored data look stale.
func stamp(commit string) ([]byte, error) {
	testsDir := filepath.Join(testDir, "tests")
	files, err := dirhash.DirFiles(testsDir, "")
	if err != nil {
		return nil, err
	}
	files = slices.DeleteFunc(files, func(name string) bool {
		return !strings.HasSuffix(name, ".json")
	})
	hash, err := dirhash.Hash1(files, func(name string) (io.ReadCloser, error) {
		data, err := os.ReadFile(filepath.Join(testsDir, name))
		if err != nil {
			return nil, err
		}
		var schemas []*externaltest.Schema
		if err := stdjson.Unmarshal(data, &schemas); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		for _, schema := range schemas {
			schema.Skip = nil
			for _, test := range schema.Tests {
				test.Skip = nil
			}
		}
		stripped, err := stdjson.Marshal(schemas)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(stripped)), nil
	})
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, `# Written by vendor_external.go; DO NOT EDIT.
commit %s
upstream-hash %s
`, commit, hash), nil
}

type skipKey struct {
	filename string
	schema   string
	test     string
}
