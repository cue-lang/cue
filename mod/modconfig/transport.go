package modconfig

import (
	"maps"
	"net/http"

	"cuelang.org/go/internal/cueversion"
)

// newUserAgentTransport returns an [http.RoundTripper] implementation
// that wraps next and adds a "User-Agent" header to every
// HTTP request containing the result of [cueversion.UserAgent](clientType).
// If next is nil, [http.DefaultTransport] will be used.
func newUserAgentTransport(clientType string, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &userAgentTransport{
		next:      next,
		userAgent: cueversion.UserAgent(clientType),
	}
}

type userAgentTransport struct {
	next      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTrip isn't allowed to modify the request, but we
	// can avoid doing a full clone.
	req1 := *req
	req1.Header = maps.Clone(req.Header)
	req1.Header.Set("User-Agent", t.userAgent)
	return t.next.RoundTrip(&req1)
}
