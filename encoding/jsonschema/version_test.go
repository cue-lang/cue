// Copyright 2024 CUE Authors
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

package jsonschema

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestVFrom(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vfrom(VersionDraft4).contains(VersionDraft4)))
	qt.Assert(t, qt.IsTrue(vfrom(VersionDraft4).contains(VersionDraft6)))
	qt.Assert(t, qt.IsTrue(vfrom(VersionDraft4).contains(VersionDraft2020_12)))
	qt.Assert(t, qt.IsFalse(vfrom(VersionDraft6).contains(VersionDraft4)))
}

func TestVTo(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vto(VersionDraft4).contains(VersionDraft4)))
	qt.Assert(t, qt.IsFalse(vto(VersionDraft4).contains(VersionDraft6)))
	qt.Assert(t, qt.IsTrue(vto(VersionDraft6).contains(VersionDraft4)))
	qt.Assert(t, qt.IsFalse(vto(VersionDraft6).contains(VersionDraft7)))
}

func TestVBetween(t *testing.T) {
	qt.Assert(t, qt.IsFalse(vbetween(VersionDraft6, VersionDraft2019_09).contains(VersionDraft4)))
	qt.Assert(t, qt.IsTrue(vbetween(VersionDraft6, VersionDraft2019_09).contains(VersionDraft6)))
	qt.Assert(t, qt.IsTrue(vbetween(VersionDraft6, VersionDraft2019_09).contains(VersionDraft2019_09)))
	qt.Assert(t, qt.IsFalse(vbetween(VersionDraft6, VersionDraft2019_09).contains(VersionDraft2020_12)))
}

func TestVSet(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vset(VersionDraft6).contains(VersionDraft6)))
	qt.Assert(t, qt.IsFalse(vset(VersionDraft6).contains(VersionDraft4)))
	qt.Assert(t, qt.IsFalse(vset(VersionDraft6).contains(VersionDraft7)))
}
