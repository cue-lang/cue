// Copyright 2022 CUE Authors
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

package path

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// TestToFeatureType also tests that SelectorType and FeatureType are in sync.
func TestToFeatureType(t *testing.T) {
	testCases := []struct {
		s cue.SelectorType
		f adt.FeatureType
	}{{
		cue.InvalidSelectorType,
		adt.InvalidLabelType,
	}, {
		cue.StringLabel,
		adt.StringLabel,
	}, {
		cue.IndexLabel,
		adt.IntLabel,
	}, {
		cue.DefinitionLabel,
		adt.DefinitionLabel,
	}, {
		cue.HiddenLabel,
		adt.HiddenLabel,
	}, {
		cue.HiddenDefinitionLabel,
		adt.HiddenDefinitionLabel,
	}, {
		cue.StringLabel | cue.OptionalConstraint,
		adt.StringLabel,
	}, {
		cue.OptionalConstraint,
		adt.InvalidLabelType,
	}}
	for _, tc := range testCases {
		t.Run(tc.s.String(), func(t *testing.T) {
			if got := ToFeatureType(tc.s); got != tc.f {
				t.Errorf("got %v, want %v", got, tc.f)
			}
		})
	}
}

func TestMakeFeature(t *testing.T) {
	testCases := []struct {
		sel cue.Selector
		str string
	}{{
		sel: cue.Str("s-t"),
		str: `"s-t"`,
	}, {
		// Optional should be disregarded, as it is not part of a Feature.
		sel: cue.Str("s-t").Optional(),
		str: `"s-t"`,
	}, {
		sel: cue.Index(5),
		str: "5",
	}, {
		sel: cue.Def("#Foo"),
		str: "#Foo",
	}, {
		sel: cue.Hid("_foo", "pkg"),
		str: "_foo",
	}, {
		sel: cue.Hid("_#foo", "pkg"),
		str: "_#foo",
	}, {
		sel: cue.AnyString,
		str: `_`,
	}}
	for _, tc := range testCases {
		r := runtime.New()
		t.Run(tc.sel.String(), func(t *testing.T) {
			got := MakeFeature(r, tc.sel).SelectorString(r)
			if got != tc.str {
				t.Errorf("got %v, want %v", got, tc.str)
			}
		})
	}
}
