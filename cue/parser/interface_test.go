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

package parser

import (
	"reflect"
	"testing"

	"cuelang.org/go/cue/ast"
)

func Test_readSource(t *testing.T) {
	type args struct {
		filename string
		src      interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		got, err := readSource(tt.args.filename, tt.args.src)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q. readSource() error = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q. readSource() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseFile(t *testing.T) {
	type args struct {
		filename string
		src      interface{}
		options  []Option
	}
	tests := []struct {
		name    string
		args    args
		wantF   *ast.File
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		gotF, err := ParseFile(tt.args.filename, tt.args.src, tt.args.options...)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q. ParseFile() error = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !reflect.DeepEqual(gotF, tt.wantF) {
			t.Errorf("%q. ParseFile() = %v, want %v", tt.name, gotF, tt.wantF)
		}
	}
}

func TestParseExprFrom(t *testing.T) {
	type args struct {
		filename string
		src      interface{}
		mode     Option
	}
	tests := []struct {
		name    string
		args    args
		want    ast.Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		got, err := ParseExpr(tt.args.filename, tt.args.src, tt.args.mode)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q. ParseExprFrom() error = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q. ParseExprFrom() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestParseExprString(t *testing.T) {
	type args struct {
		x string
	}
	tests := []struct {
		name    string
		args    args
		want    ast.Expr
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		got, err := parseExprString(tt.args.x)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q. ParseExpr() error = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q. ParseExpr() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
