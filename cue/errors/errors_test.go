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

	"cuelang.org/go/cue/token"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		e    Error
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if got := tt.e.Error(); got != tt.want {
			t.Errorf("%q. Error.Error() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestErrorList_Add(t *testing.T) {
	type args struct {
		pos token.Pos
		msg string
	}
	tests := []struct {
		name string
		p    *list
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt.p.AddNewf(tt.args.pos, tt.args.msg)
	}
}

func TestErrorList_Reset(t *testing.T) {
	tests := []struct {
		name string
		p    *list
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt.p.Reset()
	}
}

func TestErrorList_Sort(t *testing.T) {
	tests := []struct {
		name string
		p    list
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt.p.Sort()
	}
}

func TestErrorList_RemoveMultiples(t *testing.T) {
	tests := []struct {
		name string
		p    *list
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt.p.RemoveMultiples()
	}
}

func TestErrorList_Error(t *testing.T) {
	tests := []struct {
		name string
		p    list
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if got := tt.p.Error(); got != tt.want {
			t.Errorf("%q. list.Error() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestErrorList_Err(t *testing.T) {
	tests := []struct {
		name    string
		p       list
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if err := tt.p.Err(); (err != nil) != tt.wantErr {
			t.Errorf("%q. list.Err() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
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
