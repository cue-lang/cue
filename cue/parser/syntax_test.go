// Copyright 2026 The CUE Authors
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

package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
)

// TestSyntaxCorpus checks each file in testdata/syntax against the parser at
// the current language version. The directory holding a file gives its
// expected outcome: a file in pass must parse and enable no experiment; a
// file in preview must parse and enable one; a file in fail must fail to
// parse, with its first error at the position on the file's first line and
// containing the text there, which reads "// error: <line>:<col>: <text>".
// See testdata/syntax/README.md.
func TestSyntaxCorpus(t *testing.T) {
	root := filepath.Join("testdata", "syntax")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch name := e.Name(); {
		case e.IsDir() && (name == "pass" || name == "preview" || name == "fail"):
		case !e.IsDir() && name == "README.md":
		default:
			t.Errorf("unexpected %s in %s; corpus files belong in pass, preview or fail", name, root)
		}
	}
	for _, class := range []string{"pass", "preview", "fail"} {
		dir := filepath.Join(root, class)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Errorf("no corpus files in %s", dir)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || filepath.Ext(name) != ".cue" {
				t.Errorf("unexpected %s in %s; want only .cue files", name, dir)
				continue
			}
			t.Run(class+"/"+name, func(t *testing.T) {
				src, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				f, err := parser.ParseFile(name, src)
				if class == "fail" {
					checkFailure(t, src, err)
					return
				}
				if err != nil {
					t.Fatalf("unexpected error:\n%s", cueerrors.Details(err, nil))
				}
				preview := class == "preview"
				if enablesExperiment(f) != preview {
					if preview {
						t.Fatal("file must enable an experiment with @experiment(...)")
					}
					t.Fatal("file must not enable an experiment")
				}
			})
		}
	}
}

// errorLine matches the first line of a fail file.
var errorLine = regexp.MustCompile(`^// error: ([0-9]+):([0-9]+): (.+)$`)

// checkFailure checks that err, the result of parsing src, has its first
// error at the position stated on the first line of src, with a message
// containing the text stated there.
func checkFailure(t *testing.T, src []byte, err error) {
	t.Helper()
	first, _, _ := strings.Cut(string(src), "\n")
	m := errorLine.FindStringSubmatch(first)
	if m == nil {
		t.Fatalf("first line must be %q; got %q", "// error: <line>:<col>: <text>", first)
	}
	line, _ := strconv.Atoi(m[1])
	col, _ := strconv.Atoi(m[2])
	want := m[3]
	if err == nil {
		t.Fatalf("parsed without error; want an error at %d:%d containing %q", line, col, want)
	}
	e := cueerrors.Errors(err)[0]
	pos := e.Position()
	format, args := e.Msg()
	msg := fmt.Sprintf(format, args...)
	if pos.Line() != line || pos.Column() != col || !strings.Contains(msg, want) {
		t.Fatalf("first error is %d:%d: %s; want an error at %d:%d containing %q:\n%s",
			pos.Line(), pos.Column(), msg, line, col, want, cueerrors.Details(err, nil))
	}
}

// enablesExperiment reports whether f has an @experiment file attribute.
func enablesExperiment(f *ast.File) bool {
	for _, decl := range f.Decls {
		if a, ok := decl.(*ast.Attribute); ok {
			if name, _ := a.Split(); name == "experiment" {
				return true
			}
		}
	}
	return false
}
