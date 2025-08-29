// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cueexperiment

import (
	"testing"
)

func TestIsExperimentValid(t *testing.T) {
	tests := []struct {
		name       string
		experiment string
		version    string
		want       bool
	}{
		{
			name:       "valid experiment for current version",
			experiment: "explicitopen",
			version:    "v0.15.0",
			want:       true,
		},
		{
			name:       "experiment not available yet",
			experiment: "explicitopen",
			version:    "v0.14.0",
			want:       false,
		},
		{
			name:       "unknown experiment",
			experiment: "nonexistent",
			version:    "v0.15.0",
			want:       false,
		},
		{
			name:       "empty version (latest)",
			experiment: "explicitopen",
			version:    "",
			want:       true,
		},
		{
			name:       "case insensitive",
			experiment: "ExplicitOpen",
			version:    "v0.15.0",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsExperimentValid(tt.experiment, tt.version)
			if got != tt.want {
				t.Errorf("IsExperimentValid(%q, %q) = %v, want %v", tt.experiment, tt.version, got, tt.want)
			}
		})
	}
}

func TestIsExperimentAccepted(t *testing.T) {
	tests := []struct {
		name       string
		experiment string
		version    string
		want       bool
	}{
		{
			name:       "experiment not accepted yet",
			experiment: "explicitopen",
			version:    "v0.15.0",
			want:       false,
		},
		{
			name:       "unknown experiment",
			experiment: "nonexistent",
			version:    "v0.15.0",
			want:       false,
		},
		{
			name:       "empty version",
			experiment: "explicitopen",
			version:    "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsExperimentAccepted(tt.experiment, tt.version)
			if got != tt.want {
				t.Errorf("IsExperimentAccepted(%q, %q) = %v, want %v", tt.experiment, tt.version, got, tt.want)
			}
		})
	}
}

func TestCanApplyExperimentFix(t *testing.T) {
	tests := []struct {
		name        string
		experiment  string
		version     string
		target      string
		existingExp *File
		wantErr     string
	}{
		{
			name:        "valid experiment application",
			experiment:  "explicitopen",
			version:     "v0.15.0",
			target:      "v0.15.0",
			existingExp: &File{},
			wantErr:     "",
		},
		{
			name:        "experiment already enabled (should be silently ignored)",
			experiment:  "explicitopen",
			version:     "v0.15.0",
			target:      "v0.15.0",
			existingExp: &File{ExplicitOpen: true},
			wantErr:     "",
		},
		{
			name:        "unknown experiment",
			experiment:  "nonexistent",
			version:     "v0.15.0",
			target:      "v0.15.0",
			existingExp: &File{},
			wantErr:     "unknown experiment \"nonexistent\"",
		},
		{
			name:        "experiment too early",
			experiment:  "explicitopen",
			version:     "v0.14.0",
			target:      "v0.14.0",
			existingExp: &File{},
			wantErr:     "experiment \"explicitopen\" requires version v0.15.0 or later, got v0.14.0",
		},
		{
			name:        "experiment okay",
			experiment:  "accepted_",
			version:     "v0.13.0",
			target:      "v0.13.0",
			existingExp: &File{},
			wantErr:     ``,
		},
		{
			name:        "experiment too late",
			experiment:  "accepted_",
			version:     "v0.15.0",
			target:      "v0.15.0",
			existingExp: &File{},
			wantErr:     `experiment "accepted_" is already accepted as of version v0.15.0 - cannot apply fix`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanApplyExperimentFix(tt.experiment, tt.version, tt.target, tt.existingExp)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("CanApplyExperimentFix() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("CanApplyExperimentFix() error = nil, want error containing %q", tt.wantErr)
				} else if !containsSubstring(err.Error(), tt.wantErr) {
					t.Errorf("CanApplyExperimentFix() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestGetActiveExperiments(t *testing.T) {
	tests := []struct {
		name    string
		version string
		target  string
		want    []string
	}{
		{
			name:    "v0.16.0 experiments",
			version: "v0.16.0",
			target:  "v0.17.0",
			want:    []string{"explicitopen", "structcmp", "testing", "try"},
		},
		{
			name:    "v0.15.0 experiments",
			version: "v0.14.0",
			target:  "v0.15.0",
			want:    []string{"accepted_", "explicitopen", "structcmp", "testing", "try"},
		},
		{
			name:    "v0.16.0 experiments (excludes accepted_)",
			version: "v0.16.0",
			target:  "v0.16.0",
			want:    []string{"explicitopen", "structcmp", "testing", "try"},
		},
		{
			name:    "v0.14.0 experiments",
			version: "v0.14.0",
			target:  "v0.14.0",
			want:    []string{"accepted_", "structcmp", "testing"},
		},
		{
			name:    "v0.13.0 experiments",
			version: "v0.13.0",
			target:  "v0.13.0",
			want:    []string{"accepted_", "testing"},
		},
		{
			name:    "empty version (latest)",
			version: "",
			want:    []string{"explicitopen", "structcmp", "testing", "try"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetActiveExperiments(tt.version, tt.target)
			if !equalStringSlices(got, tt.want) {
				t.Errorf("GetActiveExperiments(%q, %q) = %v, want %v", tt.version, tt.target, got, tt.want)
			}
		})
	}
}

func TestGetUpgradeExperiments(t *testing.T) {
	tests := []struct {
		name    string
		version string
		target  string
		want    []string
	}{
		{
			name:    "not yet available",
			version: "v0.11.0",
			target:  "v0.12.0",
			want:    nil,
		},
		{
			name:    "first possible availability",
			version: "v0.12.0",
			target:  "v0.13.0",
			want:    []string{"accepted_"},
		},
		{
			name:    "first possible version for experiment",
			version: "v0.13.0",
			target:  "v0.17.0",
			want:    []string{"accepted_"},
		},
		{
			name:    "last possible version for experiment",
			version: "v0.14.0",
			target:  "v0.17.0",
			want:    []string{"accepted_"},
		},
		{
			name:    "first original with required semantics",
			version: "v0.16.0",
			target:  "v0.17.0",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetUpgradeExperiments(tt.version, tt.target)
			if !equalStringSlices(got, tt.want) {
				t.Errorf("GetUpgradeExperiments(%q, %q) = %v, want %v", tt.version, tt.target, got, tt.want)
			}
		})
	}
}

func TestShouldRemoveExperimentAttribute(t *testing.T) {
	tests := []struct {
		name       string
		experiment string
		version    string
		want       bool
	}{
		{
			name:       "experiment not accepted yet",
			experiment: "explicitopen",
			version:    "v0.15.0",
			want:       false,
		},
		{
			name:       "experiment accepted",
			experiment: "accepted_",
			version:    "v0.16.0",
			want:       true,
		},
		{
			name:       "unknown experiment",
			experiment: "nonexistent",
			version:    "v0.15.0",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRemoveExperimentAttribute(tt.experiment, tt.version)
			if got != tt.want {
				t.Errorf("ShouldRemoveExperimentAttribute(%q, %q) = %v, want %v", tt.experiment, tt.version, got, tt.want)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
