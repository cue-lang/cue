// Copyright 2020 CUE Authors
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

package list_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/pkg/internal/builtintest"
	"cuelang.org/go/pkg/list"
)

func TestBuiltin(t *testing.T) {
	builtintest.Run("list", t)
}

func num(t *testing.T, s string, k adt.Kind) *adt.Num {
	d, _, err := apd.NewFromString(s)
	qt.Assert(t, qt.IsNil(err))
	return &adt.Num{X: *d, K: k}
}

// TestRangeWide starts a range at an integer wider than the 34 digits of the
// decimal context. Rounding the step away would leave the range stuck at its
// start forever.
func TestRangeWide(t *testing.T) {
	start := num(t, "10000000000000000000000000000000000000000", adt.IntKind)
	limit := num(t, "10000000000000000000000000000000000000002", adt.IntKind)
	step := num(t, "1", adt.IntKind)

	var got []*adt.Num
	var err error
	done := make(chan struct{})
	go func() {
		got, err = list.Range(start, limit, step)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Range did not return")
	}
	qt.Assert(t, qt.IsNil(err))
	var strs []string
	for _, n := range got {
		strs = append(strs, n.X.String())
	}
	qt.Assert(t, qt.Equals(fmt.Sprint(strs), "[10000000000000000000000000000000000000000 10000000000000000000000000000000000000001]"))
}

// TestRangeRoundedStep steps a float range by a step which rounds away beside
// its start, as adding 1 to 1e40 does in the 34 digits of the decimal context.
// The range cannot advance, which Range must report.
func TestRangeRoundedStep(t *testing.T) {
	start := num(t, "1e40", adt.FloatKind)
	limit := num(t, "2e40", adt.FloatKind)
	step := num(t, "1", adt.IntKind)

	var err error
	done := make(chan struct{})
	go func() {
		_, err = list.Range(start, limit, step)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Range did not return")
	}
	qt.Assert(t, qt.ErrorMatches(err, `step 1 is too small to advance from 1E\+40`))
}
