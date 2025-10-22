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
//	preview:     the version from when the experiment was introduced.
//	stable:      the version from when it is permanently set to true.
//	withdrawn:   results in an error if the user attempts to use the flag.
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
	Testing bool `experiment:"preview:v0.13.0"`

	// Accepted_ is for testing purposes only. It should be removed when an
	// experiment is accepted and can be used to test this feature instead.
	Accepted_ bool `experiment:"preview:v0.13.0,stable:v0.15.0"`

	// StructCmp enables comparison of structs. This also defines the ==
	// operator to be defined on all values. For instance, comparing 1 and
	// "foo" will return false, whereas previously it would return an error.
	//
	// Proposal:      https://cuelang.org/issue/2583
	// Spec change:   https://cuelang.org/cl/1217013
	// Spec change:   https://cuelang.org/cl/1217014
	StructCmp bool `experiment:"preview:v0.14.0,stable:v0.15.0"`

	// ExplicitOpen enables the postfix ... operator to explicitly open
	// closed structs, allowing additional fields to be added.
	//
	// Proposal:      https://cuelang.org/issue/4032
	// Spec change:   https://cuelang.org/cl/1221642
	// Requires cue fix when upgrading
	ExplicitOpen bool `experiment:"preview:v0.15.0"`

	// AliasV2 enables the use of 'self' identifier to refer to the
	// enclosing struct and enables the postfix alias syntax (~X and ~(K,V)).
	// The file where this experiment is enabled disallows the use of old prefix
	// alias syntax (X=).
	//
	// Proposal:      https://cuelang.org/issue/4014
	// Spec change:   https://cuelang.org/cl/1222377
	// Requires cue fix when upgrading
	AliasV2 bool `experiment:"preview:v0.15.0"`
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

// IsPreview returns true if the experiment exists and can be used
// for the given version.
func IsPreview(experiment, version string) bool {
	return isPreview(experiment, version, File{})
}

func isPreview(experiment, version string, t any) bool {
	expInfo := getExperimentInfoT(experiment, t)
	if expInfo == nil {
		return false
	}
	return expInfo.isValidForVersion(version)
}

func (e *experimentInfo) isValidForVersion(version string) bool {
	// Check if experiment is available for this version
	if version != "" && e.Preview != "" {
		if semver.Compare(version, e.Preview) < 0 {
			return false
		}
	}

	// Check if experiment is rejected for this version
	if e.Withdrawn != "" {
		if version == "" || semver.Compare(version, e.Withdrawn) >= 0 {
			return false
		}
	}

	return true
}

// IsStable returns true if the experiment is stable (no longer
// experimental) for the given version.
func IsStable(experiment, version string) bool {
	expInfo := getExperimentInfo(experiment)
	if expInfo == nil {
		return false
	}
	return expInfo.isStableForVersion(version)
}

func (e *experimentInfo) isStableForVersion(version string) bool {
	if e.Stable == "" {
		return false
	}
	return version == "" || semver.Compare(version, e.Stable) >= 0
}

// CanApplyFix validates whether an experiment fix can be applied
// to a file with the given version and existing experiments.
func CanApplyFix(experiment, version, target string) error {
	return canApplyExperimentFix(experiment, version, target, File{})
}

func canApplyExperimentFix(experiment, version, target string, t any) error {
	expInfo := getExperimentInfoT(experiment, t)
	if expInfo == nil {
		return fmt.Errorf("unknown experiment %q", experiment)
	}

	// Check if experiment is valid for this version
	if !expInfo.isValidForVersion(target) {
		if version != "" && expInfo.Preview != "" &&
			semver.Compare(target, expInfo.Preview) < 0 {
			const msg = "experiment %q requires language version %s or later, have %s"
			return fmt.Errorf(msg, experiment, expInfo.Preview, version)
		}

		if expInfo.Withdrawn != "" {
			if version == "" || semver.Compare(target, expInfo.Withdrawn) >= 0 {
				const msg = "experiment %q is withdrawn in language version %s"
				return fmt.Errorf(msg, experiment, expInfo.Withdrawn)
			}
		}
	}

	// Check if experiment is already stable (cannot fix)
	if expInfo.isStableForVersion(version) {
		const msg = "experiment %q is already stable as of language version %s - cannot apply fix"
		return fmt.Errorf(msg, experiment, expInfo.Stable)
	}

	return nil
}

// GetActive returns all experiments that are active (can be enabled)
// for the given version, but not yet accepted.
func GetActive(origVersion, targetVersion string) []string {
	return getActiveExperiments(origVersion, targetVersion, File{})
}

func getActiveExperiments(origVersion, targetVersion string, t any) []string {
	var active []string

	ft := reflect.TypeOf(t)
	for i := 0; i < ft.NumField(); i++ {
		field := ft.Field(i)
		tagStr, ok := field.Tag.Lookup("experiment")
		if !ok {
			continue
		}
		name := strings.ToLower(field.Name)
		expInfo := parseExperimentTag(tagStr)

		// Skip if not yet available for this version
		if targetVersion != "" && expInfo.Preview != "" && semver.Compare(targetVersion, expInfo.Preview) < 0 {
			continue
		}

		// Skip if already stable
		if expInfo.Stable != "" && (targetVersion == "" || semver.Compare(origVersion, expInfo.Stable) >= 0) {
			continue
		}

		// Skip if withdrawn
		if expInfo.Withdrawn != "" {
			continue
		}

		active = append(active, name)
	}

	slices.Sort(active)
	return active
}

// GetUpgradable returns all experiments that are stable
// (possibly in later versions), that can be upgraded from the current
// version (must be lower than stable) to the desired version.
func GetUpgradable(origVersion, targetVersion string) []string {
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
		tagStr, ok := field.Tag.Lookup("experiment")
		if !ok {
			continue
		}
		name := strings.ToLower(field.Name)
		expInfo := parseExperimentTag(tagStr)

		if expInfo.Stable != "" &&
			semver.Compare(targetVersion, expInfo.Preview) >= 0 &&
			semver.Compare(origVersion, expInfo.Stable) < 0 {
			accepted = append(accepted, name)
		}
	}

	slices.Sort(accepted)
	return accepted
}

// ShouldRemoveAttribute returns true if the experiment attribute
// should be removed because the experiment is stable for the given version.
func ShouldRemoveAttribute(experiment, version string) bool {
	return IsStable(experiment, version)
}

// experimentInfo holds parsed experiment lifecycle information
type experimentInfo struct {
	Preview   string
	Stable    string
	Withdrawn string
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
		key, rest, _ := strings.Cut(f, ":")
		if !semver.IsValid(rest) {
			panic(fmt.Sprintf("invalid semver in experiment tag %q: %q", key, rest))
		}
		switch key {
		case "preview":
			info.Preview = rest
		case "stable":
			info.Stable = rest
		case "withdrawn":
			info.Withdrawn = rest
		}
	}
	return info
}
