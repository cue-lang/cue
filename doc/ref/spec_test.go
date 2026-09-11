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
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/cuetest"
	"github.com/go-quicktest/qt"
	mdast "github.com/yuin/goldmark/v2/ast"
	mdparser "github.com/yuin/goldmark/v2/parser"
)

func TestSpecCheck(t *testing.T) {
	source, err := os.ReadFile("spec.md")
	if err != nil {
		t.Fatal(err)
	}

	doc := mdparser.New().Parse(source)

	updated := walkNode(t, doc, source)

	if bytes.Equal(source, updated) {
		return
	}
	if cuetest.UpdateGoldenFiles() {
		if err := os.WriteFile("spec.md", updated, 0o666); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Error("spec.md needs updating; run with CUE_UPDATE=1")
	}
}

// TestSpecEdits checks that the error comment following a code block
// is updated to record the error of the block, if any.
func TestSpecEdits(t *testing.T) {
	const (
		parseBlock = "```cue ! parse\nx: (\n```\n"
		parseError = "<!-- error:\nexpected operand, found 'EOF':\n    1:6\n-->\n"

		vetBlock = "```cue ! vet\nx: 1 & 2\n```\n"
		vetError = "<!-- error:\nx: conflicting values 2 and 1:\n    1:4\n    1:8\n-->\n"

		validBlock = "```cue vet\nx: 1\n```\n"
	)
	tests := []struct {
		name   string
		source string
		want   string
	}{{
		name:   "up to date",
		source: parseBlock + parseError,
		want:   parseBlock + parseError,
	}, {
		name:   "missing",
		source: parseBlock,
		want:   parseBlock + parseError,
	}, {
		name:   "longer",
		source: parseBlock + strings.Replace(parseError, "1:6", "1:6000", 1),
		want:   parseBlock + parseError,
	}, {
		name:   "same length",
		source: parseBlock + strings.Replace(parseError, "1:6", "1:9", 1),
		want:   parseBlock + parseError,
	}, {
		name:   "vet",
		source: vetBlock,
		want:   vetBlock + vetError,
	}, {
		name:   "no longer failing",
		source: validBlock + vetError,
		want:   validBlock,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			doc := mdparser.New().Parse(source)
			updated := walkNode(t, doc, source)
			// Like TestSpecCheck, only a difference from the source is an update.
			got := test.source
			if !bytes.Equal(source, updated) {
				got = string(updated)
			}
			qt.Assert(t, qt.Equals(got, test.want))
		})
	}
}

