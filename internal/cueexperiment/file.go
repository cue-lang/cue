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
	"maps"
	"slices"
	"strings"
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

	// StructCmp enables comparison of structs. This also defines the ==
	// operator to be defined on all values. For instance, comparing `1` and
	// "foo" will return false, whereas previously it would return an error.
	//
	// Proposal was defined in https://cuelang.org/issue/2358.
	StructCmp bool `experiment:"since:v0.14.0"`
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
