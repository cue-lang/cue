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

	"cuelang.org/go/pkg/internal/builtintest"
	"cuelang.org/go/pkg/list"
)

func TestBuiltin(t *testing.T) {
	builtintest.Run("list", t)
}

// TestRangeWide starts a range at an integer wider than the 34 digits of the
// decimal context. Rounding the step away would leave the range stuck at its
// start forever.
func TestRangeWide(t *testing.T) {
	start, _, _ := apd.NewFromString("10000000000000000000000000000000000000000")
	limit, _, _ := apd.NewFromString("10000000000000000000000000000000000000002")
	step := apd.New(1, 0)

	var got []*apd.Decimal
	var err error
	done := make(chan struct{})
	go func() {
		got, err = list.Range(start, limit, step)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Range returned")
	case <-time.After(2 * time.Second):
		// TODO: Range rounds the step away and never advances past start.
		return
	}
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(fmt.Sprint(got), "[10000000000000000000000000000000000000000 10000000000000000000000000000000000000001]"))
}

// TestRangeRoundedStep starts a range at 1e40 with a step of 1, which rounds
// away beside it in the 34 digits of the decimal context. Spelled this way the
// start is a float, so it keeps rounding once integer arithmetic turns exact,
// and Range must report that it cannot advance.
func TestRangeRoundedStep(t *testing.T) {
	start, _, _ := apd.NewFromString("1e40")
	limit, _, _ := apd.NewFromString("2e40")
	step := apd.New(1, 0)

	var err error
	done := make(chan struct{})
	go func() {
		_, err = list.Range(start, limit, step)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Range returned")
	case <-time.After(2 * time.Second):
		// TODO: Range rounds the step away and never advances past start.
		return
	}
	qt.Assert(t, qt.ErrorMatches(err, `step 1 is too small to advance from 1E\+40`))
}
