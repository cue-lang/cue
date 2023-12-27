// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package golang

import (
	"context"
	"regexp"

	"cuelang.org/go/internal/golangorgx/gopls/cache"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/gopls/util/safetoken"
)

// IsGenerated gets and reads the file denoted by uri and reports
// whether it contains a "generated file" comment as described at
// https://golang.org/s/generatedcode.
//
// TODO(adonovan): opt: this function does too much.
// Move snapshot.ReadFile into the caller (most of which have already done it).
func IsGenerated(ctx context.Context, snapshot *cache.Snapshot, uri protocol.DocumentURI) bool {
	fh, err := snapshot.ReadFile(ctx, uri)
	if err != nil {
		return false
	}
	pgf, err := snapshot.ParseGo(ctx, fh, ParseHeader)
	if err != nil {
		return false
	}
	for _, commentGroup := range pgf.File.Comments {
		for _, comment := range commentGroup.List {
			if matched := generatedRx.MatchString(comment.Text); matched {
				// Check if comment is at the beginning of the line in source.
				if safetoken.Position(pgf.Tok, comment.Slash).Column == 1 {
					return true
				}
			}
		}
	}
	return false
}

// Matches cgo generated comment as well as the proposed standard:
//
//	https://golang.org/s/generatedcode
var generatedRx = regexp.MustCompile(`// .*DO NOT EDIT\.?`)
