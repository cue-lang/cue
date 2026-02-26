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

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
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

	updated := walkNode(t, doc, source)

	if bytes.Equal(source, updated) {
		return
	}
	if cuetest.UpdateGoldenFiles {
		if err := os.WriteFile("spec.md", updated, 0o666); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Error("spec.md needs updating; run with CUE_UPDATE=1")
	}
}

// walkNode walks the AST and returns the potentially updated source.
// Edits are collected and applied in reverse order to preserve offsets.
func walkNode(t *testing.T, doc goldast.Node, source []byte) []byte {
	type edit struct {
		offset int // position in source where comment starts or should be inserted
		oldLen int // length of existing comment (0 if none)
		newStr string
	}
	var edits []edit

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		fcb, ok := child.(*goldast.FencedCodeBlock)
		if !ok {
			continue
		}
		e, ok := checkBlock(t, fcb, source)
		if ok {
			edits = append(edits, edit(e))
		}
	}

	// Apply edits in reverse order to preserve earlier offsets.
	result := source
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		result = append(result[:e.offset],
			append([]byte(e.newStr), result[e.offset+e.oldLen:]...)...)
	}
	return result
}

type blockEdit struct {
	offset int
	oldLen int
	newStr string
}

// closingFenceEnd returns the byte offset right after the closing fence line (including its newline).
func closingFenceEnd(fcb *goldast.FencedCodeBlock, source []byte) int {
	// The content lines end before the closing fence.
	// Find the closing ``` after the last content line.
	var endOfContent int
	if n := fcb.Lines().Len(); n > 0 {
		last := fcb.Lines().At(n - 1)
		endOfContent = last.Stop
	} else {
		// Empty code block: closing fence follows the opening fence directly.
		// Use the start of the block.
		if fcb.Info != nil {
			endOfContent = fcb.Info.Segment.Stop
		}
	}
	// Scan forward past the closing ``` line.
	idx := bytes.Index(source[endOfContent:], []byte("```"))
	if idx < 0 {
		return len(source)
	}
	end := endOfContent + idx + 3
	// Skip past the newline after ```.
	if end < len(source) && source[end] == '\n' {
		end++
	}
	return end
}

const commentPrefix = "<!-- error:"

// existingComment checks if an HTML comment with error info exists
// right after the code block's closing fence. Returns the comment length and content.
func existingComment(source []byte, offset int) (length int, content string) {
	rest := source[offset:]
	if !bytes.HasPrefix(rest, []byte(commentPrefix)) {
		return 0, ""
	}
	endIdx := bytes.Index(rest, []byte("-->"))
	if endIdx < 0 {
		return 0, ""
	}
	end := endIdx + 3
	// Gobble the newline after --> if present.
	if end < len(rest) && rest[end] == '\n' {
		end++
	}
	return end, string(rest[:end])
}

func formatComment(errStr string) string {
	return commentPrefix + "\n" + errStr + "-->\n"
}

func checkBlock(t *testing.T, fcb *goldast.FencedCodeBlock, source []byte) (blockEdit, bool) {
	if fcb.Info == nil {
		return blockEdit{}, false
	}
	info := string(fcb.Info.Value(source))
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return blockEdit{}, false
	}

	// Compute the markdown line number for the opening ``` line.
	mdLine := 0
	if fcb.Lines().Len() > 0 {
		startOffset := fcb.Lines().At(0).Start
		mdLine = bytes.Count(source[:startOffset], []byte("\n"))
	}
	blockPos := fmt.Sprintf("spec.md:%d", mdLine)

	lang := fields[0]
	if lang != "cue" {
		return blockEdit{}, false
	}

	// A "!" before the mode negates it, expecting failure.
	rest := fields[1:]
	wantError := len(rest) > 0 && rest[0] == "!"
	if wantError {
		rest = rest[1:]
	}
	// By default, we check for valid syntax.
	// TODO: mark intent (export, eval, vet) and validate it.
	mode := "parse"
	if len(rest) > 0 {
		mode = rest[0]
	}
	switch mode {
	case "parse":
	case "rows":
		// TODO: parse and validate line by line
		return blockEdit{}, false
	case "untested":
		return blockEdit{}, false
	default:
		t.Errorf("%s: unknown cue code block mode: ```%s", blockPos, info)
		return blockEdit{}, false
	}

	var buf bytes.Buffer
	// Prepend newlines so the parser reports correct markdown line numbers.
	for range mdLine {
		buf.WriteByte('\n')
	}
	for i := 0; i < fcb.Lines().Len(); i++ {
		line := fcb.Lines().At(i)
		buf.Write(line.Value(source))
	}
	src := buf.String()

	_, err := parser.ParseFile("spec.md", src, parser.ParseComments)

	if !wantError {
		if err != nil {
			t.Errorf("%s: %q block failed to parse:\n%s", blockPos, info, err)
		}
		return blockEdit{}, false
	}
	if err == nil {
		t.Errorf("%s: %q block parsed successfully, but expected an error", blockPos, info)
		return blockEdit{}, false
	}
	// Check or update the error comment following the code block.
	fenceEnd := closingFenceEnd(fcb, source)
	oldLen, _ := existingComment(source, fenceEnd)
	want := formatComment(errors.Details(err, nil))
	return blockEdit{
		offset: fenceEnd,
		oldLen: oldLen,
		newStr: want,
	}, true
}
