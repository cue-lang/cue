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
	"cuelang.org/go/cue/scanner"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
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

	cuetest.WriteGoldenFile(t, "spec.md", walkNode(t, doc, source))
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
			qt.Assert(t, qt.Equals(string(walkNode(t, doc, source)), test.want))
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
	if rest[0] != "vet" && strings.Contains(src, claimPrefix) {
		t.Errorf("%s: claims are only checked in vet blocks: ```%s", blockPos, info)
	}

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
		// Validating the whole block finds errors within each field,
		// even when the block fails to build.
		err = val.Validate()
		checkClaims(t, blockPos, val, file, errors.Errors(err))
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

// claimPrefix starts a line comment claiming the value of the field it follows,
// such as "// => 4", "// => OK" or "// => error: undefined field: z",
// which sets the claim apart from comments explaining the example.
// A claim on a line of its own after the last declaration
// is about the whole block.
const claimPrefix = "// =>"

// checkClaims checks that every field followed by a claim comment
// evaluates to the claimed value.
func checkClaims(t *testing.T, blockPos string, val cue.Value, file *ast.File, blockErrs []errors.Error) {
	claims := make(map[int]string)
	ast.Walk(file, func(n ast.Node) bool {
		c, ok := n.(*ast.Comment)
		if !ok {
			return true
		}
		line := c.Pos().Line()
		claim, ok := strings.CutPrefix(c.Text, claimPrefix)
		if ok {
			claims[line] = strings.TrimSpace(claim)
		}
		word, _, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(c.Text, "//")), " ")
		switch word := strings.ToLower(strings.TrimRight(word, ":;,")); {
		case strings.Contains(c.Text, "_|_") || word == "error":
			t.Errorf("%s: write the error at line %d as a %q claim", blockPos, line, claimPrefix+" error: message")
		case word == "ok" || word == "okay":
			t.Errorf("%s: write the claim at line %d as a %q claim", blockPos, line, claimPrefix+" OK")
		}
		return true
	}, nil)
	// The fields ending at each claim's line, any of which the claim may be about.
	fields := make(map[int][]cue.Path)
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
			// The outermost field ending at a claim's line is the one meant.
			line := field.End().Line()
			if _, ok := claims[line]; ok {
				fields[line] = append(fields[line], cue.MakePath(path...))
				continue
			}
			// Struct literals unified into the field declare its fields too.
			for _, x := range cuetxtar.FlattenConjunction(field.Value) {
				if lit, ok := x.(*ast.StructLit); ok {
					walk(path, lit.Elts)
				}
			}
		}
	}
	walk(nil, file.Decls)
	lastLine := file.End().Line()
	for line, claim := range claims {
		switch paths := fields[line]; {
		case len(paths) == 1:
			checkClaim(t, blockPos, paths[0], claim, val.LookupPath(paths[0]), blockErrs)
		case len(paths) > 1:
			// Sibling fields on one line, as in "a: 1, b: 2 // => 2".
			t.Errorf("%s: fields %v all end at the claim at line %d", blockPos, paths, line)
		case line > lastLine:
			checkClaim(t, blockPos, cue.Path{}, claim, val, blockErrs)
		default:
			t.Errorf("%s: no field for the claim at line %d", blockPos, line)
		}
	}
}

func checkClaim(t *testing.T, blockPos string, path cue.Path, claim string, v cue.Value, blockErrs []errors.Error) {
	// The field's own error may be an incomplete one,
	// which validating does not report.
	errs := fieldErrors(path, slices.Concat(blockErrs, errors.Errors(v.Err())))
	if claim == "OK" {
		if len(errs) > 0 {
			t.Errorf("%s: %v is claimed OK but has errors: %q", blockPos, path, errs)
		}
		return
	}
	if msg, ok := strings.CutPrefix(claim, "error:"); ok {
		msg = strings.TrimSpace(msg)
		if !slices.ContainsFunc(errs, func(err fieldError) bool {
			return strings.Contains(err.msg, msg) || strings.HasPrefix(err.String(), msg)
		}) {
			t.Errorf("%s: %v has no error containing %q: %q", blockPos, path, msg, errs)
		}
		return
	}
	want, err := parser.ParseExpr("", claim)
	if err != nil {
		t.Errorf("%s: %v: invalid claim %q: %v", blockPos, path, claim, err)
		return
	}
	if hasComment(claim) {
		t.Errorf("%s: %v: comment within claim %q", blockPos, path, claim)
	}
	// A claim of a single value is about the field's default, if it has one.
	if !cuetxtar.IsDisjunction(want) {
		v, _ = v.Default()
	}
	if err := cuetxtar.CompareExpr(want, v); err != nil {
		t.Errorf("%s: %v does not match its claim %s: %v", blockPos, path, claim, err)
	}
}

// fieldError is an error at or within a field,
// with its path relative to the field.
type fieldError struct {
	path, msg string
}

func (e fieldError) String() string {
	if e.path == "" {
		return e.msg
	}
	return e.path + ": " + e.msg
}

// fieldErrors returns the distinct errors at or within the field at path.
func fieldErrors(path cue.Path, errs []errors.Error) []fieldError {
	var prefix []string
	for _, sel := range path.Selectors() {
		prefix = append(prefix, sel.String())
	}
	var within []fieldError
	for _, err := range errs {
		errPath := err.Path()
		if len(errPath) < len(prefix) || !slices.Equal(errPath[:len(prefix)], prefix) {
			continue
		}
		e := fieldError{
			path: strings.Join(errPath[len(prefix):], "."),
			msg:  errors.StringWithConfig(err, &errors.Config{OmitPath: true}),
		}
		if !slices.Contains(within, e) {
			within = append(within, e)
		}
	}
	return within
}

// hasComment reports whether the CUE source src contains a comment.
func hasComment(src string) bool {
	var s scanner.Scanner
	s.Init(token.NewFile("", 0, len(src)), []byte(src), nil, scanner.ScanComments)
	for {
		switch _, tok, _ := s.Scan(); tok {
		case token.COMMENT:
			return true
		case token.EOF:
			return false
		}
	}
}
