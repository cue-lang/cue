package modresolve

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestParseCUERegistry(t *testing.T) {
	testCases := []struct {
		testName        string
		in              string
		catchAllDefault string
		err             string
		lookups         map[string]*Location
	}{{
		testName: "MultipleFallbacks",
		in:       "registry.somewhere,registry.other",
		err:      "duplicate catch-all registry",
	}, {
		testName:        "NoRegistryOrDefault",
		catchAllDefault: "",
		err:             "no catch-all registry or default",
	}, {
		testName: "InvalidRegistry",
		in:       "$#foo",
		err:      `invalid registry "\$#foo": invalid host name "\$#foo" in registry`,
	}, {
		testName: "InvalidSecuritySuffix",
		in:       "foo.com+bogus",
		err:      `invalid registry "foo.com\+bogus": unknown suffix \("\+bogus"\), need \+insecure, \+secure or no suffix\)`,
	}, {
		testName: "IPV6AddrWithoutBrackets",
		in:       "::1",
		err:      `invalid registry "::1": invalid host name "::1" in registry`,
	}, {
		testName: "EmptyElement",
		in:       "foo.com,",
		err:      `empty registry part`,
	}, {
		testName: "MissingPrefix",
		in:       "=foo.com",
		err:      `empty module prefix`,
	}, {
		testName: "MissingRegistry",
		in:       "x.com=",
		err:      `empty registry reference`,
	}, {
		testName: "InvalidModulePrefix",
		in:       "foo#=foo.com",
		err:      `invalid module path "foo#": invalid char '#'`,
	}, {
		testName: "DuplicateModulePrefix",
		in:       "x.com=r.org,x.com=q.org",
		err:      `duplicate module prefix "x.com"`,
	}, {
		testName: "NoDefaultCatchAll",
		in:       "x.com=r.org",
		err:      `no default catch-all registry provided`,
	}, {
		testName:        "InvalidCatchAll",
		in:              "x.com=r.org",
		catchAllDefault: "bogus",
		err:             `invalid catch-all registry "bogus": invalid host name "bogus" in registry`,
	}, {
		testName: "InvalidRegistryRef",
		in:       "foo.com//bar",
		err:      `invalid registry "foo.com//bar": invalid reference syntax \("foo.com//bar"\)`,
	}, {
		testName: "RegistryRefWithDigest",
		in:       "foo.com/bar@sha256:f3c16f525a1b7c204fc953d6d7db7168d84ebf4902f83c3a37d113b18c28981f",
		err:      `invalid registry "foo.com/bar@sha256:f3c16f525a1b7c204fc953d6d7db7168d84ebf4902f83c3a37d113b18c28981f": cannot have an associated tag or digest`,
	}, {
		testName: "RegistryRefWithTag",
		in:       "foo.com/bar:sometag",
		err:      `invalid registry "foo.com/bar:sometag": cannot have an associated tag or digest`,
	}, {
		testName:        "SingleCatchAll",
		catchAllDefault: "registry.somewhere",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "registry.somewhere",
				Repository: "fruit.com/apple",
			},
		},
	}, {
		testName: "CatchAllWithNoDefault",
		in:       "registry.somewhere",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "registry.somewhere",
				Repository: "fruit.com/apple",
			},
		},
	}, {
		testName:        "CatchAllWithDefault",
		in:              "registry.somewhere",
		catchAllDefault: "other.cue.somewhere",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Repository: "fruit.com/apple",
				Host:       "registry.somewhere",
			},
			"": nil,
		},
	}, {
		testName: "PrefixWithCatchAllNoDefault",
		in:       "example.com=registry.example.com/offset,registry.somewhere",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "registry.somewhere",
				Repository: "fruit.com/apple",
			},
			"example.com/blah": {
				Host:       "registry.example.com",
				Repository: "offset/example.com/blah",
			},
			"example.com": {
				Host:       "registry.example.com",
				Repository: "offset/example.com",
			},
		},
	}, {
		testName:        "PrefixWithCatchAllDefault",
		in:              "example.com=registry.example.com/offset",
		catchAllDefault: "registry.somewhere",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "registry.somewhere",
				Repository: "fruit.com/apple",
			},
			"example.com/blah": {
				Host:       "registry.example.com",
				Repository: "offset/example.com/blah",
			},
		},
	}, {
		testName: "LocalhostIsInsecure",
		in:       "localhost:5000",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "localhost:5000",
				Insecure:   true,
				Repository: "fruit.com/apple",
			},
		},
	}, {
		testName: "SecureLocalhost",
		in:       "localhost:1234+secure",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "localhost:1234",
				Repository: "fruit.com/apple",
			},
		},
	}, {
		testName: "127.0.0.1IsInsecure",
		in:       "127.0.0.1",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "127.0.0.1",
				Insecure:   true,
				Repository: "fruit.com/apple",
			},
		},
	}, {
		testName: "[::1]IsInsecure",
		in:       "[::1]",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "[::1]",
				Insecure:   true,
				Repository: "fruit.com/apple",
			},
		},
	}, {
		testName: "[0:0::1]IsInsecure",
		in:       "[0:0::1]",
		lookups: map[string]*Location{
			"fruit.com/apple": {
				Host:       "[0:0::1]",
				Insecure:   true,
				Repository: "fruit.com/apple",
			},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			r, err := ParseCUERegistry(tc.in, tc.catchAllDefault)
			if tc.err != "" {
				qt.Assert(t, qt.ErrorMatches(err, tc.err))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			testLookups(t, r, tc.lookups)
		})
	}
}

