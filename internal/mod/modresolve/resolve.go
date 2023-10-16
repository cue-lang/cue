package modresolve

import (
	"fmt"
	"net"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ociref"

	"cuelang.org/go/internal/mod/module"
)

type Resolver interface {
	Resolve(path string) Location
}

type Location struct {
	Host     string
	Prefix   string
	Insecure bool
}

// for example:
//	CUE_REGISTRY=my.org/foo=myregistry.org/cuemodules,registry.cuelang.org,test.example.com,mylocalhost+insecure

func ParseCUERegistry(s string, catchAllDefault string) (Resolver, error) {
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
				return nil, fmt.Errorf("duplicate match-all registry")
			}
			key, val = "", part
		} else {
			if key == "" {
				return nil, fmt.Errorf("empty module pattern")
			}
			if val == "" {
				return nil, fmt.Errorf("empty registry reference")
			}
			if err := module.CheckPath(key); err != nil {
				return nil, fmt.Errorf("invalid module path %q: %v", key, err)
			}
			if _, ok := locs[key]; ok {
				return nil, fmt.Errorf("duplicate module pattern %q", key)
			}
		}
		loc, err := parseRegistry(val)
		if err != nil {
			return nil, err
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
			// the pattern, which means that it doesn't match.
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
			return Location{}, fmt.Errorf("$CUE_REGISTRY %q is not a valid host name", r.Host)
		}
	} else {
		var err error
		r, err = ociref.Parse(env)
		if err != nil {
			return Location{}, fmt.Errorf("cannot parse $CUE_REGISTRY: %v", err)
		}
		if r.Tag != "" || r.Digest != "" {
			return Location{}, fmt.Errorf("$CUE_REGISTRY %q cannot have an associated tag or digest", env)
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
		return Location{}, fmt.Errorf("unknown suffix (%q) to CUE_REGISTRY (need +insecure or +secure)", suffix)
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
		"::1":
		return true
	}
	// TODO other clients have logic for RFC1918 too, amongst other
	// things. Maybe we should do that too.
	return false
}
