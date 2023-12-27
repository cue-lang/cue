// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/golangorgx/tools/event"
)

// showMessage causes the client to show a progress or error message.
//
// It reports whether it succeeded. If it fails, it writes an error to
// the server log, so most callers can safely ignore the result.
func showMessage(ctx context.Context, cli protocol.Client, typ protocol.MessageType, message string) bool {
	err := cli.ShowMessage(ctx, &protocol.ShowMessageParams{
		Type:    typ,
		Message: message,
	})
	if err != nil {
		event.Error(ctx, "client.showMessage: %v", err)
		return false
	}
	return true
}
