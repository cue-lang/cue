package httplog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestTransportWithSlog(t *testing.T) {
	seq.Store(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("hello"))
	}))
	var buf strings.Builder

	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger: SlogLogger{
				Logger: slog.New(slog.NewJSONHandler(&buf, nil)),
			},
		}),
	}
	resp, err := client.Get(srv.URL + "/foo/bar?foo=bar")
	qt.Assert(t, qt.IsNil(err))
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	qt.Assert(t, qt.Equals(string(data), "hello"))

	qt.Assert(t, qt.Matches(buf.String(), `
{"time":"\d\d\d\d-[^"]+","level":"INFO","msg":"http client->","info":{"id":1,"method":"GET","url":"http://[^/]+/foo/bar\?foo=REDACTED","contentLength":0,"header":{}}}
{"time":"\d\d\d\d-[^"]+","level":"INFO","msg":"http client<-","info":{"id":1,"method":"GET","url":"http://[^/]+/foo/bar\?foo=REDACTED","statusCode":200,"header":{"Content-Length":\["5"\],"Content-Type":\["text/plain; charset=utf-8"\],"Date":\[.*\]},"body":"hello"}}
`[1:]))
}

func TestQueryParamAllowList(t *testing.T) {
	seq.Store(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger: &recorder,
		}),
	}
	ctx := ContextWithAllowedURLQueryParams(context.Background(),
		func(key string) bool {
			return key == "x1"
		},
	)
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo/bar?x1=ok&x2=redact1&x2=redact2", nil)
	qt.Assert(t, qt.IsNil(err))
	resp, err := client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	resp.Body.Close()
	req, err = http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo/bar?x1=ok1&x1=ok2", nil)
	qt.Assert(t, qt.IsNil(err))
	resp, err = client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	resp.Body.Close()
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{
		KindClientSendRequest,
		KindClientRecvResponse,
		KindClientSendRequest,
		KindClientRecvResponse,
	}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:     11,
			Method: "GET",
			URL:    "http://localhost/foo/bar?x1=ok&x2=REDACTED&x2=REDACTED",
			Header: http.Header{},
		},
		&Response{
			ID:         11,
			Method:     "GET",
			URL:        "http://localhost/foo/bar?x1=ok&x2=REDACTED&x2=REDACTED",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"0"},
				"Date":           {"now"},
			},
		},
		&Request{
			ID:     12,
			Method: "GET",
			URL:    "http://localhost/foo/bar?x1=ok1&x1=ok2",
			Header: http.Header{},
		},
		&Response{
			ID:         12,
			Method:     "GET",
			URL:        "http://localhost/foo/bar?x1=ok1&x1=ok2",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"0"},
				"Date":           {"now"},
			},
		},
	}))
}

func TestAuthorizationHeaderRedacted(t *testing.T) {
	seq.Store(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger: &recorder,
		}),
	}
	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo", nil)
	qt.Assert(t, qt.IsNil(err))
	req.SetBasicAuth("someuser", "somepassword")
	req.Header.Add("Authorization", "Bearer sensitive-info")
	req.Header.Add("Authorization", "othertoken")

	resp, err := client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	resp.Body.Close()
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{
		KindClientSendRequest,
		KindClientRecvResponse,
	}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:     11,
			Method: "GET",
			URL:    "http://localhost/foo",
			Header: http.Header{
				"Authorization": {
					"Basic REDACTED",
					"Bearer REDACTED",
					"REDACTED",
				},
			},
		},
		&Response{
			ID:         11,
			Method:     "GET",
			URL:        "http://localhost/foo",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"0"},
				"Date":           {"now"},
			},
		},
	}))
}

func TestIncludeAllQueryParams(t *testing.T) {
	seq.Store(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger:                &recorder,
			IncludeAllQueryParams: true,
		}),
	}
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo/bar?x1=ok&x2=redact1&x2=redact2", nil)
	qt.Assert(t, qt.IsNil(err))
	resp, err := client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	resp.Body.Close()
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{KindClientSendRequest, KindClientRecvResponse}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:     11,
			Method: "GET",
			URL:    "http://localhost/foo/bar?x1=ok&x2=redact1&x2=redact2",
			Header: http.Header{},
		},
		&Response{
			ID:         11,
			Method:     "GET",
			URL:        "http://localhost/foo/bar?x1=ok&x2=redact1&x2=redact2",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"0"},
				"Date":           {"now"},
			},
		},
	}))
}

func TestOmitBody(t *testing.T) {
	seq.Store(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("response body"))
	}))

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger: &recorder,
		}),
	}
	ctx := context.Background()
	ctx = RedactRequestBody(ctx, "not keen on request bodies")
	ctx = RedactResponseBody(ctx, "response bodies are right out")
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo/bar", strings.NewReader("request body"))
	qt.Assert(t, qt.IsNil(err))
	resp, err := client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	resp.Body.Close()
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{KindClientSendRequest, KindClientRecvResponse}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:            11,
			Method:        "GET",
			ContentLength: 12,
			URL:           "http://localhost/foo/bar",
			Header:        http.Header{},
			BodyData: BodyData{
				BodyRedactedBecause: "not keen on request bodies",
			},
		},
		&Response{
			ID:         11,
			Method:     "GET",
			URL:        "http://localhost/foo/bar",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"13"},
				"Content-Type":   {"text/plain; charset=utf-8"},
				"Date":           {"now"},
			},
			BodyData: BodyData{
				BodyRedactedBecause: "response bodies are right out",
			},
		},
	}))
}

