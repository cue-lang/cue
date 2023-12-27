// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !go1.19
// +build !go1.19

package hooks

import "cuelang.org/go/internal/golangorgx/gopls/settings"

func updateAnalyzers(options *settings.Options) {
	options.StaticcheckSupported = false
}
