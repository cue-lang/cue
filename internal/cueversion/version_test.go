package cueversion

import (
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/mod/semver"
)

func TestVersion(t *testing.T) {
	// This is just a smoke test to make sure that things
	// are wired up OK. It would be possible to unit
	// test the logic inside Version, but it's simple
	// enough that that would amount to creating invariants
	// that just match the code, not providing any more
	// assurance of correctness.
	vers := Version()
	qt.Assert(t, qt.Satisfies(vers, semver.IsValid))
}

func TestUserAgent(t *testing.T) {
	agent := UserAgent("custom")
	qt.Assert(t, qt.Matches(agent,
		`Cue/v[^ ]+ Go/go1\.[^ ]+ \([^/]+/[^/]+\)`,
	))
}
