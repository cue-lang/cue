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

package modfiledata

import (
	"strings"
	"testing"
)

func TestParseReplacement(t *testing.T) {
	tests := []struct {
		name      string
		oldPath   string
		replace   string
		strict    bool
		wantLocal bool   // true if expecting LocalPath to be set
		wantErr   string // substring of expected error, empty for no error
	}{
		// Valid local paths
		{
			name:      "valid local path ./",
			oldPath:   "example.com/foo@v0",
			replace:   "./local",
			wantLocal: true,
		},
		{
			name:      "valid local path ../",
			oldPath:   "example.com/foo@v0",
			replace:   "../sibling",
			wantLocal: true,
		},
		{
			name:      "valid local path ../../",
			oldPath:   "example.com/foo@v0",
			replace:   "../../deep/path",
			wantLocal: true,
		},

		// Valid remote replacements
		{
			name:      "valid remote replacement",
			oldPath:   "example.com/foo@v0",
			replace:   "example.com/bar@v0.1.0",
			wantLocal: false,
		},

		// Unix absolute paths - should be rejected
		{
			name:    "reject Unix absolute path /foo",
			oldPath: "example.com/foo@v0",
			replace: "/absolute/path",
			wantErr: "absolute path replacement",
		},
		{
			name:    "reject Unix absolute path /",
			oldPath: "example.com/foo@v0",
			replace: "/",
			wantErr: "absolute path replacement",
		},

		// Windows absolute paths - should be rejected
		{
			name:    "reject Windows absolute path C:\\",
			oldPath: "example.com/foo@v0",
			replace: "C:\\windows\\path",
			wantErr: "absolute path replacement",
		},
		{
			name:    "reject Windows absolute path C:/",
			oldPath: "example.com/foo@v0",
			replace: "C:/windows/path",
			wantErr: "absolute path replacement",
		},
		{
			name:    "reject Windows absolute path D:\\",
			oldPath: "example.com/foo@v0",
			replace: "D:\\other\\drive",
			wantErr: "absolute path replacement",
		},
		{
			name:    "reject lowercase drive letter c:\\",
			oldPath: "example.com/foo@v0",
			replace: "c:\\windows\\path",
			wantErr: "absolute path replacement",
		},

		// UNC paths - should be rejected as absolute paths
		{
			name:    "reject UNC path \\\\server",
			oldPath: "example.com/foo@v0",
			replace: "\\\\server\\share",
			wantErr: "absolute path replacement",
		},
		{
			name:    "reject UNC path //server",
			oldPath: "example.com/foo@v0",
			replace: "//server/share",
			wantErr: "absolute path replacement",
		},

		// Non-absolute paths that look like they could be
		{
			name:      "not absolute: 9:\\path (digit not letter)",
			oldPath:   "example.com/foo@v0",
			replace:   "9:\\notpath",
			wantLocal: false,                 // Will be parsed as remote (and fail version parse)
			wantErr:   "invalid replacement", // fails as invalid module@version
		},
		{
			name:      "not absolute: @:\\path (symbol not letter)",
			oldPath:   "example.com/foo@v0",
			replace:   "@:\\notpath",
			wantLocal: false,
			wantErr:   "invalid replacement",
		},

		// Strict mode
		{
			name:    "reject local path in strict mode",
			oldPath: "example.com/foo@v0",
			replace: "./local",
			strict:  true,
			wantErr: "not allowed in strict mode",
		},
		{
			name:      "allow remote in strict mode",
			oldPath:   "example.com/foo@v0",
			replace:   "example.com/bar@v0.1.0",
			strict:    true,
			wantLocal: false,
		},

		// Invalid module paths
		{
			name:    "invalid old module path",
			oldPath: "not-a-valid-module",
			replace: "./local",
			wantErr: "invalid module path",
		},

		// Invalid remote versions
		{
			name:    "invalid version in remote replacement",
			oldPath: "example.com/foo@v0",
			replace: "example.com/bar@invalid",
			wantErr: "invalid replacement",
		},
		{
			name:    "missing version in remote replacement",
			oldPath: "example.com/foo@v0",
			replace: "example.com/bar",
			wantErr: "invalid replacement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repl, err := parseReplacement(tt.oldPath, tt.replace, tt.strict)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("parseReplacement(%q, %q, %v) = %+v, want error containing %q",
						tt.oldPath, tt.replace, tt.strict, repl, tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("parseReplacement(%q, %q, %v) error = %q, want error containing %q",
						tt.oldPath, tt.replace, tt.strict, err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("parseReplacement(%q, %q, %v) error = %v, want no error",
					tt.oldPath, tt.replace, tt.strict, err)
				return
			}

			if tt.wantLocal {
				if repl.LocalPath != tt.replace {
					t.Errorf("parseReplacement(%q, %q, %v).LocalPath = %q, want %q",
						tt.oldPath, tt.replace, tt.strict, repl.LocalPath, tt.replace)
				}
				if repl.New.IsValid() {
					t.Errorf("parseReplacement(%q, %q, %v).New should be invalid for local path",
						tt.oldPath, tt.replace, tt.strict)
				}
			} else {
				if repl.LocalPath != "" {
					t.Errorf("parseReplacement(%q, %q, %v).LocalPath = %q, want empty for remote",
						tt.oldPath, tt.replace, tt.strict, repl.LocalPath)
				}
				if !repl.New.IsValid() {
					t.Errorf("parseReplacement(%q, %q, %v).New should be valid for remote",
						tt.oldPath, tt.replace, tt.strict)
				}
			}

			// Verify Old is always set correctly
			if repl.Old.Path() != tt.oldPath {
				t.Errorf("parseReplacement(%q, %q, %v).Old.Path() = %q, want %q",
					tt.oldPath, tt.replace, tt.strict, repl.Old.Path(), tt.oldPath)
			}
		})
	}
}
