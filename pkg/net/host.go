// SPDX-License-Identifier: Apache-2.0 AND BSD-3-Clause
// Copyright 2019 CUE Authors
// Copyright The Go Authors.
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

// Parts of this file are derived from Go's net package.

package net

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/pkg"
)

// TODO: revert to using ValidateLabels and StrictDomainName once we
// require Go 1.27. https://go.dev/issue/74848 is fixed, but the fix rides
// on the Unicode 17 IDNA tables, which x/net/idna only builds on Go 1.27
// and later, so the revert still regresses net.FQDN on Go 1.26.
var idnaProfile = idna.New(
	idna.MapForLookup(),
	idna.VerifyDNSLength(true),
)

// SplitHostPort splits a network address of the form "host:port",
// "host%zone:port", "[host]:port" or "[host%zone]:port" into host or host%zone
// and port.
//
// A literal IPv6 address in hostport must be enclosed in square brackets, as in
// "[::1]:80", "[::1%lo0]:80".
func SplitHostPort(s string) (hostport []string, err error) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return nil, err
	}
	return []string{host, port}, nil
}

// JoinHostPort combines host and port into a network address of the
// form "host:port". If host contains a colon, as found in literal
// IPv6 addresses, then JoinHostPort returns "[host]:port".
//
// See func Dial for a description of the host and port parameters.
func JoinHostPort(host, port cue.Value) (string, error) {
	var err error
	hostStr := ""
	switch host.Kind() {
	case cue.ListKind:
		ipdata, ipErr := netGetIP(host)
		if ipErr != nil {
			err = fmt.Errorf("invalid host %s", host)
		} else {
			hostStr = ipdata.String()
		}
	case cue.BytesKind:
		var b []byte
		b, err = host.Bytes()
		hostStr = string(b)
	default:
		hostStr, err = host.String()
	}
	if err != nil {
		return "", err
	}

	portStr := ""
	switch port.Kind() {
	case cue.StringKind:
		portStr, err = port.String()
	case cue.BytesKind:
		var b []byte
		b, err = port.Bytes()
		portStr = string(b)
	default:
		var i int64
		i, err = port.Int64()
		portStr = strconv.Itoa(int(i))
	}
	if err != nil {
		return "", err
	}

	return net.JoinHostPort(hostStr, portStr), nil
}

// FQDN reports whether is a valid fully qualified domain name.
//
// FQDN allows only ASCII characters as prescribed by RFC 1034 (A-Z, a-z, 0-9
// and the hyphen).
func FQDN(s string) pkg.Validator {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	// Strip the canonical trailing root dot; idna rejects it under VerifyDNSLength.
	s = strings.TrimSuffix(s, ".")
	_, err := idnaProfile.ToASCII(s)
	return err == nil
}
