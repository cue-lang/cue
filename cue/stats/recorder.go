// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stats

import (
	"context"
	"sync"
)

// A Recorder accumulates evaluation statistics from many operations.
// It is safe for concurrent use: each evaluation gathers its counts
// locally and adds them to a recorder when it completes.
//
// The zero Recorder is ready for use.
type Recorder struct {
	// mu guards the fields below it.
	mu     sync.Mutex
	counts Counts
}

// Add merges c into the recorder's totals.
func (r *Recorder) Add(c Counts) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts.Add(c)
}

// Counts returns a snapshot of the accumulated totals.
func (r *Recorder) Counts() Counts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts
}

// Reset sets the accumulated totals back to zero.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts = Counts{}
}

type recorderKey struct{}

// WithRecorder returns a context that directs the statistics of
// operations run with it into r, overriding any recorder configured at
// a coarser scope (for example on an evaluation runtime).
func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

// RecorderFromContext returns the recorder attached to ctx with
// [WithRecorder], if any.
func RecorderFromContext(ctx context.Context) (*Recorder, bool) {
	r, ok := ctx.Value(recorderKey{}).(*Recorder)
	return r, ok
}
