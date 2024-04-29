package cueversion

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestModuleVersion(t *testing.T) {
	// This is just a smoke test to make sure that things
	// are wired up OK. It would be possible to unit
	// test the logic inside Version, but it's simple
	// enough that that would amount to creating invariants
	// that just match the code, not providing any more
	// assurance of correctness.
	vers := ModuleVersion()
	qt.Assert(t, qt.Not(qt.Equals(vers, "")))
}

func TestUserAgent(t *testing.T) {
	agent := UserAgent("custom")
	qt.Assert(t, qt.Matches(agent,
		`Cue/[^ ]+ \(custom; lang v[^)]+\) Go/[^ ]+ \([^/]+/[^/]+\)`,
	))
}

func TestTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(req.UserAgent()))
	}))
	defer srv.Close()
	client := &http.Client{
		Transport: NewTransport("foo", nil),
	}
	resp, err := client.Get(srv.URL)
	qt.Assert(t, qt.IsNil(err))
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Matches(string(data), `Cue/[^ ]+ \(foo; lang v[^)]+\) Go/[^ ]+ \([^/]+/[^/]+\)`))
}
