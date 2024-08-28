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

package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

const (
	testRepo = "git@github.com:json-schema-org/JSON-Schema-Test-Suite"
	testDir  = "testdata/external/tests"
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
	logf("copying files to %s", testDir)
	if err := os.RemoveAll(testDir); err != nil {
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
		if err := os.MkdirAll(filepath.Join(testDir, path.Dir(filename)), 0o777); err != nil {
			return err
		}
		data, err := fs.ReadFile(fsys, filename)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(testDir, filename), data, 0o666); err != nil {
			return err
		}
		return nil
	})
	return err
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
