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
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"

	"cuelang.org/go/internal/mod/semver"
)

type expMap struct{ m map[string]bool }

func (m *expMap) add(x ...string) {
	for _, a := range x {
		if a == "" {
			continue
		}
		if m.m == nil {
			m.m = make(map[string]bool)
		}
		for _, elem := range strings.Split(a, ",") {
			m.m[strings.TrimSpace(elem)] = true
		}
	}
}

func (m *expMap) key() string {
	a := slices.Collect(maps.Keys(m.m))
	sort.Strings(a)
	return strings.Join(a, ",")
}

func (m *expMap) has(x string) bool {
	return m.m[x]
}

func (m *expMap) remove(x string) {
	delete(m.m, x)
}

func (m *expMap) remaining() map[string]bool {
	return m.m
}

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
func parseConfig[T any](flags *T, version string, experiments expMap) error {
	var errs []error

	// Collect the field indices and set the default values.
	fv := reflect.ValueOf(flags).Elem()
	ft := fv.Type()
outer:
	for i := range ft.NumField() {
		field := ft.Field(i)
		if tagStr, ok := field.Tag.Lookup("experiment"); ok {
			name := strings.ToLower(field.Name)
			for _, f := range strings.Split(tagStr, ",") {
				key, rest, _ := strings.Cut(f, ":")
				switch key {
				case "since":
					switch {
					case !experiments.has(name):
					case version != "" && semver.Compare(version, rest) < 0:
						const msg = "cannot set experiment %q before version %s"
						errs = append(errs, fmt.Errorf(msg, name, rest))
						continue outer
					default:
						fv.Field(i).Set(reflect.ValueOf(true))
					}

				case "accepted":
					if version == "" || semver.Compare(version, rest) >= 0 {
						fv.Field(i).Set(reflect.ValueOf(true))
					}

				case "rejected":
					expired := (version == "" || semver.Compare(version, rest) >= 0)
					if expired && experiments.has(name) {
						const msg = "cannot set rejected experiment %q"
						errs = append(errs, fmt.Errorf(msg, name))
						continue outer
					}

				default:
					panic(fmt.Errorf("unknown exp tag %q", f))
				}
			}
			experiments.remove(name)
		}
	}

	for name := range experiments.remaining() {
		errs = append(errs, fmt.Errorf("unknown experiment %q", name))
	}

	return errors.Join(errs...)
}
