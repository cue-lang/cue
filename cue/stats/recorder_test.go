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
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal"
)

func TestRecorderAdd(t *testing.T) {
	var r Recorder
	r.Add(Counts{
		EvalVersion:  internal.EvalV3,
		Unifications: 3,
		Conjuncts:    5,
	})
	r.Add(Counts{
		EvalVersion:  internal.EvalV3,
		Unifications: 2,
		Disjuncts:    7,
	})
	got := r.Counts()
	qt.Assert(t, qt.Equals(got.EvalVersion, internal.EvalV3))
	qt.Assert(t, qt.Equals(got.Unifications, int64(5)))
	qt.Assert(t, qt.Equals(got.Conjuncts, int64(5)))
	qt.Assert(t, qt.Equals(got.Disjuncts, int64(7)))
}

func TestRecorderReset(t *testing.T) {
	var r Recorder
	r.Add(Counts{
		EvalVersion:  internal.EvalV3,
		Unifications: 3,
	})
	r.Reset()
	qt.Assert(t, qt.Equals(r.Counts().Unifications, int64(0)))

	// After a reset, the next Add establishes the version anew.
	r.Add(Counts{
		EvalVersion:  internal.EvalV2,
		Unifications: 1,
	})
	qt.Assert(t, qt.Equals(r.Counts().EvalVersion, internal.EvalV2))
}

func TestRecorderConcurrent(t *testing.T) {
	var r Recorder
	const (
		numGoroutines = 8
		numAdds       = 1000
	)
	var wg sync.WaitGroup
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range numAdds {
				r.Add(Counts{
					EvalVersion:  internal.EvalV3,
					Unifications: 1,
				})
			}
		}()
	}
	wg.Wait()
	qt.Assert(t, qt.Equals(r.Counts().Unifications, int64(numGoroutines*numAdds)))
}

func TestRecorderContext(t *testing.T) {
	ctx := context.Background()
	_, ok := RecorderFromContext(ctx)
	qt.Assert(t, qt.IsFalse(ok))

	var r Recorder
	ctx = WithRecorder(ctx, &r)
	got, ok := RecorderFromContext(ctx)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(got, &r))
}
