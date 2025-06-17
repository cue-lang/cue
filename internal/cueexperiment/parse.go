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

// parseConfig initializes the fields in flags from the attached struct field
// tags as well as the contents of the given string, which is a comma-separated
// list of experiment names.
//
// The struct field tag indicates the life cycle of the experiment, starting
// with the version from when it was introduced, the version where it became
// default, and the version where it was rejected or accepted.
//
// Names are treated case insensitively. An empty version string indicates the
// default settings.
func parseConfig[T any](flags *T, version, experiments string) error {
	var requested map[string]bool
	if experiments != "" {
		if requested == nil {
			requested = make(map[string]bool)
		}
		for _, elem := range strings.Split(experiments, ",") {
			requested[strings.TrimSpace(elem)] = true
		}
	}
	var errs []error

	// Collect the field indices and set the default values.
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
outer:
	for i := range ft.NumField() {
		field := ft.Field(i)
		name := strings.ToLower(field.Name)
		if tagStr, ok := field.Tag.Lookup("experiment"); ok {
			for _, f := range strings.Split(tagStr, ",") {
				key, rest, _ := strings.Cut(f, ":")
				switch key {
				case "since":
					switch {
					case !requested[name]:
					case version == "":
					case semver.Compare(version, rest) < 0:
						errs = append(errs, fmt.Errorf("cannot set experiment %q before version %s", name, rest))
						continue outer
					default:
						fv.Field(i).Set(reflect.ValueOf(true))
					}

				case "accepted":
					if version == "" || semver.Compare(version, rest) >= 0 {
						fv.Field(i).Set(reflect.ValueOf(true))
					}

				case "rejected":
					if (version == "" || semver.Compare(version, rest) >= 0) && requested[name] {
						errs = append(errs, fmt.Errorf("cannot set rejected experiment %q", name))
						continue outer
					}

				default:
					panic(fmt.Errorf("unknown exp tag %q", f))
				}
			}
		}
		delete(requested, name)
	}

	for name := range requested {
		errs = append(errs, fmt.Errorf("unknown experiment %q", name))
	}

	return errors.Join(errs...)
}
