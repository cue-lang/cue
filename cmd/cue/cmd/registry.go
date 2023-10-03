package cmd

import (
	"fmt"
	"net"
	"os"
	"strings"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelabs.dev/go/oci/ociregistry/ocifilter"
	"cuelabs.dev/go/oci/ociregistry/ociref"

	"cuelang.org/go/internal/cueexperiment"
)

func getRegistry() (ociregistry.Interface, error) {
	if !cueexperiment.Flags.Modules {
		return nil, nil
	}
	// TODO document CUE_REGISTRY via a new "cue help environment" subcommand.
	env := os.Getenv("CUE_REGISTRY")
	if env == "" {
		env = "registry.cuelabs.dev"
	}
	host, prefix, insecure, err := parseRegistry(env)
	if err != nil {
		return nil, err
	}
	r, err := ociclient.New(host, &ociclient.Options{
		Insecure: insecure,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot make OCI client: %v", err)
	}
	if prefix != "" {
		r = ocifilter.Sub(r, prefix)
	}
	return r, nil
}

func parseRegistry(env string) (hostPort, prefix string, insecure bool, err error) {
	var suffix string
	if i := strings.LastIndex(env, "+"); i > 0 {
		suffix = env[i:]
		env = env[:i]
	}
	var r ociref.Reference
	if !strings.Contains(env, "/") {
		// OCI references don't allow a host name on its own without a repo,
		// but we do.
		r.Host = env
	} else {
		var err error
		r, err = ociref.Parse(env)
		if err != nil {
			return "", "", false, fmt.Errorf("cannot parse $CUE_REGISTRY: %v", err)
		}
		if r.Tag != "" || r.Digest != "" {
			return "", "", false, fmt.Errorf("$CUE_REGISTRY cannot have associated tag or digest")
		}
	}
	if suffix == "" {
		if isInsecureHost(r.Host) {
			suffix = "+insecure"
		} else {
			suffix = "+secure"
		}
	}
	switch suffix {
	case "+insecure":
		insecure = true
	case "+secure":
	default:
		return "", "", false, fmt.Errorf("unknown suffix (%q) to CUE_REGISTRY (need +insecure or +secure)", suffix)
	}
	return r.Host, r.Repository, insecure, nil
}

func isInsecureHost(hostPort string) bool {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
	}
	switch host {
	case "localhost",
		"127.0.0.1",
		"::1":
		return true
	}
	// TODO other clients have logic for RFC1918 too, amongst other
	// things. Maybe we should do that too.
	return false
}
