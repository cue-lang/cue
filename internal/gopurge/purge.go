// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gopurge blanks out the function bodies of Go source, for
// tools which read only its declarations.
//
// Derived from golang.org/x/tools/internal/astutil/purge.go at revision
// 86210da64f15f98d41324ae4dd5ea1f8be5f92eb, where PurgeFuncBodies is not
// exported. Rather than deleting the contents of each region, this copy
// blanks them out while keeping newlines, so that positions are
// unchanged.
package gopurge

import (
	"bytes"
	"go/scanner"
	"go/token"
)

// FuncBodies returns a copy of src in which the contents of each
// outermost {...} region have been blanked out, except for struct and
// interface type bodies and the bodies of length-elided array
// literals ([...]T), whose element count is part of the type. It
// includes function bodies, function-literal bodies, and the bodies
// of slice, map, and explicitly-sized array composite literals (whose
// contents don't affect the type of the enclosing declaration). This
// reduces the amount of work required to parse the top-level
// declarations.
//
// If the input is invalid, parsing the output of FuncBodies may
// result in a different tree due to its effects on parser error recovery.
func FuncBodies(src []byte) []byte {
	// Destroy the content of any {...}-bracketed regions that are
	// not immediately preceded by a "struct" or "interface" token,
	// and that are not the body of a length-elided array literal.
	// That includes function bodies, switch/select bodies, and most
	// composite literals; this will lead to non-void functions that
	// don't have return statements, which of course is a type error,
	// but that's ok.

	out := bytes.Clone(src)
	file := token.NewFileSet().AddFile("", -1, len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, 0)
	var prev token.Token
	var braces []token.Pos // stack of unclosed braces, or -1 for a region we preserve
	var ellipsis bool      // saw "[...]" not yet consumed by a literal-body "{"
	for {
		pos, tok, _ := sc.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.COMMENT:
			// TODO(adonovan): opt: skip, to save an estimated 20% of time.

		case token.SEMICOLON:
			ellipsis = false

		case token.RBRACK:
			// "...]" occurs only in the array-type prefix of a
			// composite literal; variadic "..." is followed by
			// a type or ")", never "]".
			if prev == token.ELLIPSIS {
				ellipsis = true
			}

		case token.LBRACE:
			if prev == token.STRUCT || prev == token.INTERFACE {
				pos = -1 // type body: preserve (don't consume ellipsis)
			} else if ellipsis {
				pos = -1 // [...]T literal body: preserve
				ellipsis = false
			}
			braces = append(braces, pos)

		case token.RBRACE:
			if last := len(braces) - 1; last >= 0 {
				top := braces[last]
				braces = braces[:last]
				if top < 0 {
					// preserve
				} else if len(braces) == 0 { // toplevel only
					// Blank out {...} body.
					start := file.Offset(top)
					end := file.Offset(pos)
					for i := start + len("{"); i < end; i++ {
						if out[i] != '\n' {
							out[i] = ' '
						}
					}
				}
			}
		}
		prev = tok
	}
	return out
}
