package httplog

import "net/http"

// Handler returns an [http.Handler] that wraps h,
// sending requests and response to logger. If logger
// is nil, the zero [SlogLogger] will be used.
func Handler(logger Logger, h http.Handler) http.Handler {
	panic("unimplemented")
}
