package httplog

import "context"

type (
	allowedURLQueryParamsKey struct{}
	redactResponseBodyKey    struct{}
	redactRequestBodyKey     struct{}
)

// ContextWithAllowedURLQueryParams returns a context that will allow only the URL
// query parameters for which the given allow function returns true. All others will
// be redacted from the logs.
func ContextWithAllowedURLQueryParams(ctx context.Context, allow func(key string) bool) context.Context {
	return context.WithValue(ctx, allowedURLQueryParamsKey{}, allow)
}

func queryParamChecker(ctx context.Context) func(string) bool {
	f, ok := ctx.Value(allowedURLQueryParamsKey{}).(func(string) bool)
	if ok {
		return f
	}
	return func(string) bool {
		return false
	}
}

// RedactResponseBody returns a context that will cause
// [Transport] to redact the response body when logging HTTP responses.
// If reason is empty, the body will not be redacted.
func RedactResponseBody(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, redactResponseBodyKey{}, reason)
}

func shouldRedactBody(ctx context.Context, isRequest bool) string {
	key := any(redactResponseBodyKey{})
	if isRequest {
		key = redactRequestBodyKey{}
	}
	s, _ := ctx.Value(key).(string)
	return s
}

// RedactRequestBody returns a context that will cause
// the logger to redact the request body when logging HTTP requests.
func RedactRequestBody(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, redactRequestBodyKey{}, reason)
}