// walkNode walks the AST and returns the potentially updated source.
// Edits are collected and applied in reverse order to preserve offsets.
func walkNode(t *testing.T, doc mdast.Node, source []byte) []byte {
	type edit struct {
		offset int // position in source where comment starts or should be inserted
		oldLen int // length of existing comment (0 if none)
		newStr string
	}
	var edits []edit
	ctx := cuecontext.New()

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		fcb, ok := child.(*mdast.CodeBlock)
		if !ok || fcb.CodeBlockKind != mdast.CodeBlockKindFenced {
			continue
		}
		e, ok := checkBlock(t, ctx, fcb, source)
		if ok {
			edits = append(edits, edit(e))
		}
	}

	// Apply edits in reverse order to preserve earlier offsets.
	// Never edit the source itself, as the caller compares the two.
	result := bytes.Clone(source)
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
func closingFenceEnd(fcb *mdast.CodeBlock, source []byte) int {
	// The content lines end before the closing fence.
	// Find the closing ``` after the last content line.
	var endOfContent int
	if segs := fcb.Value.Segments(); len(segs) > 0 {
		endOfContent = segs[len(segs)-1].Stop
	} else {
		// Empty code block: closing fence follows the opening fence directly.
		// Use the start of the block.
		endOfContent = fcb.Info.Index().Stop
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

func checkBlock(t *testing.T, ctx *cue.Context, fcb *mdast.CodeBlock, source []byte) (blockEdit, bool) {
	info := string(fcb.Info.Bytes(source))
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return blockEdit{}, false
	}

	// Compute the markdown line number for the opening ``` line.
	mdLine := 0
	if segs := fcb.Value.Segments(); len(segs) > 0 {
		mdLine = bytes.Count(source[:segs[0].Start], []byte("\n"))
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
	// Every block declares what we do with it and what we expect.
	if len(rest) != 1 {
		t.Errorf("%s: want a single cue code block mode: ```%s", blockPos, info)
		return blockEdit{}, false
	}
	src := string(fcb.Value.Bytes(source))

	// Keep generated diagnostics relative to the example, not to spec.md.
	var err error
	switch mode := rest[0]; mode {
	case "parse":
		_, err = parser.ParseFile("", src, parser.ParseComments)
	case "vet":
		var file *ast.File
		file, err = parser.ParseFile("", src, parser.ParseComments)
		if err != nil {
			break
		}
		val := ctx.BuildFile(file)
		err = val.Validate()
		checkBottoms(t, blockPos, val, file)
	case "rows", "untested":
		// TODO: parse and validate rows line by line
		if wantError {
			t.Errorf("%s: %q blocks are not checked: ```%s", blockPos, mode, info)
		}
		return blockEdit{}, false
	default:
		t.Errorf("%s: unknown cue code block mode: ```%s", blockPos, info)
		return blockEdit{}, false
	}

	fenceEnd := closingFenceEnd(fcb, source)
	oldLen, _ := existingComment(source, fenceEnd)
	if !wantError {
		if err != nil {
			t.Errorf("%s: %q block failed:\n%s", blockPos, info, err)
			return blockEdit{}, false
		}
		// Drop the error comment left behind by a block which used to fail.
		return blockEdit{offset: fenceEnd, oldLen: oldLen}, oldLen > 0
	}
	if err == nil {
		t.Errorf("%s: %q block succeeded, but expected an error", blockPos, info)
		return blockEdit{}, false
	}
	// Check or update the error comment following the code block.
	details := errors.Details(err, nil)
	if strings.Contains(details, "-->") {
		t.Errorf("%s: error cannot be recorded in an HTML comment:\n%s", blockPos, details)
		return blockEdit{}, false
	}
	want := formatComment(details)
	return blockEdit{
		offset: fenceEnd,
		oldLen: oldLen,
		newStr: want,
	}, true
}

// checkBottoms checks that every field annotated with a "_|_" line comment
// evaluates to an error, including an incomplete one,
// which validating the block as a whole does not report.
func checkBottoms(t *testing.T, blockPos string, val cue.Value, file *ast.File) {
	annotated := make(map[int]bool)
	ast.Walk(file, func(n ast.Node) bool {
		if c, ok := n.(*ast.Comment); ok && strings.Contains(c.Text, "_|_") {
			annotated[c.Pos().Line()] = true
		}
		return true
	}, nil)
	var walk func(path []cue.Selector, decls []ast.Decl)
	walk = func(path []cue.Selector, decls []ast.Decl) {
		for _, decl := range decls {
			field, ok := decl.(*ast.Field)
			if !ok {
				continue
			}
			sel := cue.Label(field.Label)
			if sel.Type() == cue.InvalidSelectorType || sel.ConstraintType() == cue.PatternConstraint {
				continue
			}
			path := append(slices.Clip(path), sel)
			// The outermost field ending at an annotated line is the one meant.
			if line := field.End().Line(); annotated[line] {
				delete(annotated, line)
				path := cue.MakePath(path...)
				if v := val.LookupPath(path); v.Err() == nil && v.Validate() == nil {
					t.Errorf("%s: %v is annotated as _|_ but has no error", blockPos, path)
				}
				continue
			}
			if lit, ok := field.Value.(*ast.StructLit); ok {
				walk(path, lit.Elts)
			}
		}
	}
	walk(nil, file.Decls)
	for line := range annotated {
		t.Errorf("%s: no field for the _|_ annotation at line %d", blockPos, line)
	}
}
