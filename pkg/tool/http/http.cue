// Copyright 2018 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package http

Get: Do & {method: "GET"}
Post: Do & {method: "POST"}
Put: Do & {method: "PUT"}
Delete: Do & {method: "DELETE"}

Do: {
	$id: _id
	_id: *"tool/http.Do" | "http" // http for backwards compatibility

	method: string
	url:    string // TODO: make url.URL type

	// followRedirects controls whether the http client follows redirects
	// or not. Defaults to true, like the default net/http client in Go.
	followRedirects: *true | bool

	tls: {
		// Whether the server certificate must be validated.
		verify: *true | bool
		// PEM encoded certificate(s) to validate the server certificate.
		// If not set the CA bundle of the system is used.
		caCert?: bytes | string
	}

	request: {
		body?: bytes | string
		header: [string]: [string, ...string]
		trailer: [string]: [string, ...string]
	}
	response: {
		status:     string
		statusCode: int

		body: *bytes | string
		header: [string]: string | [...string]
		trailer: [string]: string | [...string]
	}
}

// Serve launches a task that listens on the given port and serves HTTP
// requests. (EXPERIMENTAL)
//
// Serve support HTTP multiplexing. Multiple tasks can be configured to be
// served from the same address. Serve will multiplex these different instances
// based on the serving path and, optionally, method.
//
// For more details see the documentation of the routing parameters such as
// path and method.
Serve: {
	$id: _id
	_id: "tool/http.Serve"

	// listenAddr is the address to listen on (e.g., ":8080", "localhost:8000").
	// This field is required to avoid accidentally binding to privileged ports.
	listenAddr!: string

	// routing configures the HTTP routes that are served.
	//
	// Routing is done based on path and methods (TODO: allow host as well)
	//
	// Literal (that is, non-wildcard) parts of a pattern match the
	// corresponding parts of a request case-sensitively.
	//
	// If no method is given it matches every method. If routing.method is set to
	// "GET", it matches both GET and HEAD requests. Otherwise, the method must
	// match exactly.
	//
	// TODO: When no host is given, every host is matched. A pattern with a host
	// matches URLs on that host only.
	//
	// A path can include wildcard segments of the form {NAME} or {NAME...}. For
	// example, "/b/{bucket}/o/{objectname...}". The wildcard name must be a
	// valid Go identifier. Wildcards must be full path segments: they must be
	// preceded by a slash and followed by either a slash or the end of the
	// string. For example, "/b_{bucket}" is not a valid pattern.
	//
	// Normally a wildcard matches only a single path segment, ending at the
	// next literal slash (not %2F) in the request URL. But if "..." is
	// present, then the wildcard matches the remainder of the URL path,
	// including slashes. (Therefore it is invalid for a "..." wildcard to
	// appear anywhere but at the end of a pattern.) The match for a wildcard
	// can be obtained from request.pathValues with the wildcard's name. A
	// trailing slash in a path acts as an anonymous "..." wildcard.
	//
	// The special wildcard {$} matches only the end of the URL. For example,
	// the pattern "/{$}" matches only the path "/", whereas the pattern "/"
	// matches every path.
	//
	// For matching, both pattern paths and incoming request paths are unescaped
	// segment by segment. So, for example, the path "/a%2Fb/100%25" is treated
	// as having two segments, "a/b" and "100%". The pattern "/a%2fb/" matches
	// it, but the pattern "/a/b/" does not.
	//
	//
	// Precedence
	//
	// If two or more patterns match a request, then the most specific pattern
	// takes precedence. A pattern P1 is more specific than P2 if P1 matches a
	// strict subset of P2’s requests; that is, if P2 matches all the requests
	// of P1 and more. If neither is more specific, then the patterns conflict.
	// There is one exception to this rule: if two patterns would otherwise
	// conflict and one has a host while the other does not, then the pattern
	// with the host takes precedence. If a pattern conflicts with another
	// pattern that is already registered the task will panic.
	//
	// As an example of the general rule, "/images/thumbnails/" is more specific
	// than "/images/", so both can be registered. The former matches paths
	// beginning with "/images/thumbnails/" and the latter will match any other
	// path in the "/images/" subtree.
	//
	// As another example, consider a route with path "/" and method "GET" versus
	// a route with path "/index.html" and no method: both match a GET request
	// for "/index.html", but the former matches all other GET and HEAD requests,
	// while the latter matches any request for "/index.html" that uses a
	// different method. These routes would conflict.
	routing: {
		// path sets the path to route to. It may include wildcard segments
		// as described above.
		path: *"/" | =~"^/"

		// method optionally sets the HTTP method to match (e.g. "GET" |
		// "POST"). If not set, all methods are accepted.
		method?: string
	}

	// TODO:
	// - schemes: string // e.g. "http" | "https"
	// - TLS

	// request holds data about the incoming HTTP request.
	//
	// Fields marked [runtime] are populated automatically when a request is
	// received. Users can add constraints to these fields to validate input,
	// for example: `form: u!: [string]` to require a query parameter "u".
	//
	// The value field is for user-defined parsing of the request body.
	request: {
		// method is the HTTP method (GET, POST, PUT, etc.).
		method: string

		// url is the full request URL. [runtime]
		url: string

		// body is the raw request body. [runtime]
		body: *bytes | string

		// value can be set by the user to hold a parsed representation of
		// the body. For example: `value: json.Unmarshal(body)`
		value?: _

		// pathValues contains values extracted from URL path wildcards.
		// For example, with routing.path: "/users/{id}", a request to
		// "/users/123" would have pathValues: {id: "123"}. [runtime]
		pathValues: [string]: string

		// form contains the parsed form data, including both the URL
		// query parameters and POST/PUT/PATCH form bodies. [runtime]
		form: [string]: [...string]

		// header contains the request headers. Each header key maps to a
		// non-empty list of values, as HTTP allows multiple values per header.
		// [runtime]
		header: [string]: [string, ...string]

		// trailer contains the request trailers. Each trailer key maps to a
		// non-empty list of values. [runtime]
		trailer: [string]: [string, ...string]
	}

	// response defines the HTTP response to send back to the client.
	// All fields are optional and user-defined.
	response: {
		// statusCode sets the HTTP status code. If not set, 200 is used.
		statusCode?: int

		// body is the response body to send.
		body?: *bytes | string

		// header sets response headers. Each key can be set to either a single
		// string value or a list of values for headers with multiple values.
		header?: [string]: string | [...string]

		// trailer sets response trailers. Each key can be set to either a single
		// string value or a list of values.
		trailer?: [string]: string | [...string]
	}
}
