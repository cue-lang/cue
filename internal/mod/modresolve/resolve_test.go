package modresolve

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestResolver(t *testing.T) {
	fruitLookups := map[string]Location{
		"fruit.com/apple": {
			Host: "registry.cue.works",
		},
	}
	privateExample := map[string]Location{
		"example.com/blah": {
			Host:     "registry.example.com",
			Prefix:   "/offset",
			Insecure: true,
		},
	}
	for k, v := range fruitLookups {
		privateExample[k] = v
	}
	testCases := []struct {
		name            string
		s               string
		catchAllDefault string
		err             string
		lookups         map[string]Location
	}{
		{
			name: "two fallbacks (error)",
			s:    "registry.cue.works,registry.cuelabs.dev",
			err:  "duplicate catch-all registry",
		},
		{
			name: "no fallback",
			err:  "no catch-all registry or default",
		},
		{
			name:            "default catch-all",
			catchAllDefault: "registry.cue.works",
			lookups:         fruitLookups,
		},
		{
			name:    "specified catch-all, no default",
			s:       "registry.cue.works",
			lookups: fruitLookups,
		},
		{
			name:            "specified catch-all and default",
			s:               "registry.cue.works",
			catchAllDefault: "other.cue.works",
			lookups:         fruitLookups,
		},
		{
			name:    "private patterns, specified catch-all, no default",
			s:       "example.com=registry.example.com/offset+insecure,registry.cue.works",
			lookups: privateExample,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := ParseCUERegistry(tc.s, tc.catchAllDefault)
			if tc.err != "" {
				qt.Assert(t, qt.ErrorMatches(err, tc.err))
				return // early return because we got an error
			}
			qt.Assert(t, qt.IsNil(err))
			for prefix, want := range tc.lookups {
				got := r.Resolve(prefix)
				qt.Assert(t, qt.Equals(got, want))
			}
		})
	}
}
