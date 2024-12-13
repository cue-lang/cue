//go:build !go1.23

package jsonschema

import "net/url"

// resolveReference is exactly like [url.URL.ResolveReference]
// except that it fixes https://go.dev/issue/66084, which
// has been fixed in Go 1.23 (https://go.dev/cl/572915) but not go1.22
// TODO(go1.23) remove this and use ResolveReference directly]
func resolveReference(u, ref *url.URL) *url.URL {
	if !hitsBug(u, ref) {
		return u.ResolveReference(ref)
	}
	url := *ref
	if ref.Scheme == "" {
		url.Scheme = u.Scheme
	}
	if ref.Path == "" && !ref.ForceQuery && ref.RawQuery == "" {
		url.RawQuery = u.RawQuery
		if ref.Fragment == "" {
			url.Fragment = u.Fragment
			url.RawFragment = u.RawFragment
		}
	}
	url.Opaque = u.Opaque
	url.User = nil
	url.Host = ""
	url.Path = ""
	return &url
}

// This mirrors the structure of the stdlib [url.URL.ResolveReference]
// method.
func hitsBug(u, ref *url.URL) bool {
	if ref.Scheme != "" || ref.Host != "" || ref.User != nil {
		return false
	}
	if ref.Opaque != "" {
		return false
	}
	if ref.Path == "" && u.Opaque != "" {
		return true
	}
	return false
}
