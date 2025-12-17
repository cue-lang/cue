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

package modreplace

import (
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/internal/mod/modfiledata"
	"cuelang.org/go/mod/module"
)

func TestNewLocalReplacements(t *testing.T) {
	tests := []struct {
		name         string
		replacements map[string]modfiledata.Replacement
		wantNil      bool
	}{
		{
			name:         "nil replacements",
			replacements: nil,
			wantNil:      true,
		},
		{
			name:         "empty replacements",
			replacements: map[string]modfiledata.Replacement{},
			wantNil:      true,
		},
		{
			name: "only remote replacements",
			replacements: map[string]modfiledata.Replacement{
				"example.com/foo@v0": {
					Old: module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
					New: module.MustNewVersion("example.com/bar@v0", "v0.2.0"),
				},
			},
			wantNil: true,
		},
		{
			name: "has local replacement",
			replacements: map[string]modfiledata.Replacement{
				"example.com/foo@v0": {
					Old:       module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
					LocalPath: "./local-foo",
				},
			},
			wantNil: false,
		},
		{
			name: "mixed local and remote",
			replacements: map[string]modfiledata.Replacement{
				"example.com/foo@v0": {
					Old:       module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
					LocalPath: "./local-foo",
				},
				"example.com/bar@v0": {
					Old: module.MustNewVersion("example.com/bar@v0", "v0.1.0"),
					New: module.MustNewVersion("example.com/baz@v0", "v0.2.0"),
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use an absolute path so the test doesn't fail due to non-OSRootFS
			tmpDir := t.TempDir()
			lr, err := NewLocalReplacements(module.SourceLoc{
				FS:  module.OSDirFS(tmpDir),
				Dir: ".",
			}, tt.replacements)
			if err != nil {
				t.Fatalf("NewLocalReplacements() error = %v", err)
			}
			if (lr == nil) != tt.wantNil {
				t.Errorf("NewLocalReplacements() returned nil=%v, want nil=%v", lr == nil, tt.wantNil)
			}
		})
	}
}

func TestLocalPathFor(t *testing.T) {
	tmpDir := t.TempDir()
	replacements := map[string]modfiledata.Replacement{
		"example.com/local@v0": {
			Old:       module.MustNewVersion("example.com/local@v0", "v0.1.0"),
			LocalPath: "./local-dep",
		},
		"example.com/remote@v0": {
			Old: module.MustNewVersion("example.com/remote@v0", "v0.1.0"),
			New: module.MustNewVersion("example.com/other@v0", "v0.2.0"),
		},
	}

	lr, err := NewLocalReplacements(module.SourceLoc{
		FS:  module.OSDirFS(tmpDir),
		Dir: ".",
	}, replacements)
	if err != nil {
		t.Fatalf("NewLocalReplacements() error = %v", err)
	}

	tests := []struct {
		modulePath string
		want       string
	}{
		{"example.com/local@v0", "./local-dep"},
		{"example.com/remote@v0", ""},
		{"example.com/unknown@v0", ""},
	}

	for _, tt := range tests {
		t.Run(tt.modulePath, func(t *testing.T) {
			got := lr.LocalPathFor(tt.modulePath)
			if got != tt.want {
				t.Errorf("LocalPathFor(%q) = %q, want %q", tt.modulePath, got, tt.want)
			}
		})
	}

	// Test nil receiver
	var nilLR *LocalReplacements
	if got := nilLR.LocalPathFor("example.com/foo@v0"); got != "" {
		t.Errorf("nil.LocalPathFor() = %q, want empty string", got)
	}
}

func TestResolveToAbsPath(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := t.TempDir()

	replacements := map[string]modfiledata.Replacement{
		"example.com/foo@v0": {
			Old:       module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
			LocalPath: "./local-dep",
		},
	}

	// Test with OSRootFS (using OSDirFS)
	lr, err := NewLocalReplacements(module.SourceLoc{
		FS:  module.OSDirFS(tmpDir),
		Dir: ".",
	}, replacements)
	if err != nil {
		t.Fatalf("NewLocalReplacements() error = %v", err)
	}

	absPath, err := lr.ResolveToAbsPath("./local-dep")
	if err != nil {
		t.Fatalf("ResolveToAbsPath() error = %v", err)
	}

	expected := filepath.Join(tmpDir, "local-dep")
	if absPath != expected {
		t.Errorf("ResolveToAbsPath() = %q, want %q", absPath, expected)
	}

	// Test parent directory path
	absPath, err = lr.ResolveToAbsPath("../sibling")
	if err != nil {
		t.Fatalf("ResolveToAbsPath() error = %v", err)
	}

	expected = filepath.Clean(filepath.Join(tmpDir, "..", "sibling"))
	if absPath != expected {
		t.Errorf("ResolveToAbsPath(../sibling) = %q, want %q", absPath, expected)
	}

	// Test nil receiver
	var nilLR *LocalReplacements
	_, err = nilLR.ResolveToAbsPath("./foo")
	if err == nil {
		t.Error("nil.ResolveToAbsPath() expected error, got nil")
	}
}

func TestFetchSourceLoc(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	localDepDir := filepath.Join(tmpDir, "local-dep")
	if err := os.MkdirAll(localDepDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file (not directory) for error testing
	filePath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	replacements := map[string]modfiledata.Replacement{
		"example.com/foo@v0": {
			Old:       module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
			LocalPath: "./local-dep",
		},
	}

	lr, err := NewLocalReplacements(module.SourceLoc{
		FS:  module.OSDirFS(tmpDir),
		Dir: ".",
	}, replacements)
	if err != nil {
		t.Fatalf("NewLocalReplacements() error = %v", err)
	}

	// Test successful fetch
	loc, err := lr.FetchSourceLoc("./local-dep")
	if err != nil {
		t.Fatalf("FetchSourceLoc() error = %v", err)
	}
	if loc.Dir != "." {
		t.Errorf("FetchSourceLoc().Dir = %q, want \".\"", loc.Dir)
	}

	// Test missing directory
	_, err = lr.FetchSourceLoc("./nonexistent")
	if err == nil {
		t.Error("FetchSourceLoc(nonexistent) expected error, got nil")
	}

	// Test path is file, not directory
	_, err = lr.FetchSourceLoc("./not-a-dir")
	if err == nil {
		t.Error("FetchSourceLoc(file) expected error, got nil")
	}
}

func TestFetchRequirements(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create local-dep with module.cue
	localDepDir := filepath.Join(tmpDir, "local-dep", "cue.mod")
	if err := os.MkdirAll(localDepDir, 0755); err != nil {
		t.Fatal(err)
	}
	moduleCue := `module: "example.com/dep@v0"
language: version: "v0.9.0"
deps: {
	"example.com/transitive@v0": v: "v0.1.0"
}
`
	if err := os.WriteFile(filepath.Join(localDepDir, "module.cue"), []byte(moduleCue), 0644); err != nil {
		t.Fatal(err)
	}

	// Create local-nodeps without module.cue
	localNoDepsDir := filepath.Join(tmpDir, "local-nodeps")
	if err := os.MkdirAll(localNoDepsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create local-invalid with invalid module.cue
	localInvalidDir := filepath.Join(tmpDir, "local-invalid", "cue.mod")
	if err := os.MkdirAll(localInvalidDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localInvalidDir, "module.cue"), []byte("invalid cue"), 0644); err != nil {
		t.Fatal(err)
	}

	replacements := map[string]modfiledata.Replacement{
		"example.com/foo@v0": {
			Old:       module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
			LocalPath: "./local-dep",
		},
	}

	lr, err := NewLocalReplacements(module.SourceLoc{
		FS:  module.OSDirFS(tmpDir),
		Dir: ".",
	}, replacements)
	if err != nil {
		t.Fatalf("NewLocalReplacements() error = %v", err)
	}

	// Test with dependencies
	deps, err := lr.FetchRequirements("./local-dep")
	if err != nil {
		t.Fatalf("FetchRequirements() error = %v", err)
	}
	if len(deps) != 1 {
		t.Errorf("FetchRequirements() returned %d deps, want 1", len(deps))
	}
	if len(deps) > 0 && deps[0].Path() != "example.com/transitive@v0" {
		t.Errorf("deps[0].Path() = %q, want example.com/transitive@v0", deps[0].Path())
	}

	// Test without module.cue (no deps)
	deps, err = lr.FetchRequirements("./local-nodeps")
	if err != nil {
		t.Fatalf("FetchRequirements(nodeps) error = %v", err)
	}
	if deps != nil {
		t.Errorf("FetchRequirements(nodeps) = %v, want nil", deps)
	}

	// Test with invalid module.cue
	_, err = lr.FetchRequirements("./local-invalid")
	if err == nil {
		t.Error("FetchRequirements(invalid) expected error, got nil")
	}
}

func TestNewLocalReplacementsPathResolutionError(t *testing.T) {
	replacements := map[string]modfiledata.Replacement{
		"example.com/foo@v0": {
			Old:       module.MustNewVersion("example.com/foo@v0", "v0.1.0"),
			LocalPath: "./local-dep",
		},
	}

	// Test that NewLocalReplacements returns an error when the filesystem
	// doesn't implement OSRootFS and the directory is not absolute.
	// Use os.DirFS which doesn't implement OSRootFS.
	_, err := NewLocalReplacements(module.SourceLoc{
		FS:  os.DirFS("/"),
		Dir: "relative-dir",
	}, replacements)
	if err == nil {
		t.Error("NewLocalReplacements() expected error for non-absolute path with non-OSRootFS, got nil")
	}

	// Test that it succeeds with an absolute path even without OSRootFS
	tmpDir := t.TempDir()
	lr, err := NewLocalReplacements(module.SourceLoc{
		FS:  os.DirFS("/"),
		Dir: tmpDir, // absolute path
	}, replacements)
	if err != nil {
		t.Fatalf("NewLocalReplacements() with absolute path error = %v", err)
	}
	if lr == nil {
		t.Error("NewLocalReplacements() with absolute path returned nil")
	}
}
