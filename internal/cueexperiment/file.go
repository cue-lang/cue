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
	"maps"
	"reflect"
	"slices"
	"strings"

	"cuelang.org/go/internal/mod/semver"
)

// This contains experiments that are configured per file.

// File defines the experiments that can be set per file. Users can activate
// experiments by setting them using a file-based attribute @experiment() in
// a CUE file. When an experiment is first introduced, it is disabled by
// default.
//
//	since:       the version from when the experiment was introduced.
//	accepted:    the version from when it is permanently set to true.
//	rejected:    results in an error if the user attempts to use the flag.
type File struct {
	// version is the module version of the file that was compiled.
	version string

	// experiments is a comma-separated list of experiments that are enabled
	// for this file. This is for documentation purposes only, as the
	// experiments are already set in the struct fields.
	experiments string

	// Testing is used to enable experiments for testing.
	//
	// TODO: we could later use it for enabling testing features, such as
	// testing-specific builtins.
	Testing bool `experiment:"since:v0.13.0"`

	// Accepted_ is for testing purposes only. It should be removed when an
	// experiment is accepted and can be used to test this feature instead.
	Accepted_ bool `experiment:"since:v0.13.0,accepted:v0.15.0"`

	// StructCmp enables comparison of structs. This also defines the ==
	// operator to be defined on all values. For instance, comparing `1` and
	// "foo" will return false, whereas previously it would return an error.
	//
	// Proposal:      https://cuelang.org/issue/2358
	// Spec change:   https://cuelang.org/cl/1217013
	// Spec change:   https://cuelang.org/cl/1217014
	// Needs cue fix: ❌
	StructCmp bool `experiment:"since:v0.14.0"`

	// ExplicitOpen enables the postfix ... operator to explicitly open
	// closed structs, allowing additional fields to be added.
	//
	// Proposal:      https://cuelang.org/issue/4032
	// Spec change:   https://cuelang.org/cl/1221642
	// Needs cue fix: ✅
	ExplicitOpen bool `experiment:"since:v0.15.0"`

	// Try enables the try construct for succinct error handling.
	//
	// Proposal:      https://cue-lang/cue#4019
	// Spec change:   https://cuelang.org/cl/1221644
	// Needs cue fix: ❌
	Try bool `experiment:"since:v0.15.0"`
}

// LanguageVersion returns the language version of the file or "" if no language
// version is associated with it.
func (f *File) LanguageVersion() string {
	return f.version
}

// NewFile parses the given comma-separated list of experiments for
// the given version and returns a PerFile struct with the experiments enabled.
// A empty version indicates the default version.
func NewFile(version string, experiments ...string) (*File, error) {
	// TODO: cash versions for a given version where there is no experiment
	// string.
	m := parseExperiments(experiments...)
	f := &File{
		version:     version,
		experiments: strings.Join(slices.Sorted(maps.Keys(m)), ","),
	}

	if err := parseConfig(f, version, m); err != nil {
		return nil, err
	}
	return f, nil
}

// IsExperimentValid returns true if the experiment exists and can be used
// for the given version.
func IsExperimentValid(experiment, version string) bool {
	return isExperimentValid(experiment, version, File{})
}

func isExperimentValid(experiment, version string, t any) bool {
	expInfo := getExperimentInfoT(experiment, t)
	if expInfo == nil {
		return false
	}
	return expInfo.isValidForVersion(version)
}

func (e *experimentInfo) isValidForVersion(version string) bool {
	// Check if experiment is available for this version
	if version != "" && e.Since != "" {
		if semver.Compare(version, e.Since) < 0 {
			return false
		}
	}

	// Check if experiment is rejected for this version
	if e.Rejected != "" {
		rejected := (version == "" || semver.Compare(version, e.Rejected) >= 0)
		if rejected {
			return false
		}
	}

	return true
}

// IsExperimentAccepted returns true if the experiment is accepted (no longer
// experimental) for the given version.
func IsExperimentAccepted(experiment, version string) bool {
	expInfo := getExperimentInfo(experiment)
	if expInfo == nil {
		return false
	}
	return expInfo.isAcceptedForVersion(version)
}

func (e *experimentInfo) isAcceptedForVersion(version string) bool {
	if e.Accepted == "" {
		return false
	}
	return version == "" || semver.Compare(version, e.Accepted) >= 0
}

// CanApplyExperimentFix validates whether an experiment fix can be applied
// to a file with the given version and existing experiments.
func CanApplyExperimentFix(experiment, version, target string) error {
	return canApplyExperimentFix(experiment, version, target, File{})
}

