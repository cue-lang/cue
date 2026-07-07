// Copyright 2023 CUE Authors
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

package adt

import (
	"sync"

	"cuelang.org/go/cue/stats"
)

// This file contains stats and profiling functionality.

var (
	// counts is a temporary and internal solution for collecting global stats. It is protected with a mutex.
	counts   stats.Counts
	countsMu sync.Mutex
)

// ResetStats sets the global stats counters to zero.
func ResetStats() {
	countsMu.Lock()
	counts = stats.Counts{}
	countsMu.Unlock()
}

// AddStats adds the stats of the given OpContext to the global
// counters.
func AddStats(ctx *OpContext) {
	countsMu.Lock()
	counts.Add(ctx.stats)
	countsMu.Unlock()
}

// TotalStats returns the aggregate counts of all operations
// calling AddStats.
func TotalStats() stats.Counts {
	countsMu.Lock()
	// Shallow copy suffices as it only contains counter fields.
	s := counts
	countsMu.Unlock()
	return s
}

// FlushStats adds the statistics accumulated by c to its configured
// [OpContext.StatsRecorder], if any, and resets them. It is intended to
// be called exactly once at the end of each operation that created c;
// calling it again is harmless. Statistics accumulated by contexts
// without a recorder are dropped.
func (c *OpContext) FlushStats() {
	if c.StatsRecorder == nil {
		return
	}
	c.StatsRecorder.Add(c.stats)
	c.stats = stats.Counts{EvalVersion: c.stats.EvalVersion}
}