func TestLongBody(t *testing.T) {
	seq.Store(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		qt.Check(t, qt.IsNil(err))
		qt.Check(t, qt.Equals(string(data), strings.Repeat("a", 30)))
		w.Write(bytes.Repeat([]byte("b"), 20))
	}))

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger:      &recorder,
			MaxBodySize: 10,
		}),
	}
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo/bar", strings.NewReader(strings.Repeat("a", 30)))
	qt.Assert(t, qt.IsNil(err))
	resp, err := client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	data, err := io.ReadAll(resp.Body)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(data), strings.Repeat("b", 20)))
	resp.Body.Close()
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{KindClientSendRequest, KindClientRecvResponse}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:            11,
			Method:        "GET",
			ContentLength: 30,
			URL:           "http://localhost/foo/bar",
			Header:        http.Header{},
			BodyData: BodyData{
				Body:          strings.Repeat("a", 10),
				BodyTruncated: true,
			},
		},
		&Response{
			ID:         11,
			Method:     "GET",
			URL:        "http://localhost/foo/bar",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"20"},
				"Content-Type":   {"text/plain; charset=utf-8"},
				"Date":           {"now"},
			},
			BodyData: BodyData{
				Body:          strings.Repeat("b", 10),
				BodyTruncated: true,
			},
		},
	}))
}

func TestRoundTripError(t *testing.T) {
	seq.Store(10)

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Transport: errorTransport{},
			Logger:    &recorder,
		}),
	}
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:1234/foo/bar", nil)
	qt.Assert(t, qt.IsNil(err))
	_, err = client.Do(req)
	qt.Assert(t, qt.ErrorMatches(err, `Get "http://localhost:1234/foo/bar": error in RoundTrip`))
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{KindClientSendRequest, KindClientRecvResponse}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:     11,
			Method: "GET",
			URL:    "http://localhost/foo/bar",
			Header: http.Header{},
		},
		&Response{
			ID:     11,
			Method: "GET",
			URL:    "http://localhost/foo/bar",
			Error:  "error in RoundTrip",
		},
	}))
}

func TestBodyBinaryData(t *testing.T) {
	seq.Store(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		qt.Check(t, qt.IsNil(err))
		qt.Check(t, qt.Equals(string(data), "\xff"))
		w.Write([]byte{0xff})
	}))

	var recorder logRecorder
	client := &http.Client{
		Transport: Transport(&TransportConfig{
			Logger: &recorder,
		}),
	}
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/foo/bar", bytes.NewReader([]byte{0xff}))
	qt.Assert(t, qt.IsNil(err))
	resp, err := client.Do(req)
	qt.Assert(t, qt.IsNil(err))
	data, err := io.ReadAll(resp.Body)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(data), "\xff"))
	resp.Body.Close()
	qt.Assert(t, qt.DeepEquals(recorder.EventKinds, []EventKind{KindClientSendRequest, KindClientRecvResponse}))
	qt.Assert(t, qt.DeepEquals(recorder.Events, []RequestOrResponse{
		&Request{
			ID:            11,
			Method:        "GET",
			ContentLength: 1,
			URL:           "http://localhost/foo/bar",
			Header:        http.Header{},
			BodyData: BodyData{
				Body64: []byte("\xff"),
			},
		},
		&Response{
			ID:         11,
			Method:     "GET",
			URL:        "http://localhost/foo/bar",
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Length": {"1"},
				"Content-Type":   {"text/plain; charset=utf-8"},
				"Date":           {"now"},
			},
			BodyData: BodyData{
				Body64: []byte{0xff},
			},
		},
	}))
}

type logRecorder struct {
	EventKinds []EventKind
	Events     []RequestOrResponse
}

func (r *logRecorder) Log(ctx context.Context, kind EventKind, event RequestOrResponse) {
	field := urlField(event)
	// Sanitize the host so we don't need to worry about localhost ports.
	u, err := url.Parse(*field)
	if err != nil {
		panic(err)
	}
	u.Host = "localhost"
	*field = u.String()

	if _, ok := headerField(event)["Date"]; ok {
		headerField(event)["Date"] = []string{"now"}
	}

	r.EventKinds = append(r.EventKinds, kind)
	r.Events = append(r.Events, event)
}

type errorTransport struct{}

func (errorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		req.Body.Close()
	}
	return nil, fmt.Errorf("error in RoundTrip")
}

func urlField(event RequestOrResponse) *string {
	switch event := event.(type) {
	case *Request:
		return &event.URL
	case *Response:
		return &event.URL
	}
	panic("unreachable")
}

func headerField(event RequestOrResponse) http.Header {
	switch event := event.(type) {
	case *Request:
		return event.Header
	case *Response:
		return event.Header
	}
	panic("unreachable")
}
