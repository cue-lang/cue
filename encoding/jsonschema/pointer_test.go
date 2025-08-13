// Copyright 2025 CUE Authors
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

package jsonschema

import (
	"slices"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestJSONPointerFromTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{
			name:   "empty",
			tokens: []string{},
			want:   "",
		},
		{
			name:   "single_simple_token",
			tokens: []string{"foo"},
			want:   "/foo",
		},
		{
			name:   "multiple_simple_tokens",
			tokens: []string{"foo", "bar", "baz"},
			want:   "/foo/bar/baz",
		},
		{
			name:   "tokens_with_slash",
			tokens: []string{"foo/bar", "baz"},
			want:   "/foo~1bar/baz",
		},
		{
			name:   "tokens_with_tilde",
			tokens: []string{"foo~bar", "baz"},
			want:   "/foo~0bar/baz",
		},
		{
			name:   "tokens_with_both_slash_and_tilde",
			tokens: []string{"foo~/bar", "baz~test/more"},
			want:   "/foo~0~1bar/baz~0test~1more",
		},
		{
			name:   "empty_token",
			tokens: []string{"foo", "", "bar"},
			want:   "/foo//bar",
		},
		{
			name:   "numeric_tokens",
			tokens: []string{"0", "123", "foo"},
			want:   "/0/123/foo",
		},
		{
			name:   "special_chars",
			tokens: []string{"foo bar", "test\tmore", "with\nnewline"},
			want:   "/foo bar/test\tmore/with\nnewline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JSONPointerFromTokens(slices.Values(tt.tokens))
			qt.Check(t, qt.Equals(got, tt.want))
		})
	}
}

func TestJSONPointerTokens(t *testing.T) {
	tests := []struct {
		name    string
		pointer string
		want    []string
	}{
		{
			name:    "empty",
			pointer: "",
			want:    nil,
		},
		{
			name:    "root",
			pointer: "/",
			want:    []string{""},
		},
		{
			name:    "single_token",
			pointer: "/foo",
			want:    []string{"foo"},
		},
		{
			name:    "multiple_tokens",
			pointer: "/foo/bar/baz",
			want:    []string{"foo", "bar", "baz"},
		},
		{
			name:    "escaped_slash",
			pointer: "/foo~1bar/baz",
			want:    []string{"foo/bar", "baz"},
		},
		{
			name:    "escaped_tilde",
			pointer: "/foo~0bar/baz",
			want:    []string{"foo~bar", "baz"},
		},
		{
			name:    "both_escapes",
			pointer: "/foo~0~1bar/baz~0test~1more",
			want:    []string{"foo~/bar", "baz~test/more"},
		},
		{
			name:    "empty_tokens",
			pointer: "/foo//bar",
			want:    []string{"foo", "", "bar"},
		},
		{
			name:    "numeric_tokens",
			pointer: "/0/123/foo",
			want:    []string{"0", "123", "foo"},
		},
		{
			name:    "special_chars",
			pointer: "/foo bar/test\tmore/with\nnewline",
			want:    []string{"foo bar", "test\tmore", "with\nnewline"},
		},
		{
			name:    "no_leading_slash",
			pointer: "foo/bar",
			want:    []string{"foo", "bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Collect(JSONPointerTokens(tt.pointer))
			qt.Check(t, qt.DeepEquals(got, tt.want))
		})
	}
}

func TestJSONPointerRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
	}{
		{"simple", []string{"foo", "bar", "baz"}},
		{"with_slashes", []string{"foo/bar", "baz/qux"}},
		{"with_tildes", []string{"foo~bar", "baz~qux"}},
		{"with_both", []string{"foo~/bar", "baz~qux/more"}},
		{"empty_tokens", []string{"foo", "", "bar"}},
		{"numeric", []string{"0", "123", "456"}},
		{"special_chars", []string{"foo bar", "test\tmore", "with\nnewline"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pointer := JSONPointerFromTokens(slices.Values(tt.tokens))
			roundTrip := slices.Collect(JSONPointerTokens(pointer))
			qt.Check(t, qt.DeepEquals(roundTrip, tt.tokens))
		})
	}
}
