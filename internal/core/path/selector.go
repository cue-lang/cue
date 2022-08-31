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

// Package path provides utilities for converting cue.Selectors and cue.Paths to
// internal equivalents.
package path

import (
	"math/bits"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// ToFeatureType converts a SelectorType constant to a FeatureType. It assumes a single label bit is set.
func ToFeatureType(t cue.SelectorType) adt.FeatureType {
	t = t.LabelType()
	return adt.FeatureType(bits.Len16(uint16(t)))
}

// MakeFeature converts a cue.Selector to an adt.Feature for a given runtime.
func MakeFeature(r *runtime.Runtime, s cue.Selector) adt.Feature {
	constraintType := s.ConstraintType()
	labelType := s.LabelType()

	if constraintType == cue.PatternConstraint {
		switch labelType {
		case cue.StringLabel:
			return adt.AnyString
		case cue.IndexLabel:
			return adt.AnyIndex

		// These are not really a thing at the moment:
		case cue.DefinitionLabel:
			return adt.AnyDefinition
		case cue.HiddenLabel:
			return adt.AnyHidden // TODO: fix
		case cue.HiddenDefinitionLabel:
			return adt.AnyHidden // TODO: fix
		default:
			panic("unreachable")
		}
	}

	switch labelType {
	case cue.StringLabel:
		return adt.MakeStringLabel(r, s.Unquoted())

	case cue.IndexLabel:
		return adt.MakeIntLabel(adt.IntLabel, int64(s.Index()))

	case cue.DefinitionLabel:
		return adt.MakeNamedLabel(r, adt.DefinitionLabel, s.String())

	case cue.HiddenLabel:
		str := adt.HiddenKey(s.String(), s.PkgPath())
		return adt.MakeNamedLabel(r, adt.HiddenLabel, str)

	case cue.HiddenDefinitionLabel:
		str := adt.HiddenKey(s.String(), s.PkgPath())
		return adt.MakeNamedLabel(r, adt.HiddenDefinitionLabel, str)

	default:
		return adt.InvalidLabel
	}
}
