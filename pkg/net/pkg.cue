// SPDX-License-Identifier: Apache-2.0 AND BSD-3-Clause
// Copyright 2026 The CUE Authors
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

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

// Package net provides net-related type definitions.
//
// The IP-related definitions can be represented as either a string or a list of
// byte values. To allow one format over an other these types can be further
// constraint using string or [...]. For instance,
//
// 	// multicast defines a multicast IP address in string form.
// 	multicast: net.MulticastIP & string
//
// 	// unicast defines a global unicast IP address in list form.
// 	unicast: net.GlobalUnicastIP & [...]
package net

// SplitHostPort splits a network address of the form "host:port",
// "host%zone:port", "[host]:port" or "[host%zone]:port" into host or host%zone
// and port.
//
// A literal IPv6 address in hostport must be enclosed in square brackets, as in
// "[::1]:80", "[::1%lo0]:80".
SplitHostPort: func(s: string) -> [...string]

// JoinHostPort combines host and port into a network address of the
// form "host:port". If host contains a colon, as found in literal
// IPv6 addresses, then JoinHostPort returns "[host]:port".
//
// See func Dial for a description of the host and port parameters.
JoinHostPort: func(host: #IP, port: string | bytes | int) -> string

// FQDN reports whether is a valid fully qualified domain name.
//
// FQDN allows only ASCII characters as prescribed by RFC 1034 (A-Z, a-z, 0-9
// and the hyphen).
FQDN: validator(string) | (func(s: string) -> bool)

IPv4len: 4

IPv6len: 16

// ParseIP parses s as an IP address, returning the result.
// The string s can be in dotted decimal ("192.0.2.1")
// or IPv6 ("2001:db8::68") form.
// If s is not a valid textual representation of an IP address,
// ParseIP returns an error.
ParseIP: func(s: string) -> [...]

// IPv4 reports whether ip is a valid IPv4 address.
//
// The address may be a string or list of bytes.
IPv4: validator(#IP) | (func(ip: #IP) -> bool)

// IPv6 reports whether ip is a valid IPv6 address.
//
// The address may be a string or list of bytes.
IPv6: validator(#IP) | (func(ip: #IP) -> bool)

// IP reports whether ip is a valid IPv4 or IPv6 address.
//
// The address may be a string or list of bytes.
IP: validator(#IP) | (func(ip: #IP) -> bool)

// IPCIDR reports whether ip is a valid IPv4 or IPv6 address with CIDR subnet notation.
//
// The address may be a string or list of bytes.
IPCIDR: validator(#CIDR) | (func(ip: #CIDR) -> bool)

// LoopbackIP reports whether ip is a loopback address.
LoopbackIP: validator(#IP) | (func(ip: #IP) -> bool)

// MulticastIP reports whether ip is a multicast address.
MulticastIP: validator(#IP) | (func(ip: #IP) -> bool)

// InterfaceLocalMulticastIP reports whether ip is an interface-local multicast
// address.
InterfaceLocalMulticastIP: validator(#IP) | (func(ip: #IP) -> bool)

// LinkLocalMulticastIP reports whether ip is a link-local multicast address.
LinkLocalMulticastIP: validator(#IP) | (func(ip: #IP) -> bool)

// LinkLocalUnicastIP reports whether ip is a link-local unicast address.
LinkLocalUnicastIP: validator(#IP) | (func(ip: #IP) -> bool)

// GlobalUnicastIP reports whether ip is a global unicast address.
//
// The identification of global unicast addresses uses address type
// identification as defined in RFC 1122, RFC 4632 and RFC 4291 with the
// exception of IPv4 directed broadcast addresses. It returns true even if ip is
// in IPv4 private address space or local IPv6 unicast address space.
GlobalUnicastIP: validator(#IP) | (func(ip: #IP) -> bool)

// UnspecifiedIP reports whether ip is an unspecified address, either the IPv4
// address "0.0.0.0" or the IPv6 address "::".
UnspecifiedIP: validator(#IP) | (func(ip: #IP) -> bool)

// ToIP4 converts a given IP address, which may be a string or a list, to its
// 4-byte representation.
ToIP4: func(ip: #IP) -> [...]

// ToIP16 converts a given IP address, which may be a string or a list, to its
// 16-byte representation.
ToIP16: func(ip: #IP) -> [...]

// IPString returns the string form of the IP address ip. It returns one of 4 forms:
//
// - "<nil>", if ip has length 0
// - dotted decimal ("192.0.2.1"), if ip is an IPv4 or IP4-mapped IPv6 address
// - IPv6 ("2001:db8::1"), if ip is a valid IPv6 address
// - the hexadecimal form of ip, without punctuation, if no other cases apply
IPString: func(ip: #IP) -> string

// AddIP adds a numerical offset to a given IP address.
// The address can be provided as a string, byte array, or CIDR subnet notation.
// It returns the resulting IP address or CIDR subnet notation as a string.
AddIP: func(ip: #IP, offset: int) -> string

// AddIPCIDR adds a numerical offset to a given CIDR subnet
// string, returning a CIDR string.
AddIPCIDR: func(ip: #CIDR, offset: int) -> string

// ParseCIDR parses a CIDR notation string and returns its components:
// prefix_mask (e.g. "255.255.255.0"), prefix_len (e.g. 24),
// prefix_addr (e.g. "10.20.30.0"), and broadcast_addr (e.g. "10.20.30.255").
// broadcast_addr is only set for IPv4 CIDRs.
ParseCIDR: func(s: string) -> {...}

// InCIDR reports whether an IP address is contained a CIDR subnet string.
InCIDR: (func(cidr: #CIDR) -> validator(#IP)) | (func(ip: #IP, cidr: #CIDR) -> bool)

// CompareIP compares two IP addresses and returns an integer:
// -1 if ip1 sorts before ip2, 0 if they are equal, and +1 if ip1 sorts after ip2.
// IPv4 addresses sort before IPv6 addresses.
//
// The addresses may be strings or lists of bytes.
CompareIP: func(ip1: #IP, ip2: #IP) -> int

// PathEscape escapes the string so it can be safely placed inside a URL path
// segment, replacing special characters (including /) with %XX sequences as
// needed.
PathEscape: func(s: string) -> string

// PathUnescape does the inverse transformation of PathEscape, converting each
// 3-byte encoded substring of the form "%AB" into the hex-decoded byte 0xAB.
// It returns an error if any % is not followed by two hexadecimal digits.
//
// PathUnescape is identical to QueryUnescape except that it does not unescape
// '+' to ' ' (space).
PathUnescape: func(s: string) -> string

// QueryEscape escapes the string so it can be safely placed inside a URL
// query.
QueryEscape: func(s: string) -> string

// QueryUnescape does the inverse transformation of QueryEscape, converting
// each 3-byte encoded substring of the form "%AB" into the hex-decoded byte
// 0xAB. It returns an error if any % is not followed by two hexadecimal
// digits.
QueryUnescape: func(s: string) -> string

// URL validates that s is a valid relative or absolute URL.
// Note: this does also allow non-ASCII characters.
URL: validator(string) | (func(s: string) -> bool)

// AbsURL validates that s is an absolute URL.
// Note: this does also allow non-ASCII characters.
AbsURL: validator(string) | (func(s: string) -> bool)

// An #IP is an IP address in any of the forms the builtins of this
// package accept: its textual representation as a string or as bytes,
// such as "192.0.2.1" or "2001:db8::68", or a list of the address's
// byte values.
#IP: string | bytes | [...int]

// A #CIDR is a subnet in CIDR notation, such as "192.0.2.0/24", as a
// string or its bytes.
#CIDR: string | bytes
