// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"cuelang.org/go/internal/golangorgx/gopls/file"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

type unit = struct{}

func documentChanges(fh file.Handle, edits []protocol.TextEdit) []protocol.DocumentChanges {
	return protocol.TextEditsToDocumentChanges(fh.URI(), fh.Version(), edits)
}
