//go:build ignore

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package module

import "testing"

var escapeTests = []struct {
	path string
	esc  string // empty means same as path
}{
	{path: "ascii.com/abcdefghijklmnopqrstuvwxyz.-/~_0123456789"},
	{path: "github.com/GoogleCloudPlatform/omega", esc: "github.com/!google!cloud!platform/omega"},
}

func TestEscapePath(t *testing.T) {
	// Check invalid paths.
	for _, tt := range checkPathTests {
		if !tt.ok {
			_, err := EscapePath(tt.path)
			if err == nil {
				t.Errorf("EscapePath(%q): succeeded, want error (invalid path)", tt.path)
			}
		}
	}

	// Check encodings.
	for _, tt := range escapeTests {
		esc, err := EscapePath(tt.path)
		if err != nil {
			t.Errorf("EscapePath(%q): unexpected error: %v", tt.path, err)
			continue
		}
		want := tt.esc
		if want == "" {
			want = tt.path
		}
		if esc != want {
			t.Errorf("EscapePath(%q) = %q, want %q", tt.path, esc, want)
		}
	}
}

var badUnescape = []string{
	"github.com/GoogleCloudPlatform/omega",
	"github.com/!google!cloud!platform!/omega",
	"github.com/!0google!cloud!platform/omega",
	"github.com/!_google!cloud!platform/omega",
	"github.com/!!google!cloud!platform/omega",
	"",
}

func TestUnescapePath(t *testing.T) {
	// Check invalid decodings.
	for _, bad := range badUnescape {
		_, err := UnescapePath(bad)
		if err == nil {
			t.Errorf("UnescapePath(%q): succeeded, want error (invalid decoding)", bad)
		}
	}

	// Check invalid paths (or maybe decodings).
	for _, tt := range checkPathTests {
		if !tt.ok {
			path, err := UnescapePath(tt.path)
			if err == nil {
				t.Errorf("UnescapePath(%q) = %q, want error (invalid path)", tt.path, path)
			}
		}
	}

	// Check encodings.
	for _, tt := range escapeTests {
		esc := tt.esc
		if esc == "" {
			esc = tt.path
		}
		path, err := UnescapePath(esc)
		if err != nil {
			t.Errorf("UnescapePath(%q): unexpected error: %v", esc, err)
			continue
		}
		if path != tt.path {
			t.Errorf("UnescapePath(%q) = %q, want %q", esc, path, tt.path)
		}
	}
}

func TestMatchPathMajor(t *testing.T) {
	for _, test := range []struct {
		v, pathMajor string
		want         bool
	}{
		{"v0.0.0", "", true},
		{"v0.0.0", "/v2", false},
		{"v0.0.0", ".v0", true},
		{"v0.0.0-20190510104115-cbcb75029529", ".v1", true},
		{"v1.0.0", "/v2", false},
		{"v1.0.0", ".v1", true},
		{"v1.0.0", ".v1-unstable", true},
		{"v2.0.0+incompatible", "", true},
		{"v2.0.0", "", false},
		{"v2.0.0", "/v2", true},
		{"v2.0.0", ".v2", true},
	} {
		if got := MatchPathMajor(test.v, test.pathMajor); got != test.want {
			t.Errorf("MatchPathMajor(%q, %q) = %v, want %v", test.v, test.pathMajor, got, test.want)
		}
	}
}

func TestMatchPrefixPatterns(t *testing.T) {
	for _, test := range []struct {
		globs, target string
		want          bool
	}{
		{"", "rsc.io/quote", false},
		{"/", "rsc.io/quote", false},
		{"*/quote", "rsc.io/quote", true},
		{"*/quo", "rsc.io/quote", false},
		{"*/quo??", "rsc.io/quote", true},
		{"*/quo*", "rsc.io/quote", true},
		{"*quo*", "rsc.io/quote", false},
		{"rsc.io", "rsc.io/quote", true},
		{"*.io", "rsc.io/quote", true},
		{"rsc.io/", "rsc.io/quote", true},
		{"rsc", "rsc.io/quote", false},
		{"rsc*", "rsc.io/quote", true},

		{"rsc.io", "rsc.io/quote/v3", true},
		{"*/quote", "rsc.io/quote/v3", true},
		{"*/quote/", "rsc.io/quote/v3", true},
		{"*/quote/*", "rsc.io/quote/v3", true},
		{"*/quote/*/", "rsc.io/quote/v3", true},
		{"*/v3", "rsc.io/quote/v3", false},
		{"*/*/v3", "rsc.io/quote/v3", true},
		{"*/*/*", "rsc.io/quote/v3", true},
		{"*/*/*/", "rsc.io/quote/v3", true},
		{"*/*/*", "rsc.io/quote", false},
		{"*/*/*/", "rsc.io/quote", false},

		{"*/*/*,,", "rsc.io/quote", false},
		{"*/*/*,,*/quote", "rsc.io/quote", true},
		{",,*/quote", "rsc.io/quote", true},
	} {
		if got := MatchPrefixPatterns(test.globs, test.target); got != test.want {
			t.Errorf("MatchPrefixPatterns(%q, %q) = %t, want %t", test.globs, test.target, got, test.want)
		}
	}
}
