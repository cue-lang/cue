// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from
// golang.org/x/tools/gopls/internal/test/integration/fake/edit.go at
// revision c11269ccb0bf.

package fake

import (
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// NewEdit creates an edit replacing all content between the 0-based
// (startLine, startColumn) and (endLine, endColumn) with text.
//
// Columns measure UTF-16 codes.
func NewEdit(startLine, startColumn, endLine, endColumn uint32, text string) protocol.TextEdit {
	return protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: startLine, Character: startColumn},
			End:   protocol.Position{Line: endLine, Character: endColumn},
		},
		NewText: text,
	}
}
