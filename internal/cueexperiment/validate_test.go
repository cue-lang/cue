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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cuelang.org/go/internal/mod/semver"
)

// TestExperimentVersionOrdering validates that all experiment lifecycle versions
// follow logical ordering constraints: preview <= default <= stable,
// preview <= withdrawn, default <= withdrawn
func TestExperimentVersionOrdering(t *testing.T) {
	// Test both global experiments (Config) and file experiments (File)
	testTypes := []struct {
		name     string
		typeSpec any
	}{
		{"Config", Config{}},
		{"File", File{}},
	}

	for _, tt := range testTypes {
		t.Run(tt.name, func(t *testing.T) {
			ft := reflect.TypeOf(tt.typeSpec)
			for i := 0; i < ft.NumField(); i++ {
				field := ft.Field(i)
				tagStr, ok := field.Tag.Lookup("experiment")
				if !ok {
					continue
				}

				// Parse all version tags
				versions := make(map[string]string)
				for _, part := range strings.Split(tagStr, ",") {
					part = strings.TrimSpace(part)
					key, value, found := strings.Cut(part, ":")
					if found {
						versions[key] = value
					}
				}

				// Validate version ordering
				if err := validateExperimentVersionOrdering(field.Name, versions); err != nil {
					t.Errorf("experiment %s in %s: %v", field.Name, tt.name, err)
				}
			}
		})
	}
}

// validateExperimentVersionOrdering checks that experiment lifecycle versions follow logical ordering:
// preview <= default <= stable, preview <= withdrawn, default <= withdrawn
func validateExperimentVersionOrdering(fieldName string, versions map[string]string) error {
	preview := versions["preview"]
	defaultVer := versions["default"]
	stable := versions["stable"]
	withdrawn := versions["withdrawn"]

	// Helper function to compare versions with proper error handling
	compareVersions := func(v1, v2, v1Name, v2Name string) error {
		if v1 != "" && v2 != "" {
			if semver.Compare(v1, v2) > 0 {
				return fmt.Errorf("%s version (%s) must be <= %s version (%s)",
					v1Name, v1, v2Name, v2)
			}
		}
		return nil
	}

	// Check preview <= default
	if err := compareVersions(preview, defaultVer, "preview", "default"); err != nil {
		return err
	}

	// Check default <= stable
	if err := compareVersions(defaultVer, stable, "default", "stable"); err != nil {
		return err
	}

	// Check preview <= stable
	if err := compareVersions(preview, stable, "preview", "stable"); err != nil {
		return err
	}

	// Check preview <= withdrawn
	if err := compareVersions(preview, withdrawn, "preview", "withdrawn"); err != nil {
		return err
	}

	// Check default <= withdrawn
	if err := compareVersions(defaultVer, withdrawn, "default", "withdrawn"); err != nil {
		return err
	}

	return nil
}
