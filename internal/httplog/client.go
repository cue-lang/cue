package httplog

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// DefaultMaxBodySize holds the maximum body size to include in
// logged requests when [TransportConfig.MaxBodySize] is <=0.
const DefaultMaxBodySize = 1024

// TransportConfig holds configuration for [Transport].
type TransportConfig struct {
	// Logger is used to log the requests. If it is nil,
	// the zero [SlogLogger] will be used.
	Logger Logger

	// Transport is used as the underlying transport for
	// making HTTP requests. If it is nil,
	// [http.DefaultTransport] will be used.
	Transport http.RoundTripper

	// IncludeAllQueryParams causes all URL query parameters to be included
	// rather than redacted using [RedactedURL].
	IncludeAllQueryParams bool

	// MaxBodySize holds the maximum size of body data to include
	// in the logged data. When a body is larger than this, only this
	// amount of body will be included, and the "BodyTruncated"
	// field will be set to true to indicate that this happened.
	//
	// If this is <=0, DefaultMaxBodySize will be used.
	// Use [RedactRequestBody] or [RedactResponseBody]
	// to cause body data to be omitted entirely.
	MaxBodySize int
}

// Transport returns an [http.RoundTripper] implementation that
// logs HTTP requests. If cfg0 is nil, it's equivalent to a pointer
// to a zero-valued [TransportConfig].
func Transport(cfg0 *TransportConfig) http.RoundTripper {
	var cfg TransportConfig
	if cfg0 != nil {
		cfg = *cfg0
	}
	if cfg.Logger == nil {
		cfg.Logger = SlogLogger{}
	}
	if cfg.Transport == nil {
		cfg.Transport = http.DefaultTransport
	}
	if cfg.MaxBodySize <= 0 {
		cfg.MaxBodySize = DefaultMaxBodySize
	}
	return &loggingTransport{
		cfg: cfg,
	}
}

type loggingTransport struct {
	cfg TransportConfig
}

var seq atomic.Int64

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	id := seq.Add(1)
	var reqURL string
	if t.cfg.IncludeAllQueryParams {
		reqURL = req.URL.String()
	} else {
		reqURL = RedactedURL(ctx, req.URL).String()
	}
	t.cfg.Logger.Log(ctx, KindClientSendRequest, fromHTTPRequest(ctx, id, reqURL, req, true, t.cfg.MaxBodySize))
	resp, err := t.cfg.Transport.RoundTrip(req)
	if err != nil {
		t.cfg.Logger.Log(ctx, KindClientRecvResponse, &Response{
			ID:     id,
			Method: req.Method,
			URL:    reqURL,
			Error:  err.Error(),
		})
		return nil, err
	}
	logResp := &Response{
		ID:         id,
		Method:     req.Method,
		URL:        reqURL,
		Header:     resp.Header,
		StatusCode: resp.StatusCode,
	}
	setBodyData(ctx, &logResp.BodyData, &resp.Body, true, false, t.cfg.MaxBodySize)
	t.cfg.Logger.Log(ctx, KindClientRecvResponse, logResp)
	return resp, nil
}

func fromHTTPRequest(ctx context.Context, id int64, reqURL string, req *http.Request, closeBody bool, maxBodySize int) *Request {
	logReq := &Request{
		ID:            id,
		URL:           reqURL,
		Method:        req.Method,
		Header:        redactAuthorization(req.Header),
		ContentLength: req.ContentLength,
	}
	setBodyData(ctx, &logReq.BodyData, &req.Body, closeBody, true, maxBodySize)
	return logReq
}

func redactAuthorization(h http.Header) http.Header {
	auths, ok := h["Authorization"]
	if !ok {
		return h
	}
	h = maps.Clone(h) // shallow copy
	auths = slices.Clone(auths)
	for i, auth := range auths {
		if kind, _, ok := strings.Cut(auth, " "); ok && (kind == "Basic" || kind == "Bearer") {
			auths[i] = kind + " REDACTED"
		} else {
			auths[i] = "REDACTED"
		}
	}
	h["Authorization"] = auths
	return h
}

func setBodyData(ctx context.Context, body *BodyData, r *io.ReadCloser, needClose, isRequest bool, maxBodySize int) {
	if *r == nil {
		return
	}
	if reason := shouldRedactBody(ctx, isRequest); reason != "" {
		body.BodyRedactedBecause = reason
		return
	}
	data, err := ioutil.ReadAll(io.LimitReader(*r, int64(maxBodySize+1)))
	if len(data) > maxBodySize {
		body.BodyTruncated = true
		*r = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.MultiReader(
				bytes.NewReader(data),
				*r,
			),
			Closer: *r,
		}
		data = data[:maxBodySize]
	} else {
		if err != nil {
			body.BodyTruncated = true
		}
		if needClose {
			(*r).Close()
		}
		*r = ioutil.NopCloser(bytes.NewReader(data))
	}
	if utf8.Valid(data) {
		body.Body = string(data)
	} else {
		body.Body64 = data
	}
}

// RedactedURL returns u with query parameters redacted according
// to [ContextWithAllowedURLQueryParams].
// If there is no allow function associated with the context,
// all query parameters will be redacted.
func RedactedURL(ctx context.Context, u *url.URL) *url.URL {
	if u.RawQuery == "" {
		return u
	}
	qs := u.Query()
	allow := queryParamChecker(ctx)
	changed := false
	for k, v := range qs {
		if allow(k) {
			continue
		}
		changed = true
		for i := range v {
			v[i] = "REDACTED"
		}
	}
	if !changed {
		return u
	}
	r := *u
	r.RawQuery = qs.Encode()
	return &r
}
