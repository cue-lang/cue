package cueversion

import (
	"maps"
	"net/http"
)

// NewTransport returns an [http.RoundTripper] implementation
// that wraps t and adds a "User-Agent" header to every
// HTTP request containing the result of UserAgent(clientType).
// If t is nil, [http.DefaultTransport] will be used.
func NewTransport(clientType string, t http.RoundTripper) http.RoundTripper {
	if t == nil {
		t = http.DefaultTransport
	}
	return &userAgentTransport{
		transport: t,
		userAgent: UserAgent(clientType),
	}
}

type userAgentTransport struct {
	transport http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTrip isn't allowed to modify the request, but we
	// can avoid doing a full clone.
	req1 := *req
	req1.Header = maps.Clone(req.Header)
	req1.Header.Set("User-Agent", t.userAgent)
	return t.transport.RoundTrip(&req1)
}
