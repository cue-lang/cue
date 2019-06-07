// Copyright 2019 CUE Authors
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

package basics

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/internal/cuetest"
)

func TestTutorial(t *testing.T) {
	// t.Skip()

	err := filepath.Walk(".", func(path string, f os.FileInfo, err error) error {
		if strings.HasSuffix(path, ".md") {
			t.Run(path, func(t *testing.T) { simulateFile(t, path) })
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func simulateFile(t *testing.T, path string) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to open file %q: %v", path, err)
	}

	dir, err := ioutil.TempDir("", "tutbasics")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(dir)

	c := cuetest.NewChunker(t, b)

	// collect files
	for c.Find("<!-- CUE editor -->") {
		if !c.Next("_", "_") {
			continue
		}
		filename := strings.TrimRight(c.Text(), ":")

		if !c.Next("```", "```") {
			t.Fatalf("No body for filename %q in file %q", filename, path)
		}
		b := bytes.TrimSpace(c.Bytes())

		ioutil.WriteFile(filepath.Join(dir, filename), b, 0644)
	}

	if !c.Find("<!-- result -->") {
		return
	}

	if !c.Next("`$ ", "`") {
		t.Fatalf("No command for result section in file %q", path)
	}
	command := c.Text()

	if !c.Next("```", "```") {
		t.Fatalf("No body for result section in file %q", path)
	}
	gold := c.Text()
	if p := strings.Index(gold, "\n"); p > 0 {
		gold = gold[p+1:]
	}

	cuetest.Run(t, dir, command, &cuetest.Config{Golden: gold})
}
