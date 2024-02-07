// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.19
// +build go1.19

package golang

// Starting with go1.19, the formatting of comments has changed, and there
// is a new package (go/doc/comment) for processing them.
// As long as gopls has to compile under earlier versions, tests
// have to pass with both the old and new code, which produce
// slightly different results.

// When gopls no longer needs to compile with go1.18, the old comment.go should
// be replaced by this file, the golden test files should be updated.
// (and checkSameMarkdown() could be replaced by a simple comparison.)

import (
	"fmt"
	"go/doc/comment"

	"cuelang.org/go/internal/golangorgx/gopls/settings"
)

// CommentToMarkdown converts comment text to formatted markdown.
// The comment was prepared by DocReader,
// so it is known not to have leading, trailing blank lines
// nor to have trailing spaces at the end of lines.
// The comment markers have already been removed.
func CommentToMarkdown(text string, options *settings.Options) string {
	var p comment.Parser
	doc := p.Parse(text)
	var pr comment.Printer
	// The default produces {#Hdr-...} tags for headings.
	// vscode displays thems, which is undesirable.
	// The godoc for comment.Printer says the tags
	// avoid a security problem.
	pr.HeadingID = func(*comment.Heading) string { return "" }
	pr.DocLinkURL = func(link *comment.DocLink) string {
		msg := fmt.Sprintf("https://%s/%s", options.LinkTarget, link.ImportPath)
		if link.Name != "" {
			msg += "#"
			if link.Recv != "" {
				msg += link.Recv + "."
			}
			msg += link.Name
		}
		return msg
	}
	easy := pr.Markdown(doc)
	return string(easy)
}
