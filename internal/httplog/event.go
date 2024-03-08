package httplog

import (
	"context"
	"net/http"
)

type Logger interface {
	// Log logs an event of the given kind with the given request
	// or response (either *Request or *Response).
	Log(ctx context.Context, kind EventKind, r RequestOrResponse)
}

type EventKind int

const (
	NoEvent EventKind = iota
	KindClientSendRequest
	KindClientRecvResponse

	// TODO KindServerRecvRequest
	// TODO KindServerSendResponse
)

func (k EventKind) String() string {
	switch k {
	case KindClientSendRequest:
		return "client->"
	case KindClientRecvResponse:
		return "client<-"
	default:
		return "unknown"
	}
}

// Request represents an HTTP request.
type Request struct {
	ID            int64       `json:"id"`
	Method        string      `json:"method"`
	URL           string      `json:"url"`
	ContentLength int64       `json:"contentLength"`
	Header        http.Header `json:"header"`
	BodyData
}

func (*Request) requestOrResponse() {}

// RequestOrResponse is implemented by [*Request] and [*Response].
type RequestOrResponse interface {
	requestOrResponse()
}

// Response represents an HTTP response.
type Response struct {
	ID         int64       `json:"id"`
	Method     string      `json:"method,omitempty"`
	URL        string      `json:"url,omitempty"`
	Error      string      `json:"error,omitempty"`
	StatusCode int         `json:"statusCode,omitempty"`
	Header     http.Header `json:"header,omitempty"`
	BodyData
}

// BodyData holds information about the body of a request
// or response.
type BodyData struct {
	Body                string `json:"body,omitempty"`
	Body64              []byte `json:"body64,omitempty"`
	BodyRedactedBecause string `json:"bodyRedactedBecause,omitempty"`
	BodyTruncated       bool   `json:"bodyTruncated,omitempty"`
}

func (*Response) requestOrResponse() {}
