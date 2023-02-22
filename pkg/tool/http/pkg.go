// Code generated by cuelang.org/go/pkg/gen. DO NOT EDIT.

// Package http provides tasks related to the HTTP protocol.
//
// These are the supported tasks:
//
//	Get:    Do & {method: "GET"}
//	Post:   Do & {method: "POST"}
//	Put:    Do & {method: "PUT"}
//	Delete: Do & {method: "DELETE"}
//
//	Do: {
//		$id: *"tool/http.Do" | "http" // http for backwards compatibility
//
//		method: string
//		url:    string // TODO: make url.URL type
//
//		tls: {
//			// Whether the server certificate must be validated.
//			verify: *true | bool
//			// PEM encoded certificate(s) to validate the server certificate.
//			// If not set the CA bundle of the system is used.
//			caCert?: bytes | string
//		}
//
//		request: {
//			body?: bytes | string
//			header: [string]:  string | [...string]
//			trailer: [string]: string | [...string]
//		}
//		response: {
//			status:     string
//			statusCode: int
//
//			body: *bytes | string
//			header: [string]:  string | [...string]
//			trailer: [string]: string | [...string]
//		}
//	}
//
//	//  TODO: support serving once we have the cue serve command.
//	// Serve: {
//	//  port: int
//	//
//	//  cert: string
//	//  key:  string
//	//
//	//  handle: [Pattern=string]: Message & {
//	//   pattern: Pattern
//	//  }
//	// }
package http

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
)

func init() {
	pkg.Register("tool/http", p)
}

var _ = adt.TopKind // in case the adt package isn't used

var p = &pkg.Package{
	Native: []*pkg.Builtin{},
	CUE: `{
	Get: Do & {
		method: "GET"
	}
	Post: Do & {
		method: "POST"
	}
	Put: Do & {
		method: "PUT"
	}
	Delete: Do & {
		method: "DELETE"
	}
	Do: {
		$id:    *"tool/http.Do" | "http"
		method: string
		url:    string
		tls: {
			verify:  *true | bool
			caCert?: bytes | string
		}
		request: {
			body?: bytes | string
			header: {
				[string]: string | [...string]
			}
			trailer: {
				[string]: string | [...string]
			}
		}
		response: {
			status:     string
			statusCode: int
			body:       *bytes | string
			header: {
				[string]: string | [...string]
			}
			trailer: {
				[string]: string | [...string]
			}
		}
	}
}`,
}
