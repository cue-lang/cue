package modresolve

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestResolver(t *testing.T) {
	testCases := []struct {
		testName        string
		s               string
		catchAllDefault string
		err             string
		lookups         map[string]Location
	}{{
		testName: "MultipleFallbacks",
		s:        "registry.cue.works,registry.cuelabs.dev",
		err:      "duplicate catch-all registry",
	}, {
		testName: "NoRegistryOrDefault",
		err:      "no catch-all registry or default",
	}, {
		testName: "InvalidRegistry",
		s:        "$#foo",
		err:      `invalid registry "\$#foo": invalid host name "\$#foo" in registry`,
	}, {
		testName: "InvalidSecuritySuffix",
		s:        "foo.com+bogus",
		err:      `invalid registry "foo.com\+bogus": unknown suffix \("\+bogus"\), need \+insecure, \+secure or no suffix\)`,
	}, {
		testName: "IPV6AddrWithoutBrackets",
		s:        "::1",
		err:      `invalid registry "::1": invalid host name "::1" in registry`,
	}, {
		testName: "EmptyElement",
		s:        "foo.com,",
		err:      `empty registry part`,
	}, {
		testName: "MissingPrefix",
		s:        "=foo.com",
		err:      `empty module prefix`,
	}, {
		testName: "MissingRegistry",
		s:        "x.com=",
		err:      `empty registry reference`,
	}, {
		testName: "InvalidModulePrefix",
		s:        "foo#=foo.com",
		err:      `invalid module path "foo#": invalid char '#'`,
	}, {
		testName: "DuplicateModulePrefix",
		s:        "x.com=r.org,x.com=q.org",
		err:      `duplicate module prefix "x.com"`,
	}, {
		testName: "NoDefaultCatchAll",
		s:        "x.com=r.org",
		err:      `no default catch-all registry provided`,
	}, {
		testName:        "InvalidCatchAll",
		s:               "x.com=r.org",
		catchAllDefault: "bogus",
		err:             `invalid catch-all registry "bogus": invalid host name "bogus" in registry`,
	}, {
		testName: "InvalidRegistryRef",
		s:        "foo.com//bar",
		err:      `invalid registry "foo.com//bar": invalid reference syntax \("foo.com//bar"\)`,
	}, {
		testName: "RegistryRefWithDigest",
		s:        "foo.com/bar@sha256:f3c16f525a1b7c204fc953d6d7db7168d84ebf4902f83c3a37d113b18c28981f",
		err:      `invalid registry "foo.com/bar@sha256:f3c16f525a1b7c204fc953d6d7db7168d84ebf4902f83c3a37d113b18c28981f": cannot have an associated tag or digest`,
	}, {
		testName: "RegistryRefWithTag",
		s:        "foo.com/bar:sometag",
		err:      `invalid registry "foo.com/bar:sometag": cannot have an associated tag or digest`,
	}, {
		testName:        "SingleCatchAll",
		catchAllDefault: "registry.cue.works",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.cue.works",
			},
		},
	}, {
		testName: "CatchAllWithNoDefault",
		s:        "registry.cue.works",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.cue.works",
			},
		},
	}, {
		testName:        "CatchAllWithDefault",
		s:               "registry.cue.works",
		catchAllDefault: "other.cue.works",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.cue.works",
			},
			"": {
				Host: "registry.cue.works",
			},
		},
	}, {
		testName: "PrefixWithCatchAllNoDefault",
		s:        "example.com=registry.example.com/offset,registry.cue.works",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.cue.works",
			},
			"example.com/blah": {
				Host:   "registry.example.com",
				Prefix: "offset",
			},
			"example.com": {
				Host:   "registry.example.com",
				Prefix: "offset",
			},
		},
	}, {
		testName:        "PrefixWithCatchAllDefault",
		s:               "example.com=registry.example.com/offset",
		catchAllDefault: "registry.cue.works",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.cue.works",
			},
			"example.com/blah": {
				Host:   "registry.example.com",
				Prefix: "offset",
			},
		},
	}, {
		testName: "LocalhostIsInsecure",
		s:        "localhost:5000",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host:     "localhost:5000",
				Insecure: true,
			},
		},
	}, {
		testName: "SecureLocalhost",
		s:        "localhost:1234+secure",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "localhost:1234",
			},
		},
	}, {
		testName: "127.0.0.1IsInsecure",
		s:        "127.0.0.1",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host:     "127.0.0.1",
				Insecure: true,
			},
		},
	}, {
		testName: "[::1]IsInsecure",
		s:        "[::1]",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host:     "[::1]",
				Insecure: true,
			},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			r, err := ParseCUERegistry(tc.s, tc.catchAllDefault)
			if tc.err != "" {
				qt.Assert(t, qt.ErrorMatches(err, tc.err))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			for prefix, want := range tc.lookups {
				got := r.Resolve(prefix)
				qt.Assert(t, qt.Equals(got, want), qt.Commentf("prefix %q", prefix))
			}
		})
	}
}
