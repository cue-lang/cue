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
	"testing"
)

// TODO this foundational package could do with a bunch more tests.

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
