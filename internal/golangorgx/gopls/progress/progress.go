// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The progress package defines utilities for reporting the progress
// of long-running operations using features of the LSP client
// interface such as Progress and ShowMessage.
package progress

import (
	"context"
	"math/rand"
	"strconv"

	"cuelang.org/go/internal/golangorgx/gopls/protocol"
)

// NewTracker returns a new Tracker that reports progress to the
// specified client.
func NewTracker(client protocol.Client, supportsWorkDoneProgress bool) *Tracker {
	return &Tracker{
		client:                   client,
		supportsWorkDoneProgress: supportsWorkDoneProgress,
	}
}

// A Tracker reports the progress of a long-running operation to an LSP client.
type Tracker struct {
	client                   protocol.Client
	supportsWorkDoneProgress bool
}

// Start notifies the client of work being done on the server. It uses either
// ShowMessage RPCs or $/progress messages, depending on the capabilities of
// the client. The returned WorkDone handle must be used to report work
// completion via [WorkDone.End] or [WorkDone.EndAsync].
func (t *Tracker) Start(ctx context.Context, title, message string) *WorkDone {
	ctx = context.WithoutCancel(ctx) // progress messages should not be cancelled
	wd := &WorkDone{
		client: t.client,
	}
	if !t.supportsWorkDoneProgress {
		// Just show a simple message.
		wd.client.ShowMessage(ctx, &protocol.ShowMessageParams{
			Type:    protocol.Log,
			Message: message,
		})
		return wd
	}
	token := strconv.FormatInt(rand.Int63(), 10)
	err := wd.client.WorkDoneProgressCreate(ctx, &protocol.WorkDoneProgressCreateParams{
		Token: token,
	})
	if err != nil {
		wd.err = err
		return wd
	}
	wd.token = token
	wd.client.Progress(ctx, &protocol.ProgressParams{
		Token: wd.token,
		Value: &protocol.WorkDoneProgressBegin{
			Kind:    "begin",
			Message: message,
			Title:   title,
		},
	})
	return wd
}

// WorkDone represents a unit of work that is reported to the client via the
// progress API.
type WorkDone struct {
	client protocol.Client
	// If token is nil, this workDone object uses the ShowMessage API, rather
	// than $/progress.
	token protocol.ProgressToken
	// err is set if progress reporting is broken for some reason (for example,
	// if there was an initial error creating a token).
	err error
}

// EndAsync reports a workdone completion back to the client,
// asynchronously. It spawns a new go-routine to call [WorkDone.End].
func (wd *WorkDone) EndAsync(ctx context.Context, message string) {
	go wd.End(ctx, message)
}

// End reports a workdone completion back to the client.
func (wd *WorkDone) End(ctx context.Context, message string) {
	ctx = context.WithoutCancel(ctx) // progress messages should not be cancelled
	if wd == nil {
		return
	}
	switch {
	case wd.err != nil:
		// There is a prior error.
	case wd.token == nil:
		// We're falling back to message-based reporting.
		wd.client.ShowMessage(ctx, &protocol.ShowMessageParams{
			Type:    protocol.Info,
			Message: message,
		})
	default:
		wd.client.Progress(ctx, &protocol.ProgressParams{
			Token: wd.token,
			Value: &protocol.WorkDoneProgressEnd{
				Kind:    "end",
				Message: message,
			},
		})
	}
}