func TestParseConfig(t *testing.T) {
	testCases := []struct {
		testName        string
		in              string
		catchAllDefault string
		err             string
		lookups         map[string]*Location
	}{{
		testName:        "NoRegistryOrDefault",
		catchAllDefault: "",
		err:             "no default catch-all registry provided",
	}, {
		testName: "InvalidRegistry",
		in: `
defaultRegistry: host: "$#foo"
`,
		err: `invalid default registry configuration: invalid host name "\$#foo"`,
	}, {
		testName: "EncHashAsRepo",
		in: `
defaultRegistry: {
	host: "registry.somewhere"
	repository: "hello"
	pathEncoding: "hashAsRepo"
	prefixForTags: "mod-"
}
`,
		lookups: map[string]*Location{
			"foo.com/bar v1.2.3": {
				Host:       "registry.somewhere",
				Repository: "hello/" + hashOf("foo.com/bar"),
				Tag:        "v1.2.3",
			},
		},
	}, {
		testName: "EncHashAsTag",
		in: `
defaultRegistry: {
	host: "registry.somewhere"
	repository: "hello"
	pathEncoding: "hashAsTag"
	prefixForTags: "mod-"
}
`,
		lookups: map[string]*Location{
			"foo.com/bar v1.2.3": {
				Host:       "registry.somewhere",
				Repository: "hello",
				Tag:        hashOf("foo.com/bar") + "-v1.2.3",
			},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			r, err := ParseConfig([]byte(tc.in), "somefile.cue", tc.catchAllDefault)
			if tc.err != "" {
				qt.Assert(t, qt.ErrorMatches(err, tc.err))
				return
			}
			qt.Assert(t, qt.IsNil(err))
			testLookups(t, r, tc.lookups)
		})
	}
}

func testLookups(t *testing.T, r Resolver, lookups map[string]*Location) {
	for key, want := range lookups {
		t.Run(key, func(t *testing.T) {
			m, v, _ := strings.Cut(key, " ")
			got, ok := r.Resolve(m, v)
			if want == nil {
				qt.Assert(t, qt.IsFalse(ok))
			} else {
				qt.Assert(t, qt.DeepEquals(&got, want))
			}
		})
	}
}

func hashOf(s string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
}
