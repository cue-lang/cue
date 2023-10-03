package cmd

import (
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/internal/tdtest"
)

type registryTest struct {
	registry     string
	wantHost     string
	wantPrefix   string
	wantInsecure bool
	wantError    string
}

var parseRegistryTests = []registryTest{{
	registry: "registry.cuelabs.dev",
	wantHost: "registry.cuelabs.dev",
}, {
	registry:     "registry.cuelabs.dev+insecure",
	wantHost:     "registry.cuelabs.dev",
	wantInsecure: true,
}, {
	registry:     "foo.com/bar/baz",
	wantHost:     "foo.com",
	wantPrefix:   "bar/baz",
	wantInsecure: false,
}, {
	registry:     "localhost:8080/blah",
	wantHost:     "localhost:8080",
	wantPrefix:   "blah",
	wantInsecure: true,
}, {
	registry:  "localhost/blah",
	wantError: `cannot parse \$CUE_REGISTRY: reference does not contain host name`,
}, {
	registry:     "127.0.0.1/blah",
	wantHost:     "127.0.0.1",
	wantPrefix:   "blah",
	wantInsecure: true,
}, {
	registry:     "localhost:1324",
	wantHost:     "localhost:1324",
	wantInsecure: true,
}, {
	registry:  "foo.com/bar:1324",
	wantError: `\$CUE_REGISTRY "foo.com/bar:1324" cannot have an associated tag or digest`,
}, {
	registry:  "foo.com/bar@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
	wantError: `\$CUE_REGISTRY "foo.com/bar@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae" cannot have an associated tag or digest`,
}, {
	registry:  "foo.com/bar:blah@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
	wantError: `\$CUE_REGISTRY "foo.com/bar:blah@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae" cannot have an associated tag or digest`,
}, {
	registry:  "foo.com/bar+baz",
	wantError: `unknown suffix \("\+baz"\) to CUE_REGISTRY \(need \+insecure or \+secure\)`,
}, {
	registry:  "badhost",
	wantError: `\$CUE_REGISTRY "badhost" is not a valid host name`,
}}

func TestParseRegistry(t *testing.T) {
	tdtest.Run(t, parseRegistryTests, func(t *tdtest.T, test *registryTest) {
		host, prefix, insecure, err := parseRegistry(test.registry)
		if test.wantError != "" {
			qt.Assert(t, qt.ErrorMatches(err, test.wantError))
			return
		}
		qt.Assert(t, qt.IsNil(err))
		t.Equal(host, test.wantHost)
		t.Equal(prefix, test.wantPrefix)
		t.Equal(insecure, test.wantInsecure)
	})
}
