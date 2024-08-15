package jsonschema

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestVFrom(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vfrom(versionDraft04).contains(versionDraft04)))
	qt.Assert(t, qt.IsTrue(vfrom(versionDraft04).contains(versionDraft06)))
	qt.Assert(t, qt.IsTrue(vfrom(versionDraft04).contains(version2020_12)))
	qt.Assert(t, qt.IsFalse(vfrom(versionDraft06).contains(versionDraft04)))
}

func TestVTo(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vto(versionDraft04).contains(versionDraft04)))
	qt.Assert(t, qt.IsFalse(vto(versionDraft04).contains(versionDraft06)))
	qt.Assert(t, qt.IsTrue(vto(versionDraft06).contains(versionDraft04)))
	qt.Assert(t, qt.IsFalse(vto(versionDraft06).contains(versionDraft07)))
}

func TestVBetween(t *testing.T) {
	qt.Assert(t, qt.IsFalse(vbetween(versionDraft06, version2019_09).contains(versionDraft04)))
	qt.Assert(t, qt.IsTrue(vbetween(versionDraft06, version2019_09).contains(versionDraft06)))
	qt.Assert(t, qt.IsTrue(vbetween(versionDraft06, version2019_09).contains(version2019_09)))
	qt.Assert(t, qt.IsFalse(vbetween(versionDraft06, version2019_09).contains(version2020_12)))
}

func TestVSet(t *testing.T) {
	qt.Assert(t, qt.IsTrue(vset(versionDraft06).contains(versionDraft06)))
	qt.Assert(t, qt.IsFalse(vset(versionDraft06).contains(versionDraft04)))
	qt.Assert(t, qt.IsFalse(vset(versionDraft06).contains(versionDraft07)))
}
