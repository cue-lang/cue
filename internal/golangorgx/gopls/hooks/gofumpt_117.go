// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !go1.18
// +build !go1.18

package hooks

import "cuelang.org/go/internal/golangorgx/gopls/lsp/source"

func updateGofumpt(options *source.Options) {
}