func canApplyExperimentFix(experiment, version, target string, t any) error {
	expInfo := getExperimentInfoT(experiment, t)
	if expInfo == nil {
		return fmt.Errorf("unknown experiment %q", experiment)
	}

	// Check if experiment is valid for this version
	if !expInfo.isValidForVersion(target) {
		if version != "" && expInfo.Since != "" && semver.Compare(target, expInfo.Since) < 0 {
			return fmt.Errorf("experiment %q requires version %s or later, got %s", experiment, expInfo.Since, version)
		}

		if expInfo.Rejected != "" {
			rejected := (version == "" || semver.Compare(target, expInfo.Rejected) >= 0)
			if rejected {
				return fmt.Errorf("experiment %q is rejected in version %s", experiment, expInfo.Rejected)
			}
		}
	}

	// Check if experiment is already accepted (cannot fix)
	if expInfo.isAcceptedForVersion(version) {
		return fmt.Errorf("experiment %q is already accepted as of version %s - cannot apply fix", experiment, expInfo.Accepted)
	}

	return nil
}

// GetActiveExperiments returns all experiments that are active (can be enabled)
// for the given version, but not yet accepted.
func GetActiveExperiments(originalVersion, targetVersion string) []string {
	return getActiveExperiments(originalVersion, targetVersion, File{})
}

func getActiveExperiments(originalVersion, targetVersion string, t any) []string {
	var active []string

	ft := reflect.TypeOf(t)
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		if tagStr, ok := field.Tag.Lookup("experiment"); ok {
			name := strings.ToLower(field.Name)
			expInfo := parseExperimentTag(tagStr)

			// Skip if not yet available for this version
			if targetVersion != "" && expInfo.Since != "" && semver.Compare(targetVersion, expInfo.Since) < 0 {
				continue
			}

			// Skip if already accepted
			if expInfo.Accepted != "" && (targetVersion == "" || semver.Compare(originalVersion, expInfo.Accepted) >= 0) {
				continue
			}

			// Skip if rejected
			if expInfo.Rejected != "" {
				continue
			}

			active = append(active, name)
		}
	}

	slices.Sort(active)
	return active
}

// GetUpgradeExperiments returns all experiments that are accepted
// (possibly in later versions), that can be upgraded from the current
// version (must be lower than accepted) to the desired version.
func GetUpgradeExperiments(origVersion, targetVersion string) []string {
	return getUpgradeExperiments(origVersion, targetVersion, File{})
}

func getUpgradeExperiments(origVersion, targetVersion string, t any) []string {
	var accepted []string
	if origVersion == "" {
		panic("original version is empty")
	}

	ft := reflect.TypeOf(t)
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		if tagStr, ok := field.Tag.Lookup("experiment"); ok {
			name := strings.ToLower(field.Name)
			expInfo := parseExperimentTag(tagStr)

			if expInfo.Accepted != "" {
				if semver.Compare(targetVersion, expInfo.Since) >= 0 &&
					semver.Compare(origVersion, expInfo.Accepted) < 0 {
					accepted = append(accepted, name)
				}
			}
		}
	}

	slices.Sort(accepted)
	return accepted
}

// ShouldRemoveExperimentAttribute returns true if the experiment attribute
// should be removed because the experiment is accepted for the given version.
func ShouldRemoveExperimentAttribute(experiment, version string) bool {
	return IsExperimentAccepted(experiment, version)
}

// experimentInfo holds parsed experiment lifecycle information
type experimentInfo struct {
	Since    string
	Accepted string
	Rejected string
}

// getExperimentInfo returns experiment lifecycle info for the given experiment name
func getExperimentInfo(experiment string) *experimentInfo {
	return getExperimentInfoT(experiment, File{})
}

func getExperimentInfoT(experiment string, t any) *experimentInfo {
	ft := reflect.TypeOf(t)
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		if strings.EqualFold(field.Name, experiment) {
			if tagStr, ok := field.Tag.Lookup("experiment"); ok {
				return parseExperimentTag(tagStr)
			}
		}
	}
	return nil
}

// parseExperimentTag parses experiment tag string into experimentInfo
func parseExperimentTag(tagStr string) *experimentInfo {
	info := &experimentInfo{}
	for f := range strings.SplitSeq(tagStr, ",") {
		f = strings.TrimSpace(f)
		key, rest, _ := strings.Cut(f, ":")
		switch key {
		case "since":
			info.Since = rest
		case "accepted":
			info.Accepted = rest
		case "rejected":
			info.Rejected = rest
		}
	}
	return info
}
