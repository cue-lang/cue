package jsonschema

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestVFrom(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vfrom(VersionDraft4).contains(VersionDraft4)))
	qt.Assert(t, qt.IsTrue(vfrom(VersionDraft4).contains(VersionDraft6)))
	qt.Assert(t, qt.IsTrue(vfrom(VersionDraft4).contains(Version2020_12)))
	qt.Assert(t, qt.IsFalse(vfrom(VersionDraft6).contains(VersionDraft4)))
}

func TestVTo(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vto(VersionDraft4).contains(VersionDraft4)))
	qt.Assert(t, qt.IsFalse(vto(VersionDraft4).contains(VersionDraft6)))
	qt.Assert(t, qt.IsTrue(vto(VersionDraft6).contains(VersionDraft4)))
	qt.Assert(t, qt.IsFalse(vto(VersionDraft6).contains(VersionDraft7)))
}

func TestVBetween(t *testing.T) {
	qt.Assert(t, qt.IsFalse(vbetween(VersionDraft6, Version2019_09).contains(VersionDraft4)))
	qt.Assert(t, qt.IsTrue(vbetween(VersionDraft6, Version2019_09).contains(VersionDraft6)))
	qt.Assert(t, qt.IsTrue(vbetween(VersionDraft6, Version2019_09).contains(Version2019_09)))
	qt.Assert(t, qt.IsFalse(vbetween(VersionDraft6, Version2019_09).contains(Version2020_12)))
}

func TestVSet(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vset(VersionDraft6).contains(VersionDraft6)))
	qt.Assert(t, qt.IsFalse(vset(VersionDraft6).contains(VersionDraft4)))
	qt.Assert(t, qt.IsFalse(vset(VersionDraft6).contains(VersionDraft7)))
}
