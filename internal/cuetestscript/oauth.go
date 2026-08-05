// Copyright 2026 The CUE Authors
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

package cuetestscript

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
)

// The modes supported by [NewMockRegistryOauth].
const (
	// OauthDeviceCodeNotFound makes /login/device/code respond with 404.
	OauthDeviceCodeNotFound = "device-code-not-found"

	// OauthDeviceCodeMethodNotAllowed makes /login/device/code respond with 405.
	OauthDeviceCodeMethodNotAllowed = "device-code-method-not-allowed"

	// OauthDeviceCodeExpired makes polling for a token with device_code
	// always respond with [tokenErrorCodeExpired].
	OauthDeviceCodeExpired = "device-code-expired"

	// OauthPendingForever makes polling for a token with device_code always
	// respond with [tokenErrorCodePending].
	OauthPendingForever = "pending-forever"

	// OauthPendingSuccess makes polling for a token with device_code respond
	// with [tokenErrorCodePending] once, and then succeed.
	OauthPendingSuccess = "pending-success"

	// OauthImmediateSuccess makes polling for a token with device_code
	// succeed right away.
	OauthImmediateSuccess = "immediate-success"
)

// NewMockRegistryOauth starts a test HTTP server with the OAuth2 device flow
// endpoints used by `cue login` to obtain an access token. The mode describes
// the server's behavior; it must be one of the Oauth* constants above.
//
// Note that this HTTP server isn't an OCI registry yet, as that isn't needed
// for now.
//
// TODO: once we support refresh tokens, add those endpoints and test them too.
func NewMockRegistryOauth(mode string) (*httptest.Server, error) {
	switch mode {
	case OauthDeviceCodeNotFound,
		OauthDeviceCodeMethodNotAllowed,
		OauthDeviceCodeExpired,
		OauthPendingForever,
		OauthPendingSuccess,
		OauthImmediateSuccess:
	default:
		return nil, fmt.Errorf("unknown oauth registry mode %q", mode)
	}
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	const (
		staticUserCode    = "user-code"
		staticDeviceCode  = "device-code-longer-string"
		staticAccessToken = "secret-access-token"
		intervalSecs      = 1 // 1s to keep the tests fast
	)
	// OAuth2 Device Authorization Request endpoint: https://datatracker.ietf.org/doc/html/rfc8628#section-3.1
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case OauthDeviceCodeNotFound:
			http.Error(w, "404 page not found", http.StatusNotFound)
			return
		case OauthDeviceCodeMethodNotAllowed:
			http.Error(w, "405 method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, oauth2.DeviceAuthResponse{
			DeviceCode: staticDeviceCode,
			UserCode:   staticUserCode,

			VerificationURI:         ts.URL + "/login/device",
			VerificationURIComplete: ts.URL + "/login/device?user_code=" + url.QueryEscape(staticUserCode),

			Expiry:   time.Now().Add(time.Minute),
			Interval: intervalSecs,
		})
	})
	// OAuth2 Token endpoint: https://datatracker.ietf.org/doc/html/rfc6749#section-3.2
	var tokenRequestCounter atomic.Int64
	mux.HandleFunc("/login/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		deviceCode := r.FormValue("device_code")
		if deviceCode != staticDeviceCode {
			writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodeDenied})
			return
		}
		switch mode {
		case OauthDeviceCodeExpired:
			writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodeExpired})
		case OauthPendingForever:
			writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodePending})
		case OauthPendingSuccess:
			count := tokenRequestCounter.Add(1)
			if count == 1 {
				writeJSON(w, http.StatusBadRequest, tokenError{ErrorCode: tokenErrorCodePending})
				break
			}
			fallthrough
		case OauthImmediateSuccess:
			writeJSON(w, http.StatusOK, oauth2.Token{
				AccessToken: staticAccessToken,
				TokenType:   "Bearer",
				ExpiresIn:   int64(time.Hour / time.Second), // 1h in seconds
			})
		}
	})
	return ts, nil
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	b, err := json.Marshal(v)
	if err != nil { // should never happen
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(b)
}

const (
	// Device flow token error code strings from https://datatracker.ietf.org/doc/html/rfc8628#section-3.5
	tokenErrorCodePending = "authorization_pending" // waiting for user
	tokenErrorCodeDenied  = "access_denied"         // the user denied the request
	tokenErrorCodeExpired = "expired_token"         // the device_code expired
)

// tokenError implements the error response structure defined by
// https://datatracker.ietf.org/doc/html/rfc6749#section-5.2
type tokenError struct {
	ErrorCode        string `json:"error"` // one of the constants above
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
}
