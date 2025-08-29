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
	"reflect"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	// Define a test struct with experiment tags
	type testFlags struct {
		Feature1 bool `experiment:"preview:v0.1.0"`
		Feature2 bool `experiment:"preview:v0.2.0,stable:v1.0.0"`
		Feature3 bool `experiment:"preview:v0.3.0,withdrawn:v0.5.0"`
		Feature4 bool `experiment:"preview:v0.1.0,stable:v0.4.0"`
	}

	tests := []struct {
		name        string
		version     string
		experiments string
		want        testFlags
		wantErr     bool
		errSubstr   string
	}{{
		name:        "empty_inputs",
		version:     "",
		experiments: "",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     false,
	}, {
		name:        "enable_feature1",
		version:     "v0.1.0",
		experiments: "feature1",
		want:        testFlags{Feature1: true},
		wantErr:     false,
	}, {
		name:        "enable_feature1_no_version",
		experiments: "feature1",
		want:        testFlags{Feature1: true, Feature2: true, Feature4: true},
		wantErr:     false,
	}, {
		name:        "enable_accepted_feature2_no_version",
		experiments: "feature2",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     false,
	}, {
		name:        "enable_rejected_feature3_no_version",
		experiments: "feature3",
		want:        testFlags{Feature1: true, Feature2: true, Feature4: true},
		wantErr:     true,
		errSubstr:   `cannot set rejected experiment "feature3"`,
	}, {
		name:        "feature_not_available_yet",
		version:     "v0.0.9",
		experiments: "feature1",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     true,
		errSubstr:   "cannot set experiment \"feature1\" before version v0.1.0",
	}, {
		name:        "rejected_feature",
		version:     "v0.5.0",
		experiments: "feature3",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     true,
		errSubstr:   "cannot set rejected experiment \"feature3\"",
	}, {
		name:        "accepted_feature_automatically_enabled",
		version:     "v1.0.0",
		experiments: "",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     false,
	}, {
		name:        "unknown_experiment",
		version:     "v1.0.0",
		experiments: "nonexistent",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     true,
		errSubstr:   "unknown experiment \"nonexistent\"",
	}, {
		name:        "multiple_experiments",
		version:     "v0.3.0",
		experiments: "feature1,feature2,feature3",
		want:        testFlags{Feature1: true, Feature2: true, Feature3: true},
		wantErr:     false,
	}, {
		name:        "case_insensitive",
		version:     "v0.3.0",
		experiments: "FEATURE1",
		want:        testFlags{Feature1: true, Feature2: true, Feature4: true},
		wantErr:     true,
		errSubstr:   `unknown experiment "FEATURE1"`,
	}, {
		name:        "experiments_with_spaces",
		version:     "v0.3.0",
		experiments: " feature1 , feature2 ",
		want:        testFlags{Feature1: true, Feature2: true},
		wantErr:     false,
	}, {
		name:        "multiple_errors",
		version:     "v0.0.9",
		experiments: "feature1,feature3,nonexistent",
		want:        testFlags{Feature2: true, Feature4: true},
		wantErr:     true,
		errSubstr:   "cannot set experiment",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got testFlags
			m := parseExperiments(tt.experiments)
			err := parseConfig(&got, tt.version, m)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseConfig() error = nil, want error containing %q", tt.errSubstr)
					return
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("parseConfig() error = %v, want error containing %q", err, tt.errSubstr)
				}
				return
			}

			if err != nil {
				t.Errorf("parseConfig() unexpected error = %v", err)
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
