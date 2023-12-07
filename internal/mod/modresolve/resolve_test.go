// Copyright 2023 CUE Authors
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

package modresolve

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestResolver(t *testing.T) {
	testCases := []struct {
		testName        string
		in              string
		catchAllDefault string
		err             string
		lookups         map[string]Location
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
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.somewhere",
			},
		},
	}, {
		testName: "CatchAllWithNoDefault",
		in:       "registry.somewhere",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.somewhere",
			},
		},
	}, {
		testName:        "CatchAllWithDefault",
		in:              "registry.somewhere",
		catchAllDefault: "other.cue.somewhere",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.somewhere",
			},
			"": {
				Host: "registry.somewhere",
			},
		},
	}, {
		testName: "PrefixWithCatchAllNoDefault",
		in:       "example.com=registry.example.com/offset,registry.somewhere",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.somewhere",
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
		in:              "example.com=registry.example.com/offset",
		catchAllDefault: "registry.somewhere",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "registry.somewhere",
			},
			"example.com/blah": {
				Host:   "registry.example.com",
				Prefix: "offset",
			},
		},
	}, {
		testName: "LocalhostIsInsecure",
		in:       "localhost:5000",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host:     "localhost:5000",
				Insecure: true,
			},
		},
	}, {
		testName: "SecureLocalhost",
		in:       "localhost:1234+secure",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host: "localhost:1234",
			},
		},
	}, {
		testName: "127.0.0.1IsInsecure",
		in:       "127.0.0.1",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host:     "127.0.0.1",
				Insecure: true,
			},
		},
	}, {
		testName: "[::1]IsInsecure",
		in:       "[::1]",
		lookups: map[string]Location{
			"fruit.com/apple": {
				Host:     "[::1]",
				Insecure: true,
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
			for prefix, want := range tc.lookups {
				got := r.Resolve(prefix)
				qt.Assert(t, qt.Equals(got, want), qt.Commentf("prefix %q", prefix))
			}
		})
	}
}
