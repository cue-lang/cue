// Copyright 2018 The CUE Authors
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

package errors

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"cuelang.org/go/cue/token"
)

// TODO this foundational package could do with a bunch more tests.

// countingError is a [posError] that counts how often its message is rendered
// via Error, so tests can assert that removeMultiples only renders errors it
// must compare to deduplicate. It overrides Path, which posError leaves nil.
type countingError struct {
	posError
	path    []string
	renders int
}

func (e *countingError) Path() []string { return e.path }

func (e *countingError) Error() string {
	e.renders++
	return e.posError.Error()
}

// TestRemoveMultiplesRenders records how many times removeMultiples renders
// each error in a list while sorting and deduplicating it, along with which
// errors survive and the rendered result. Rendering fully formats an error's
// message, so the counts are worth pinning: later commits that change how
// errors are deduplicated or rendered show their effect as updates here.
//
// The exact render count of an error within a group reflects how many
// comparisons it takes part in while the group is sorted and compacted by
// message, so it depends on the standard library's sort; it is deterministic
// but not otherwise significant.
func TestRemoveMultiplesRenders(t *testing.T) {
	f := token.NewFile("test", 0, 100)
	pos := func(offset int) token.Pos { return f.Pos(offset, token.NoRelPos) }
	newErr := func(p token.Pos, msg string, path ...string) *countingError {
		return &countingError{posError: posError{pos: p, Message: Message{format: msg}}, path: path}
	}

	tests := []struct {
		*countingError
		wantKept    bool // whether the error survives deduplication
		wantRenders int  // expected number of Error calls
	}{
		{newErr(pos(10), "unique position and path", "a"), true, 1},
		{newErr(pos(15), "unique with a multi-element path", "a", "b"), true, 0},
		{newErr(pos(20), "shares position 20 but has a different path", "z"), true, 0},
		{newErr(pos(20), "shares position 20 but has a longer path", "b", "x"), true, 0},
		{newErr(pos(20), "distinct at position 20 path b (first by message)", "b"), true, 1},
		{newErr(pos(20), "distinct at position 20 path b (second by message)", "b"), false, 1},
		{newErr(pos(30), "identical errors at position 30 path c", "c"), true, 1},
		{newErr(pos(30), "identical errors at position 30 path c", "c"), false, 1},
		{newErr(pos(40), "one of three distinct at position 40 path d (first)", "d"), true, 2},
		{newErr(pos(40), "one of three distinct at position 40 path d (second)", "d"), false, 2},
		{newErr(pos(40), "one of three distinct at position 40 path d (third)", "d"), false, 2},
		{newErr(token.NoPos, "invalid position, grouped regardless of path (first)"), true, 3},
		{newErr(token.NoPos, "invalid position, grouped regardless of path (second)"), true, 4},
		{newErr(token.NoPos, "invalid position, grouped regardless of path (third)"), true, 4},
	}

	l := make(list, len(tests))
	for i, tt := range tests {
		l[i] = tt.countingError
	}
	l.removeMultiples()

	for i, tt := range tests {
		msg, _ := tt.Msg()
		kept := slices.Contains(l, Error(tt.countingError))
		if kept != tt.wantKept {
			t.Errorf("error %q (#%d): kept = %v, want %v", msg, i, kept, tt.wantKept)
		}
		if tt.renders != tt.wantRenders {
			t.Errorf("error %q (#%d): rendered %d times, want %d", msg, i, tt.renders, tt.wantRenders)
		}
	}

	// The surviving errors are sorted by position, then path, then message.
	// At this point distinct errors sharing a position and path are dropped,
	// keeping only the first; only the next commit reports them all.
	want := `invalid position, grouped regardless of path (first)
invalid position, grouped regardless of path (second)
invalid position, grouped regardless of path (third)
a: unique position and path:
    test:1:11
a.b: unique with a multi-element path:
    test:1:16
b: distinct at position 20 path b (first by message):
    test:1:21
b.x: shares position 20 but has a longer path:
    test:1:21
z: shares position 20 but has a different path:
    test:1:21
c: identical errors at position 30 path c:
    test:1:31
d: one of three distinct at position 40 path d (first):
    test:1:41
`
	if got := Details(l, nil); got != want {
		t.Errorf("deduplicated errors:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintError(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		wantW string
	}{{
		name:  "SimplePromoted",
		err:   Promote(fmt.Errorf("hello"), "msg"),
		wantW: "msg: hello\n",
	}, {
		name:  "PromoteWithPercent",
		err:   Promote(fmt.Errorf("hello"), "msg%s"),
		wantW: "msg%s: hello\n",
	}, {
		name:  "PromoteWithEmptyString",
		err:   Promote(fmt.Errorf("hello"), ""),
		wantW: "hello\n",
	}, {
		name:  "TwoErrors",
		err:   Append(Promote(fmt.Errorf("hello"), "x"), Promote(fmt.Errorf("goodbye"), "y")),
		wantW: "x: hello\ny: goodbye\n",
	}, {
		name:  "WrappedSingle",
		err:   fmt.Errorf("wrap: %w", Promote(fmt.Errorf("hello"), "x")),
		wantW: "x: hello\n",
	}, {
		name: "WrappedMultiple",
		err: fmt.Errorf("wrap: %w",
			Append(Promote(fmt.Errorf("hello"), "x"), Promote(fmt.Errorf("goodbye"), "y")),
		),
		wantW: "x: hello\ny: goodbye\n",
	}}
	// TODO tests for errors with positions.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			Print(w, tt.err, nil)
			if gotW := w.String(); gotW != tt.wantW {
				t.Errorf("unexpected PrintError result\ngot %q\nwant %q", gotW, tt.wantW)
			}
		})
	}
}
