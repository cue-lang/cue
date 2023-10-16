package modresolve

import (
	"fmt"
	"net"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ociref"

	"cuelang.org/go/internal/mod/module"
)

// Resolve resolves a module path (a.k.a. OCI repository name) to the
// location for that path. Invalid paths will map to the default location.
type Resolver interface {
	Resolve(path string) Location
}

// Location represents the location for a given path.
type Location struct {
	// Host holds the host or host:port of the registry.
	Host string
	// Prefix holds a prefix to be added to the path.
	Prefix string
	// Insecure holds whether an insecure connection
	// should be used when connecting to the registry.
	Insecure bool
}

// ParseCUERegistry parses a registry routing specification that
// maps module prefixes to the registry that should be used to
// fetch that module.
//
// The specification consists of an order-independent, comma-separated list.
//
// Each element either maps a module prefix to the registry that will be used
// for all modules that have that prefix (prefix=registry), or a catch-all registry to be used
// for modules that do not match any prefix (registry).
//
// For example:
//
//	myorg.com=myregistry.com/m,catchallregistry.example.org
//
// Any module with a matching prefix will be routed to the given registry.
// A prefix only matches whole path elements.
// In the above example, module myorg.com/foo/bar@v0 will be looked up
// in myregistry.com in the repository m/myorg.com/foo/bar,
// whereas github.com/x/y will be looked up in catchallregistry.example.com.
//
// The registry part is syntactically similar to a [docker reference]
// except that the repository is optional and no tag or digest is allowed.
// Additionally, a +secure or +insecure suffix may be used to indicate
// whether to use a secure or insecure connection. Without that,
// localhost, 127.0.0.1 and [::1] will default to insecure, and anything
// else to secure.
//
// If s does not declare a catch-all registry location, catchAllDefault is
// used. It is an error if s fails to declares a catch-all registry location
// and no catchAllDefault is provided.
//
// [docker reference]: https://pkg.go.dev/github.com/distribution/reference
func ParseCUERegistry(s string, catchAllDefault string) (Resolver, error) {
	if s == "" && catchAllDefault == "" {
		return nil, fmt.Errorf("no catch-all registry or default")
	}
	locs := make(map[string]Location)
	if s == "" {
		s = catchAllDefault
	}
	parts := strings.Split(s, ",")
	for _, part := range parts {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			if part == "" {
				// TODO or just ignore it?
				return nil, fmt.Errorf("empty registry part")
			}
			if _, ok := locs[""]; ok {
				return nil, fmt.Errorf("duplicate catch-all registry")
			}
			key, val = "", part
		} else {
			if key == "" {
				return nil, fmt.Errorf("empty module prefix")
			}
			if val == "" {
				return nil, fmt.Errorf("empty registry reference")
			}
			if err := module.CheckPathWithoutVersion(key); err != nil {
				return nil, fmt.Errorf("invalid module path %q: %v", key, err)
			}
			if _, ok := locs[key]; ok {
				return nil, fmt.Errorf("duplicate module prefix %q", key)
			}
		}
		loc, err := parseRegistry(val)
		if err != nil {
			return nil, fmt.Errorf("invalid registry %q: %v", val, err)
		}
		locs[key] = loc
	}
	if _, ok := locs[""]; !ok {
		if catchAllDefault == "" {
			return nil, fmt.Errorf("no default catch-all registry provided")
		}
		loc, err := parseRegistry(catchAllDefault)
		if err != nil {
			return nil, fmt.Errorf("invalid catch-all registry %q: %v", catchAllDefault, err)
		}
		locs[""] = loc
	}
	return &resolver{
		locs: locs,
	}, nil
}

type resolver struct {
	locs map[string]Location
}

func (r *resolver) Resolve(path string) Location {
	if path == "" {
		return r.locs[""]
	}
	bestMatch := ""
	// Note: there's always a wildcard match.
	bestMatchLoc := r.locs[""]
	for pat, loc := range r.locs {
		if pat == path {
			return loc
		}
		if !strings.HasPrefix(path, pat) {
			continue
		}
		if len(bestMatch) > len(pat) {
			// We've already found a more specific match.
			continue
		}
		if path[len(pat)] != '/' {
			// The path doesn't have a separator at the end of
			// the prefix, which means that it doesn't match.
			// For example, foo.com/bar does not match foo.com/ba.
			continue
		}
		// It's a possible match but not necessarily the longest one.
		bestMatch, bestMatchLoc = pat, loc
	}
	return bestMatchLoc
}

func parseRegistry(env string) (Location, error) {
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
		if !ociref.IsValidHost(r.Host) {
			return Location{}, fmt.Errorf("invalid host name %q in registry", r.Host)
		}
	} else {
		var err error
		r, err = ociref.Parse(env)
		if err != nil {
			return Location{}, err
		}
		if r.Tag != "" || r.Digest != "" {
			return Location{}, fmt.Errorf("cannot have an associated tag or digest")
		}
	}
	if suffix == "" {
		if isInsecureHost(r.Host) {
			suffix = "+insecure"
		} else {
			suffix = "+secure"
		}
	}
	insecure := false
	switch suffix {
	case "+insecure":
		insecure = true
	case "+secure":
	default:
		return Location{}, fmt.Errorf("unknown suffix (%q), need +insecure, +secure or no suffix)", suffix)
	}
	return Location{
		Host:     r.Host,
		Prefix:   r.Repository,
		Insecure: insecure,
	}, nil
}

func isInsecureHost(hostPort string) bool {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		host = hostPort
	}
	switch host {
	case "localhost",
		"127.0.0.1",
		"::1", "[::1]":
		return true
	}
	// TODO other clients have logic for RFC1918 too, amongst other
	// things. Maybe we should do that too.
	return false
}
