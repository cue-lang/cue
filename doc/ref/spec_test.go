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

package ref_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"cuelang.org/go/cue/parser"
	"github.com/yuin/goldmark"
	goldast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func TestSpecCheck(t *testing.T) {
	source, err := os.ReadFile("spec.md")
	if err != nil {
		t.Fatal(err)
	}

	md := goldmark.New()
	doc := md.Parser().Parse(text.NewReader(source))

	walkNode(t, doc, source)
}

func walkNode(t *testing.T, node goldast.Node, source []byte) {
	if fcb, ok := node.(*goldast.FencedCodeBlock); ok {
		checkBlock(t, fcb, source)
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		walkNode(t, child, source)
	}
}

func checkBlock(t *testing.T, fcb *goldast.FencedCodeBlock, source []byte) {
	if fcb.Info == nil {
		return
	}
	info := string(fcb.Info.Value(source))
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return
	}

	// Compute the markdown line number for the opening ``` line.
	mdLine := 0
	if fcb.Lines().Len() > 0 {
		startOffset := fcb.Lines().At(0).Start
		mdLine = bytes.Count(source[:startOffset], []byte("\n"))
	}
	pos := fmt.Sprintf("spec.md:%d", mdLine)

	lang := fields[0]
	if lang != "cue" {
		return // skip non-CUE code blocks
	}

	// By default, we check for valid syntax.
	// TODO: mark intent (export, eval, vet) and validate it.
	mode := "parse"
	if len(fields) > 1 {
		mode = fields[1]
	}
	switch mode {
	case "parse":
	case "rows":
		// TODO: parse and validate line by line
		return
	case "untested":
		return // intentionally not tested
	default:
		t.Errorf("%s: unknown cue code block mode: ```%s", pos, info)
		return
	}

	var buf bytes.Buffer
	for i := 0; i < fcb.Lines().Len(); i++ {
		line := fcb.Lines().At(i)
		buf.Write(line.Value(source))
	}
	src := buf.String()

	_, err := parser.ParseFile(pos, src, parser.ParseComments)
	if err != nil {
		t.Errorf("%s: %q block failed to parse:\n%s", pos, info, err)
	}
}
