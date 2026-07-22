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

package cache

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestExtractLineEnding(t *testing.T) {
	testCases := []struct {
		content string
		want    string
	}{
		{"", "\n"},
		{"foo", "\n"},
		{"foo\nbar", "\n"},
		{"foo\r\nbar", "\r\n"},
		// The first line consisting only of its line terminator:
		{"\nbar", "\n"},
		// This shows bad behaviour: a CRLF file whose first line is
		// empty is detected as using LF endings.
		{"\r\nbar", "\n"},
	}

	for _, tc := range testCases {
		// Compute the 0-based line start offsets, mirroring
		// token.File.Lines.
		lineStartOffsets := []int{0}
		for i, c := range []byte(tc.content) {
			if c == '\n' {
				lineStartOffsets = append(lineStartOffsets, i+1)
			}
		}
		got := extractLineEnding([]byte(tc.content), lineStartOffsets)
		qt.Check(t, qt.Equals(got, tc.want), qt.Commentf("content %q", tc.content))
	}
}
