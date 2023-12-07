// Copyright 2023 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cueexperiment

import (
	"testing"

	"github.com/go-quicktest/qt"
)

var tests = []struct {
	testName      string
	cueExperiment string
	flagVal       *bool
	want          bool
	wantError     string
}{{
	testName:      "Empty",
	cueExperiment: "",
	flagVal:       &Flags.Modules,
	want:          false,
}, {
	testName:      "Unknown",
	cueExperiment: "foo",
	flagVal:       &Flags.Modules,
	wantError:     "unknown CUE_EXPERIMENT foo",
}, {
	testName:      "Set",
	cueExperiment: "modules",
	flagVal:       &Flags.Modules,
	want:          true,
}, {
	testName:      "SetTwice",
	cueExperiment: "modules,modules",
	flagVal:       &Flags.Modules,
	want:          true,
}, {
	testName:      "SetWithUnknown",
	cueExperiment: "modules,other",
	flagVal:       &Flags.Modules,
	wantError:     "unknown CUE_EXPERIMENT other",
}}

func TestInit(t *testing.T) {
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			setZero(&Flags)
			t.Setenv("CUE_EXPERIMENT", test.cueExperiment)
			err := Init()
			if test.wantError != "" {
				qt.Assert(t, qt.ErrorMatches(err, test.wantError))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			qt.Assert(t, qt.Equals(*test.flagVal, test.want))
		})
	}
}

func setZero[T any](x *T) {
	*x = *new(T)
}
