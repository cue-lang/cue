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
	"errors"
	"fmt"
	"reflect"
	"strings"

	"cuelang.org/go/internal/mod/semver"
)

func parseExperiments(x ...string) (m map[string]bool) {
	for _, a := range x {
		if a == "" {
			continue
		}
		if m == nil {
			m = make(map[string]bool)
		}
		for elem := range strings.SplitSeq(a, ",") {
			m[strings.TrimSpace(elem)] = true
		}
	}
	return m
}

// parseConfig initializes the fields in flags from the attached struct field
// tags as well as the contents of the given string, which is a comma-separated
// list of experiment names.
//
// version is the language version associated with th module of a file. An empty
// version string indicates the latest language version supported by the
// compiler.
//
// experiments is a comma-separated list of experiment names.
//
// The struct field tag indicates the life cycle of the experiment, starting
// with the version from when it was introduced, the version where it became
// default, and the version where it was rejected or accepted.
//
// Experiments are all lowercase. Field names are converted to lower case.
func parseConfig[T any](flags *T, version string, experiments map[string]bool) error {
	var errs []error

	// Collect the field indices and set the default values.
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
	for i := range ft.NumField() {
		field := ft.Field(i)
		if tagStr, ok := field.Tag.Lookup("experiment"); ok {
			name := strings.ToLower(field.Name)
			for f := range strings.SplitSeq(tagStr, ",") {
				key, rest, _ := strings.Cut(f, ":")
				switch key {
				case "preview":
					switch {
					case !experiments[name]:
					case version != "" && semver.Compare(version, rest) < 0:
						const msg = "cannot set experiment %q before version %s"
						errs = append(errs, fmt.Errorf(msg, name, rest))
					default:
						fv.Field(i).Set(reflect.ValueOf(true))
					}

				case "stable":
					if version == "" || semver.Compare(version, rest) >= 0 {
						fv.Field(i).Set(reflect.ValueOf(true))
					}

				case "withdrawn":
					expired := (version == "" || semver.Compare(version, rest) >= 0)
					if expired && experiments[name] {
						const msg = "cannot set rejected experiment %q"
						errs = append(errs, fmt.Errorf(msg, name))
					}

				default:
					panic(fmt.Errorf("unknown exp tag %q", f))
				}
			}
			delete(experiments, name)
		}
	}

	for name := range experiments {
		errs = append(errs, fmt.Errorf("unknown experiment %q", name))
	}

	return errors.Join(errs...)
}
