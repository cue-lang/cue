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
	"slices"
	"strings"
	"testing"
)

// testFile is like File, but with a fixed set of experiments for testing
// purposes
type testFile struct {
	// version is the module version of the file that was compiled.
	version     string
	experiments string

	Exp10_R14 bool `experiment:"preview:v0.10.0,withdrawn:v0.14.0"`
	Exp11_R13 bool `experiment:"preview:v0.11.0,withdrawn:v0.13.0"`

	Exp13_A15 bool `experiment:"preview:v0.13.0,stable:v0.15.0"`

	Exp14 bool `experiment:"preview:v0.14.0"`
	Exp15 bool `experiment:"preview:v0.15.0"`
}

func TestIsAvailable(t *testing.T) {
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
			got := IsAvailable(tt.experiment, tt.version)
			if got != tt.want {
				t.Errorf("IsAvailable(%q, %q) = %v, want %v", tt.experiment, tt.version, got, tt.want)
			}
		})
	}
}

func TestIsStable(t *testing.T) {
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
			got := IsStable(tt.experiment, tt.version)
			if got != tt.want {
				t.Errorf("IsStable(%q, %q) = %v, want %v", tt.experiment, tt.version, got, tt.want)
			}
		})
	}
}

func TestCanApplyFix(t *testing.T) {
	tests := []struct {
		name       string
		experiment string
		version    string
		target     string
		wantErr    string
	}{
		{
			name:       "valid experiment application",
			experiment: "exp15",
			version:    "v0.15.0",
			target:     "v0.15.0",
			wantErr:    "",
		},
		{
			name:       "experiment already enabled (should be silently ignored)",
			experiment: "exp15",
			version:    "v0.15.0",
			target:     "v0.15.0",
			wantErr:    "",
		},
		{
			name:       "unknown experiment",
			experiment: "nonexistent",
			version:    "v0.15.0",
			target:     "v0.15.0",
			wantErr:    "unknown experiment \"nonexistent\"",
		},
		{
			name:       "experiment too early",
			experiment: "exp15",
			version:    "v0.14.0",
			target:     "v0.14.0",
			wantErr:    "experiment \"exp15\" requires language version v0.15.0 or later, have v0.14.0",
		},
		{
			name:       "experiment okay",
			experiment: "exp13_a15",
			version:    "v0.13.0",
			target:     "v0.13.0",
			wantErr:    ``,
		},
		{
			name:       "experiment too late",
			experiment: "exp13_a15",
			version:    "v0.15.0",
			target:     "v0.15.0",
			wantErr:    `experiment "exp13_a15" is already stable as of language version v0.15.0 - cannot apply fix`,
		},
		{
			name:       "rejected",
			experiment: "exp11_r13",
			version:    "v0.11.0",
			target:     "v0.13.0",
			wantErr:    `experiment "exp11_r13" is withdrawn in language version v0.13.0`,
		},
		{
			name:       "allow explicit fixes that are not yet rejected",
			experiment: "exp10_r14",
			version:    "v0.9.0",
			target:     "v0.13.0",
			wantErr:    ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := canApplyExperimentFix(tt.experiment, tt.version, tt.target, testFile{})
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("CanApplyFix() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("CanApplyFix() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("CanApplyFix() error = %v, want error containing %q", err, tt.wantErr)
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
			want:    []string{"exp14", "exp15"},
		},
		{
			name:    "v0.15.0 experiments",
			version: "v0.14.0",
			target:  "v0.15.0",
			want:    []string{"exp13_a15", "exp14", "exp15"},
		},
		{
			// Do not include experiments if the source version is geq than
			// the accepted date, as this implies the new semantics is already
			// in place.
			name:    "v0.16.0 experiments (excludes exp13_a15)",
			version: "v0.16.0",
			target:  "v0.16.0",
			want:    []string{"exp14", "exp15"},
		},
		{
			name:    "v0.14.0 experiments",
			version: "v0.14.0",
			target:  "v0.14.0",
			want:    []string{"exp13_a15", "exp14"},
		},
		{
			name:    "v0.13.0 experiments",
			version: "v0.13.0",
			target:  "v0.13.0",
			want:    []string{"exp13_a15"},
		},
		{
			name:    "empty version (latest)",
			version: "",
			want:    []string{"exp14", "exp15"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getActiveExperiments(tt.version, tt.target, testFile{})
			if !slices.Equal(got, tt.want) {
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
			want:    []string{"exp13_a15"},
		},
		{
			name:    "first possible version for experiment",
			version: "v0.13.0",
			target:  "v0.17.0",
			want:    []string{"exp13_a15"},
		},
		{
			name:    "last possible version for experiment",
			version: "v0.14.0",
			target:  "v0.17.0",
			want:    []string{"exp13_a15"},
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
			got := getUpgradeExperiments(tt.version, tt.target, testFile{})
			if !slices.Equal(got, tt.want) {
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
