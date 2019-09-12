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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/internal/cuetest"
	"github.com/rogpeppe/testscript/txtar"
)

func TestTutorial(t *testing.T) {
	t.Skip()

	err := filepath.Walk(".", func(path string, f os.FileInfo, err error) error {
		if strings.HasSuffix(path, ".md") && !strings.Contains(path, "/") {
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

	if path == "Readme.md" {
		return
	}

	archive := &txtar.Archive{}

	addFile := func(filename string, body string) {
		archive.Files = append(archive.Files, txtar.File{
			Name: filename,
			Data: []byte(strings.TrimSpace(body) + "\n\n"),
		})
	}

	var (
		baseName = path[:len(path)-len(".md")]
		txtFile  = baseName + ".txt"
	)

	defer func() {
		err = ioutil.WriteFile(txtFile, txtar.Format(archive), 0644)
	}()

	c := cuetest.NewChunker(t, b)

	c.Find("\n")
	c.Next("_", "_")
	section := c.Text()
	sub := strings.ToLower(strings.Fields(section)[0])
	sub = strings.TrimRight(sub, ",")

	c.Next("# ", "\n")
	addFile("frontmatter.toml", fmt.Sprintf(`title = %q
description = ""
`, c.Text()))

	for i := 0; c.Find("<!-- CUE editor -->"); i++ {
		if i == 0 {
			addFile("text.md", c.Text())
		}
		if !c.Next("_", "_") {
			continue
		}
		filename := strings.TrimRight(c.Text(), ":")

		if !c.Next("```", "```") {
			t.Fatalf("No body for filename %q in file %q", filename, path)
		}
		addFile(filename, c.Text())
	}

	if !c.Find("<!-- result -->") {
		return
	}

	if !c.Next("`$ ", "`") {
		t.Fatalf("No command for result section in file %q", path)
	}
	archive.Comment = []byte(c.Text())
	archive.Comment = append(archive.Comment, `
cmp stdout expect-stdout-cue

`...)

	if !c.Next("```", "```") {
		return
	}
	gold := c.Text()
	if p := strings.Index(gold, "\n"); p > 0 {
		gold = gold[p+1:]
	}

	gold = strings.TrimSpace(gold) + "\n"
	// TODO: manually adjust file type and stderr.
	addFile("expect-stdout-cue", gold)
}
