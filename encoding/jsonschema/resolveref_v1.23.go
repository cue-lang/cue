//go:build go1.23

// TODO(go1.12) remove this file.

package jsonschema

import "net/url"

func resolveReference(u, ref *url.URL) *url.URL {
	return u.ResolveReference(ref)
}
