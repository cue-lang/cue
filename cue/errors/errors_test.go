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
		p    *List
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
		p    *List
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt.p.Reset()
	}
}

func TestErrorList_Len(t *testing.T) {
	tests := []struct {
		name string
		p    List
		want int
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if got := tt.p.Len(); got != tt.want {
			t.Errorf("%q. List.Len() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestErrorList_Swap(t *testing.T) {
	type args struct {
		i int
		j int
	}
	tests := []struct {
		name string
		p    List
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		tt.p.Swap(tt.args.i, tt.args.j)
	}
}

func TestErrorList_Less(t *testing.T) {
	type args struct {
		i int
		j int
	}
	tests := []struct {
		name string
		p    List
		args args
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if got := tt.p.Less(tt.args.i, tt.args.j); got != tt.want {
			t.Errorf("%q. List.Less() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestErrorList_Sort(t *testing.T) {
	tests := []struct {
		name string
		p    List
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
		p    *List
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
		p    List
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if got := tt.p.Error(); got != tt.want {
			t.Errorf("%q. List.Error() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestErrorList_Err(t *testing.T) {
	tests := []struct {
		name    string
		p       List
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		if err := tt.p.Err(); (err != nil) != tt.wantErr {
			t.Errorf("%q. List.Err() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestPrintError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name  string
		args  args
		wantW string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		w := &bytes.Buffer{}
		Print(w, tt.args.err, nil)
		if gotW := w.String(); gotW != tt.wantW {
			t.Errorf("%q. PrintError() = %v, want %v", tt.name, gotW, tt.wantW)
		}
	}
}
