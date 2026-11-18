// SPDX-License-Identifier: BSD-3-Clause
// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from golang.org/x/tools/gopls/internal/settings/default.go at
// revision c11269ccb0bf.

package settings

import "time"

const ExternalValidateCommand = "cuelsp.externalvalidate"

// ConfigSection is the section which the server requests via
// workspace/configuration.
const ConfigSection = "cuelsp"

// DefaultOptions is the options that are used for the LSP server
// independent of any externally provided configuration (LSP
// initialization, command invocation, etc.).
func DefaultOptions() *Options {
	return &Options{
		// 601ms is long enough to not distract the user
		// while typing incomplete syntax, but short enough
		// that we'll show diagnostics reasonably quickly
		// when they stop.
		DiagnosticsDelay:    601 * time.Millisecond,
		SubdirWatchPatterns: SubdirWatchPatternsAuto,
	}
}
